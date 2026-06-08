package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/joho/godotenv"
	"github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/logging"
	"github.com/xact-iot/xact/metrics"
	"github.com/xact-iot/xact/notifications"
	"github.com/xact-iot/xact/rtdb/api"
	"github.com/xact-iot/xact/rtdb/blocks"
	"github.com/xact-iot/xact/rtdb/ingest"
	"github.com/xact-iot/xact/rtdb/ingest/mqtt"
	"github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/persistence"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/scheduler"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
	"github.com/xact-iot/xact/sqldb/sqlite"
	"github.com/xact-iot/xact/tagcalcs"
	webapi "github.com/xact-iot/xact/web/api"
)

//go:embed VERSION.txt
var Version string

func appVersion() string {
	return strings.TrimSpace(Version)
}

func main() {
	spew.Config.Indent = "   "

	// Load environment variables early so config can use them
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found, using environment variables")
	}

	log.Printf("XACT version %s starting", appVersion())
	if err := validateProductionSecrets(); err != nil {
		log.Fatalf("Production security configuration is invalid: %v", err)
	}

	// Resolve plugin directory. In development the server may be started from
	// either the repo root or server/, so accept both common relative layouts.
	pluginDir := os.Getenv("PLUGIN_DIR")
	if pluginDir == "" {
		pluginDir = resolveDefaultPluginDir()
	}

	// Ensure the plugin directory tree exists
	for _, sub := range []string{"authentication", "widgets", "map-layer", "themes"} {
		if err := os.MkdirAll(filepath.Join(pluginDir, sub), 0o755); err != nil {
			log.Printf("Warning: could not create plugin dir %s/%s: %v", pluginDir, sub, err)
		}
	}

	// Read or generate NATS credentials.
	// NATS_INTERNAL_PASSWORD is used by the Go server process (full access).
	// NATS_BROWSER_TOKEN is served to authenticated browsers with scoped permissions.
	internalPassword := os.Getenv("NATS_INTERNAL_PASSWORD")
	if internalPassword == "" {
		internalPassword = mustRandHex(16)
	}
	browserToken := os.Getenv("NATS_BROWSER_TOKEN")
	if browserToken == "" {
		browserToken = mustRandHex(16)
	}

	// HTTPS configuration from environment
	enableHTTPS := os.Getenv("ENABLE_HTTPS") == "yes"
	certsDir := os.Getenv("HTTP_CERTS_DIR")
	if certsDir == "" {
		certsDir = os.Getenv("HTTPS_CERTS_DIR")
	}

	// Configure NATS server with WebSocket support and user-based permissions.
	natsStoreDir := os.Getenv("NATS_STORE_DIR")
	if natsStoreDir == "" {
		natsStoreDir = "./nats-store"
	}

	// Configure NATS WebSocket - use TLS when HTTPS is enabled to avoid
	// browser mixed-content blocks.
	wsNoTLS := true
	var wsTLSConfig *tls.Config
	if enableHTTPS && certsDir != "" {
		crtFile := filepath.Join(certsDir, "server.crt")
		keyFile := filepath.Join(certsDir, "server.key")
		if cert, err := tls.LoadX509KeyPair(crtFile, keyFile); err == nil {
			wsNoTLS = false
			wsTLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		}
	}

	natsHost := envStringDefault("NATS_HOST", "127.0.0.1")
	natsPort := envIntDefault("NATS_PORT", 4222)
	natsWSHost := envStringDefault("NATS_WS_HOST", "127.0.0.1")
	natsWSPort := envIntDefault("NATS_WS_PORT", 9222)
	natsLogFile := envStringDefault("NATS_LOG_FILE", "./logs/nats.log")
	if err := ensureParentDir(natsLogFile); err != nil {
		log.Fatalf("Failed to prepare NATS log file %s: %v", natsLogFile, err)
	}
	natsDiag := natsStartupDiagnostics{
		ClientHost:    natsHost,
		ClientPort:    natsPort,
		WebSocketHost: natsWSHost,
		WebSocketPort: natsWSPort,
		StoreDir:      natsStoreDir,
		LogFile:       natsLogFile,
	}

	browserPublishAllow := []string{"$JS.API.>", "_INBOX.>"}
	if envEnabled("NATS_BROWSER_ALLOW_COMMANDS") {
		browserPublishAllow = append(browserPublishAllow, "xact.command.>")
	}

	opts := &server.Options{
		Host: natsHost,
		Port: natsPort,
		Websocket: server.WebsocketOpts{
			Host:      natsWSHost,
			Port:      natsWSPort,
			NoTLS:     wsNoTLS,
			TLSConfig: wsTLSConfig,
		},
		StoreDir:   natsStoreDir,
		JetStream:  true,
		NoLog:      false,
		Debug:      envEnabled("NATS_DEBUG"),
		Trace:      envEnabled("NATS_TRACE"),
		LogFile:    natsLogFile,
		Logtime:    true,
		MaxPayload: 8 * 1024 * 1024,
		Users: []*server.User{
			{
				Username: "internal",
				Password: internalPassword,
				Permissions: &server.Permissions{
					Publish:   &server.SubjectPermission{Allow: []string{">"}},
					Subscribe: &server.SubjectPermission{Allow: []string{">"}},
				},
			},
			{
				// Browser clients: subscribe to RTDB/broadcast subjects and use
				// JetStream API (for KV and stream consumers). Publishing to
				// application subjects (rtdb.>, xact.>) is denied.
				Username: "browser",
				Password: browserToken,
				Permissions: &server.Permissions{
					Publish: &server.SubjectPermission{
						Allow: browserPublishAllow,
					},
					Subscribe: &server.SubjectPermission{
						Allow: []string{
							"rtdb.tree.>",
							"xact.internal.bcast.>",
							"$JS.>",
							"$KV.>",
							"_INBOX.>",
						},
					},
				},
			},
		},
	}

	// Create and start NATS server
	s, err := server.NewServer(opts)
	if err != nil {
		log.Fatalf("Failed to create NATS server: %v", err)
	}
	s.ConfigureLogger()
	log.Printf("Starting embedded NATS server: client=%s:%d websocket=%s:%d store=%s log=%s", natsHost, natsPort, natsWSHost, natsWSPort, natsStoreDir, natsLogFile)
	go s.Start()

	if !s.ReadyForConnections(10 * time.Second) {
		logNATSStartupDiagnostics("NATS server failed readiness check", s, natsDiag)
		log.Fatalf("NATS server failed to start within 10s; see %s for embedded NATS logs", natsLogFile)
	}

	// Connect as internal client with full-access credentials.
	nc, err := natsgo.Connect(s.ClientURL(),
		natsgo.UserInfo("internal", internalPassword),
		natsgo.Name("internal"),
	)
	if err != nil {
		logNATSStartupDiagnostics("NATS internal client connection failed", s, natsDiag)
		log.Fatalf("Failed to connect to NATS at %s: %v", s.ClientURL(), err)
	}
	defer nc.Close()

	// Prepare publish de-duplication (used by process blocks)
	if err := nats.PreparePubDedup(s, nc); err != nil {
		log.Fatalf("Failed to prepare publish de-dup: %v", err)
	}
	tagPublisher, err := nats.GetBroadcastStream(nats.TagValueStream)
	if err != nil {
		log.Fatalf("Failed to prepare tag value publisher: %v", err)
	}
	tree.TagValuePublisher = tagPublisher

	// Prepare scheduler de-dup lock store
	if err := scheduler.PrepareSchedLocks(nc); err != nil {
		log.Printf("Warning: scheduler lock store unavailable: %v", err)
	}

	// Prepare the tag persist store
	if err := nats.PreparePersistStore(nc); err != nil {
		log.Printf("Warning: persist KV store unavailable: %v", err)
	}
	if err := nats.PrepareCommandStream(nc); err != nil {
		log.Printf("Warning: command stream unavailable: %v", err)
	}

	// Initialize console logger
	console := logging.New(logging.DefaultConfig())

	// Initialize events publisher (publishes to NATS notifications stream)
	publisher, err := events.Init(nc, console)
	if err != nil {
		log.Fatalf("Failed to initialize events publisher: %v", err)
	}

	// Wire the events publisher into the processing blocks package
	blocks.SetEventsPublisher(publisher)

	console.Info("nats", "", "NATS server started", "addr", fmt.Sprintf("nats://%s:%d", natsHost, natsPort))
	if enableHTTPS {
		console.Info("nats", "", "WebSocket server started", "addr", fmt.Sprintf("wss://%s:%d", natsWSHost, natsWSPort))
	} else {
		console.Info("nats", "", "WebSocket server started", "addr", fmt.Sprintf("ws://%s:%d", natsWSHost, natsWSPort))
	}

	// Create TreeSync for publishing changes
	treeSync := nats.NewTreeSync(nc, "rtdb.tree.")

	// Create tree (onChange callback set up below after persistence manager)
	treeOps := tree.NewTreeWithOperations(nil)

	// Initialize database (PostgreSQL or SQLite, selected by environment variable)
	var database sqldb.DB
	var persistMgr *persistence.Manager
	var eventWriter *events.EventWriter
	var notifHandler *events.NotificationHandler
	var metricWriter *sqldb.MetricWriter

	dbURL := os.Getenv("DATABASE_URL")
	sqlitePath := os.Getenv("SQLITE_PATH")

	if dbURL != "" || sqlitePath != "" {
		ctx := context.Background()

		// Connect to the chosen backend.
		if dbURL != "" {
			pgDB, err := psql.NewPostgresDB(ctx, dbURL)
			if err != nil {
				log.Fatalf("Failed to connect to database: %v", err)
			}
			defer pgDB.Close()
			if err := pgDB.Migrate(ctx); err != nil {
				log.Fatalf("Failed to run database migrations: %v", err)
			}
			console.Info("db", "", "PostgreSQL connected and migrated")
			database = pgDB

			// Configure time-series retention (default 180 days, PostgreSQL/TimescaleDB only)
			retentionDays := 180
			if v := os.Getenv("METRICS_RETENTION_DAYS"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					retentionDays = n
				}
			}
			if err := pgDB.ConfigureMetricsRetention(ctx,
				time.Duration(retentionDays)*24*time.Hour); err != nil {
				console.Warn("db", "", "Metrics retention policy not configured", "error", err)
			}
		} else {
			sqlDB, err := sqlite.NewSQLiteDB(ctx, sqlitePath)
			if err != nil {
				log.Fatalf("Failed to open SQLite database: %v", err)
			}
			defer sqlDB.Close()
			if err := sqlDB.Migrate(ctx); err != nil {
				log.Fatalf("Failed to run SQLite migrations: %v", err)
			}
			console.Info("db", "", "SQLite connected and migrated", "path", sqlitePath)
			database = sqlDB
		}

		// Register HistoryRecorder processing block
		metricWriter = blocks.RegisterHistoryRecorder(database)

		// Start EventWriter (buffers and batch-inserts events to DB)
		eventWriter = events.NewEventWriter(database)
		eventWriter.Start()
		if retentionDays := eventRetentionDays(); retentionDays > 0 {
			eventWriter.EnablePurger(database, retentionDays)
			console.Info("events", "", "Event retention purger enabled", "retentionDays", retentionDays)
		} else {
			console.Info("events", "", "Event retention purger disabled")
		}

		// Load notification channel config and create senders
		channelCfg, err := notifications.LoadChannelConfig(ctx, database, "default")
		if err != nil {
			console.Warn("notifications", "", "Failed to load channel config", "error", err)
		}
		var notifiers []events.Notifier
		if channelCfg.Email.Host != "" {
			notifiers = append(notifiers, events.NewEmailSenderFromConfig(events.EmailConfig{
				Host:     channelCfg.Email.Host,
				Port:     channelCfg.Email.Port,
				Username: channelCfg.Email.Username,
				Password: channelCfg.Email.Password,
				From:     channelCfg.Email.From,
				UseTLS:   channelCfg.Email.UseTLS,
			}))
			console.Info("notifications", "", "Email sender configured", "host", channelCfg.Email.Host)
		}
		if channelCfg.Telegram.BotToken != "" {
			notifiers = append(notifiers, events.NewTelegramSenderFromConfig(events.TelegramConfig{
				BotToken: channelCfg.Telegram.BotToken,
			}))
			console.Info("notifications", "", "Telegram sender configured")
		}

		// Start NotificationHandler (subscribes to NATS, calls EventWriter + dispatches)
		resolver := &notifications.DBResolver{DB: database}
		notifHandler, err = events.NewNotificationHandler(nc, eventWriter, resolver, notifiers)
		if err != nil {
			log.Fatalf("Failed to start notification handler: %v", err)
		}

		// Try to restore tree from database
		persistMgr = persistence.NewManager(database, treeOps, "default", 5*time.Second)
		restored, err := persistMgr.Restore(ctx)
		if err != nil {
			console.Warn("db", "", "Failed to restore tree config", "error", err)
		}
		if !restored {
			console.Info("db", "", "No saved tree config found, starting with empty tree")
		}
	} else {
		console.Info("db", "", "No DATABASE_URL or SQLITE_PATH set, persistence disabled")
	}

	// Publish all changes (including value updates) to NATS.
	treeOps.SetOnChange(func(path string, node tree.TreeNode) {
		if err := treeSync.PublishChange(path, node); err != nil {
			console.Error("tree", "", "Failed to publish change", "error", err)
		}
	})

	// Mark dirty only on structural changes (node/tag create/delete/update),
	// not on leaf value updates, so periodic script writes don't trigger saves.
	if persistMgr != nil {
		treeOps.SetOnStructureChange(func(path string, node tree.TreeNode) {
			persistMgr.MarkDirty()
		})
	}

	// Configure and start REST API server
	staticServeMode := os.Getenv("STATIC_SERVE_MODE")
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./web"
	}

	// XACT is always deployed behind a reverse proxy at /xact/ - the proxy
	// forwards /xact/* to the server, which serves at that same prefix.
	allowedOrigins := envList("CORS_ALLOWED_ORIGINS")
	if len(allowedOrigins) == 0 && !isProductionMode() {
		allowedOrigins = []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"https://localhost:3000",
			"https://localhost:5173",
		}
	}
	apiConfig := api.ServerConfig{
		Host: envStringDefault("API_HOST", "127.0.0.1"),
		Port: envIntDefault("API_PORT", 8080),
		TLS: api.TLSConfig{
			Enabled:  false,
			CertFile: "",
			KeyFile:  "",
		},
		ProxyPath:                "/xact",
		StaticServeMode:          staticServeMode,
		StaticDir:                staticDir,
		ExposeNATSInternalConfig: envEnabled("EXPOSE_NATS_INTERNAL_CONFIG"),
		AllowedOrigins:           allowedOrigins,
		MaxRequestBodyBytes:      envInt64Default("MAX_REQUEST_BODY_BYTES", 8<<20),
	}

	if enableHTTPS {
		apiConfig.TLS.Enabled = true
		if certsDir != "" {
			crtFile := filepath.Join(certsDir, "server.crt")
			keyFile := filepath.Join(certsDir, "server.key")
			if _, err := os.Stat(crtFile); err == nil {
				if _, err := os.Stat(keyFile); err == nil {
					apiConfig.TLS.CertFile = crtFile
					apiConfig.TLS.KeyFile = keyFile
				}
			}
		}
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = mustRandHex(32)
		log.Printf("Warning: JWT_SECRET is not set; generated a temporary secret for this server process")
	}

	// Subscribe to ingest events so this server processes all data published
	// by any server in the cluster (including itself).
	processor := ingest.NewProcessor(treeOps)
	processor.SetNotificationResolver(database)
	ingestSub, err := ingest.SubscribeIngest(nc, func(evt ingest.IngestEvent) error {
		if err := processor.WriteDeviceData(evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName, evt.TagData); err != nil {
			console.Error("ingest", "", "Failed to process ingest event", "error", err)
			return err
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to ingest events: %v", err)
	}

	apiConfig.AppVersion = appVersion()
	apiServer := api.NewServer(apiConfig, treeOps, treeSync, nc, jwtSecret, database, pluginDir)
	apiServer.SetEventsPublisher(publisher)

	// Log static file serving mode
	if apiConfig.StaticServeMode == "server" {
		if enableHTTPS {
			console.Info("api", "", "Static files served by server", "dir", apiConfig.StaticDir, "url", fmt.Sprintf("https://%s:%d/", apiConfig.Host, apiConfig.Port))
		} else {
			console.Info("api", "", "Static files served by server", "dir", apiConfig.StaticDir, "url", fmt.Sprintf("http://%s:%d/", apiConfig.Host, apiConfig.Port))
		}
	} else {
		console.Info("api", "", "Static files served by proxy (VITE/NGINX)")
	}

	// Populate organisation meta tags from the database.
	if database != nil {
		if err := apiServer.Bootstrap(context.Background()); err != nil {
			log.Printf("Warning: org bootstrap failed: %v", err)
		}
	}

	// Start tag calc engine.
	var tsEngine *tagcalcs.Engine
	if database != nil {
		tsEngine = tagcalcs.New(database, treeOps)
		if err := tsEngine.Load(context.Background()); err != nil {
			log.Printf("Warning: tag calc engine load failed: %v", err)
		}
		getOrg := func(r *http.Request) string {
			claims, ok := api.GetClaimsFromContext(r.Context())
			if !ok || claims.TenantID == "" {
				return "default"
			}
			return claims.TenantID
		}
		apiServer.SetTagCalcHandlers(webapi.NewTagCalcHandlers(database, tsEngine, getOrg))

		schedEngine := scheduler.NewWithOptions(database, nc, scheduler.EngineOptions{
			AllowUnsafeTasks: envEnabled("ENABLE_UNSAFE_SCHEDULER_TASKS"),
		})
		if err := schedEngine.LoadForOrg(context.Background(), "default"); err != nil {
			log.Printf("Warning: scheduler load failed: %v", err)
		}
		apiServer.SetScheduleHandlers(webapi.NewScheduleHandlers(database, schedEngine, getOrg))
		defer schedEngine.Stop()
	}

	// Start API server
	go func() {
		if enableHTTPS {
			console.Info("api", "", "REST API server starting", "addr", fmt.Sprintf("https://%s:%d", apiConfig.Host, apiConfig.Port))
			console.Info("api", "", "API documentation available", "url", fmt.Sprintf("https://%s:%d/api-docs", apiConfig.Host, apiConfig.Port))
		} else {
			console.Info("api", "", "REST API server starting", "addr", fmt.Sprintf("http://%s:%d", apiConfig.Host, apiConfig.Port))
			console.Info("api", "", "API documentation available", "url", fmt.Sprintf("http://%s:%d/api-docs", apiConfig.Host, apiConfig.Port))
		}
		if err := apiServer.Start(); err != nil {
			log.Fatalf("API server failed: %v", err)
		}
	}()

	// Inject notification handler and wire reload notifiers callback
	if notifHandler != nil {
		apiServer.SetNotificationHandler(notifHandler)
	}

	// Start MQTT broker if enabled
	embeddedMqtt := os.Getenv("EMBEDDED_MQTT_SERVER")
	embeddedBrokerRunning := false
	if embeddedMqtt == "" || embeddedMqtt == "yes" {
		if err := StartMqttBroker(); err != nil {
			log.Printf("Embedded MQTT broker failed to start: %v", err)
		} else {
			embeddedBrokerRunning = true
		}
	}

	// Start MQTT client if enabled
	mqttClientEnabled := os.Getenv("MQTT_CLIENT_ENABLED")
	var mqttClient *mqtt.Client
	externalBroker := os.Getenv("MQTT_URL") != ""
	if (mqttClientEnabled == "" || mqttClientEnabled == "yes") && (embeddedBrokerRunning || externalBroker) {
		mqttClient = mqtt.NewClientFromEnv(treeOps, nc)
		if err := mqttClient.Start(); err != nil {
			console.Warn("mqtt", "", "Failed to start MQTT client", "error", err)
		}
	}

	// Start server metrics collector (CPU, memory, goroutines, ingest queue).
	// Passing mqttClient as the sampler is safe even when nil.
	var sampler metrics.Sampler
	if mqttClient != nil {
		sampler = mqttClient
	}

	metricsCollector := metrics.New(nc, sampler)
	metricsCollector.SetIngestSubscription(ingestSub)
	metricsCollector.SetMetricWriter(metricWriter)

	// Don't collect metrics when in test mode.
	if os.Getenv("TEST_MODE") != "true" {
		metricsCollector.Start()
	}

	// Expose NATS browser credentials via the REST API so the frontend can
	// connect with the correct token after authenticating.
	natsWSPath := os.Getenv("NATS_WS_PATH")
	if natsWSPath == "" {
		natsWSPath = "/xact/ws"
	}
	apiServer.SetNATSBrowserConfig(api.NATSBrowserConfig{
		Username:   "browser",
		Password:   browserToken,
		NATSWSPath: natsWSPath,
		NATSWSURL:  os.Getenv("NATS_WS_URL"),
	})

	// Expose internal NATS credentials for test harness connections.
	internalNATSURL := os.Getenv("NATS_INTERNAL_URL")
	if internalNATSURL == "" {
		internalHost := natsHost
		if internalHost == "0.0.0.0" || internalHost == "::" {
			internalHost = "localhost"
		}
		internalNATSURL = fmt.Sprintf("nats://%s:%d", internalHost, natsPort)
	}
	apiServer.SetNATSInternalConfig(api.NATSInternalConfig{
		URL:      internalNATSURL,
		Username: "internal",
		Password: internalPassword,
	})

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	console.Info("", "", "Shutting down...")

	// Final save of tree config
	if persistMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := persistMgr.Stop(ctx); err != nil {
			console.Warn("", "", "Final save failed", "error", err)
		}
	}

	// Stop tag calc engine
	if tsEngine != nil {
		tsEngine.Stop()
	}

	// Stop metrics collector
	metricsCollector.Stop()

	// Stop MQTT client
	if mqttClient != nil {
		mqttClient.Stop()
	}

	// Stop shared ingest subscription and workers.
	if ingestSub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := ingestSub.Stop(ctx); err != nil {
			console.Warn("", "", "Ingest relay stop failed", "error", err)
		}
	}

	// Stop notification handler (unsubscribes from NATS)
	if notifHandler != nil {
		notifHandler.Stop()
	}

	// Stop event writer (flushes buffer)
	if eventWriter != nil {
		eventWriter.Stop()
	}

	// Stop metric writer (flushes history recorder buffer)
	if metricWriter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := metricWriter.Stop(ctx); err != nil {
			console.Warn("", "", "Metric writer flush failed", "error", err)
		}
	}

	// Stop persist store (flushes coalesced last-known values)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := nats.StopPersistStore(ctx); err != nil {
		console.Warn("", "", "Persist store flush failed", "error", err)
	}
}

func resolveDefaultPluginDir() string {
	for _, candidate := range []string{"./plugins", "../plugins"} {
		if info, err := os.Stat(filepath.Join(candidate, "widgets")); err == nil && info.IsDir() {
			return candidate
		}
	}
	return "../plugins"
}

// mustRandHex returns n random bytes encoded as a hex string. Panics on failure.
func mustRandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
