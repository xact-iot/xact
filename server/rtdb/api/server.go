// Package api provides the REST API server for RTDB
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/notifications"
	pluginauth "github.com/xact-iot/xact/plugins/auth"
	"github.com/xact-iot/xact/rtdb/ingest/rest"
	"github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/web/api"
)

// ServerConfig contains configuration for the API server
type ServerConfig struct {
	Host                     string
	Port                     int
	TLS                      TLSConfig
	ProxyPath                string
	StaticServeMode          string // "server" = serve static files, "proxy" = don't serve (VITE/NGINX does)
	StaticDir                string // Path to static files directory (e.g. "../ui/dist")
	AppVersion               string // Application version reported by /health
	ExposeNATSInternalConfig bool   // exposes full NATS credentials for explicitly enabled test harness use
	AllowedOrigins           []string
	MaxRequestBodyBytes      int64
}

// TLSConfig contains TLS configuration
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
}

// NATSBrowserConfig holds the WebSocket NATS credentials served to browsers.
type NATSBrowserConfig struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	NATSWSPath string `json:"natsWsPath"`
	NATSWSURL  string `json:"natsWsUrl,omitempty"`
}

// NATSInternalConfig holds the internal NATS credentials for test harness connections.
type NATSInternalConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Server handles REST API requests
type Server struct {
	config               ServerConfig
	router               chi.Router
	tree                 *tree.TreeWithOperations
	treeSync             *nats.TreeSync
	nc                   *natsgo.Conn
	jwtSecret            []byte
	db                   sqldb.DB
	pluginDir            string
	authPlugin           *pluginauth.AuthPlugin
	natsBrowserConfig    NATSBrowserConfig
	natsInternalConfig   NATSInternalConfig
	natsCfgMu            sync.RWMutex
	dashboardHandlers    *api.DashboardHandlers
	permissionHandlers   *api.PermissionHandlers
	logHandlers          *api.LogHandlers
	userHandlers         *api.UserHandlers
	meHandlers           *api.MeHandlers
	orgHandlers          *api.OrgHandlers
	metricHandlers       *api.MetricHandlers
	reportHandlers       *api.ReportHandlers
	notificationHandlers *api.NotificationHandlers
	tagCalcHandlers      *api.TagCalcHandlers
	scheduleHandlers     *api.ScheduleHandlers
	ingestHandler        *rest.Handler
	notifHandler         *events.NotificationHandler
	eventPublisher       *events.Publisher
}

// NewServer creates a new API server.
// pluginDir is the root directory to search for plugins (e.g. "../plugins").
// Pass an empty string to skip plugin loading.
func NewServer(config ServerConfig, treeOps *tree.TreeWithOperations, treeSync *nats.TreeSync, nc *natsgo.Conn, jwtSecret string, database sqldb.DB, pluginDir string) *Server {
	pluginDir = trustedPluginDir(pluginDir)
	s := &Server{
		config:    config,
		tree:      treeOps,
		treeSync:  treeSync,
		nc:        nc,
		jwtSecret: []byte(jwtSecret),
		db:        database,
		pluginDir: pluginDir,
	}

	// Load optional authentication plugin
	if pluginDir != "" && authPluginExecutionEnabled() {
		plugin, err := pluginauth.Load(pluginDir)
		if err != nil {
			log.Printf("auth plugin: failed to load: %v", err)
		} else if plugin != nil {
			s.authPlugin = plugin
			log.Printf("auth plugin: loaded from %s", plugin.Path())
		}
	} else if pluginDir != "" {
		log.Printf("auth plugin: runtime execution disabled; set ENABLE_AUTH_PLUGIN=yes to enable")
	}
	if database != nil {
		s.dashboardHandlers = api.NewDashboardHandlers(database, func(ctx context.Context) (string, bool) {
			claims, ok := GetClaimsFromContext(ctx)
			if !ok {
				return "", false
			}
			return claims.TenantID, claims.TenantID != ""
		})
		s.permissionHandlers = api.NewPermissionHandlers(database,
			func(ctx context.Context) ([]string, bool) {
				claims, ok := GetClaimsFromContext(ctx)
				if !ok {
					return nil, false
				}
				return claims.Roles, true
			},
			func(ctx context.Context) (string, bool) {
				claims, ok := GetClaimsFromContext(ctx)
				if !ok {
					return "", false
				}
				return claims.TenantID, claims.TenantID != ""
			},
		)
		s.logHandlers = api.NewLogHandlers(database, 1000)
		s.logHandlers.GetTenantID = func(r *http.Request) string {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok || claims.TenantID == "" {
				return "default"
			}
			return claims.TenantID
		}
		s.logHandlers.GetUserID = func(r *http.Request) int {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				return 0
			}
			id, _ := strconv.Atoi(claims.UserID)
			return id
		}
		s.logHandlers.CanAccessOrg = s.canAccessOrg
		s.logHandlers.IsSystemAdmin = func(r *http.Request) bool {
			claims, ok := GetClaimsFromContext(r.Context())
			return ok && claimsHasSystemAdmin(claims)
		}
		s.userHandlers = api.NewUserHandlers(database)
		s.userHandlers.CanAccessOrg = s.canAccessOrg
		s.userHandlers.AllowedOrgSet = s.allowedOrgSet
		s.userHandlers.IsSystemAdmin = func(r *http.Request) bool {
			claims, ok := GetClaimsFromContext(r.Context())
			return ok && claimsHasSystemAdmin(claims)
		}
		s.userHandlers.CurrentOrgName = func(r *http.Request) string {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok {
				return ""
			}
			return claims.TenantID
		}
		s.meHandlers = api.NewMeHandlers(database, func(ctx context.Context) (int, bool) {
			claims, ok := GetClaimsFromContext(ctx)
			if !ok {
				return 0, false
			}
			id, err := strconv.Atoi(claims.UserID)
			if err != nil {
				return 0, false
			}
			return id, true
		})
		s.orgHandlers = api.NewOrgHandlers(database, s.makeOrgNodeSyncer(), s.makeOrgNodeDeleter())
		s.orgHandlers.CanAccessOrg = s.canAccessOrg
		s.orgHandlers.AllowedOrgSet = s.allowedOrgSet
		s.orgHandlers.IsSystemAdmin = func(r *http.Request) bool {
			claims, ok := GetClaimsFromContext(r.Context())
			return ok && claimsHasSystemAdmin(claims)
		}
		s.orgHandlers.GetUserID = func(ctx context.Context) (int, bool) {
			claims, ok := GetClaimsFromContext(ctx)
			if !ok {
				return 0, false
			}
			id, err := strconv.Atoi(claims.UserID)
			if err != nil {
				return 0, false
			}
			return id, true
		}
		s.metricHandlers = api.NewMetricHandlers(database, func(ctx context.Context) (string, bool) {
			claims, ok := GetClaimsFromContext(ctx)
			if !ok {
				return "", false
			}
			return claims.TenantID, claims.TenantID != ""
		})
		s.reportHandlers = api.NewReportHandlers(database,
			func(ctx context.Context) (string, bool) {
				claims, ok := GetClaimsFromContext(ctx)
				if !ok {
					return "", false
				}
				return claims.TenantID, claims.TenantID != ""
			},
			func(path string) (string, bool) {
				// Resolve RTDB tag to its current string value
				leaf, err := treeOps.FindLeaf("/" + strings.ReplaceAll(path, ".", "/"))
				if err != nil {
					return "", false
				}
				if ln, ok := leaf.(interface{ GetAnyValue() any }); ok {
					return fmt.Sprintf("%v", ln.GetAnyValue()), true
				}
				return "", false
			},
		)
		s.notificationHandlers = api.NewNotificationHandlers(database, func(r *http.Request) string {
			claims, ok := GetClaimsFromContext(r.Context())
			if !ok || claims.TenantID == "" {
				return "default"
			}
			return claims.TenantID
		}, func(ctx context.Context, org string) error {
			if s.notifHandler != nil {
				cfg, err := notifications.LoadChannelConfig(ctx, database, org)
				if err != nil {
					return err
				}
				emailCfg := events.EmailConfig{
					Host:     cfg.Email.Host,
					Port:     cfg.Email.Port,
					Username: cfg.Email.Username,
					Password: cfg.Email.Password,
					From:     cfg.Email.From,
					UseTLS:   cfg.Email.UseTLS,
				}
				telegramCfg := events.TelegramConfig{
					BotToken: cfg.Telegram.BotToken,
				}
				s.notifHandler.ReloadNotifiers(emailCfg, telegramCfg)
			}
			return nil
		})
		s.ingestHandler = rest.New(nc, database)
		s.ingestHandler.CurrentOrg = currentOrgFromRequest
		s.ingestHandler.Audit = func(r *http.Request, orgName, action string, params map[string]any) {
			claims, _ := GetClaimsFromContext(r.Context())
			userID := 0
			if claims != nil {
				userID, _ = strconv.Atoi(claims.UserID)
			}
			s.auditSecurityEvent(r.Context(), orgName, userID, events.Info, "api-keys", action, params)
		}
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	s.router = chi.NewRouter()

	s.router.Use(securityHeaders)
	s.router.Use(limitRequestBody(s.maxRequestBodyBytes()))
	if len(s.config.AllowedOrigins) > 0 {
		s.router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.config.AllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With", "X-API-Key"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}

	// Middleware
	// s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)

	// Add the global prefix if defined
	if s.config.ProxyPath != "" {
		s.router.Route(s.config.ProxyPath, func(r chi.Router) {
			s.buildRoutes(r, s.config.ProxyPath)
		})
	} else {
		s.buildRoutes(s.router, "")
	}
	// Get here when there is no proxy and default URL
	s.router.Get("/", s.serveIndexFallback)

	// chi.Walk(s.router, func(method string, route string, handler http.Handler,
	// 	middlewares ...func(http.Handler) http.Handler) error {

	// 	fmt.Printf("%s %s\n", method, route)
	// 	return nil
	// })

}

// setupRoutesUnder sets up routes under a specific path prefix
func (s *Server) buildRoutes(r chi.Router, prefix string) {
	// Public routes (no JWT required)
	r.Group(func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/api-docs", s.handleAPIDocs)
		r.Post("/login", s.handleLogin)
		r.Get("/api/v1/bootstrap/admin", s.handleBootstrapAdminStatus)
		r.Post("/api/v1/bootstrap/admin/password", s.handleSetBootstrapAdminPassword)
		// Plugin discovery and static file serving (widgets + map layers + themes)
		r.Get("/api/v1/plugins/widgets", s.handleListWidgetPlugins)
		r.Get("/plugins/widgets/{filename}", s.handleServeWidgetPlugin)
		r.Get("/api/v1/plugins/map-layer", s.handleListMapLayerPlugins)
		r.Get("/plugins/map-layer/{filename}", s.handleServeMapLayerPlugin)
		r.Get("/api/v1/plugins/themes", s.handleListThemePlugins)
		r.Get("/plugins/themes/{filename}", s.handleServeThemePlugin)
		// Device data ingest - authenticated via API key, not JWT
		if s.ingestHandler != nil {
			r.Post("/api/v1/ingest/{tenant}/{devicetype}/{devicename}", s.ingestHandler.HandleIngest)
			r.Post("/api/v1/ingest/{tenant}/zone/{zone}/{devicetype}/{devicename}", s.ingestHandler.HandleIngestWithZone)
		}

		// Static file serving (only when server serves files, not proxy/VITE)
		// Serve files when StaticServeMode is "server" or "" (default/unset)
		if s.config.StaticServeMode != "proxy" && s.config.StaticDir != "" {
			assetsPrefix := prefix + "/assets"
			assetsFs := http.StripPrefix(assetsPrefix+"/", http.FileServer(http.Dir(s.config.StaticDir+"/assets")))
			r.Get("/assets/{path:.*}", assetsFs.ServeHTTP)

			// Serve icons, favicon, logo, etc. directly
			// fs := http.FileServer(http.Dir(s.config.StaticDir))

			fs := http.StripPrefix(prefix+"/", http.FileServer(http.Dir(s.config.StaticDir)))
			r.Get("/icons/{path:.*}", fs.ServeHTTP)
			r.Get("/manual/{path:.*}", fs.ServeHTTP)
			r.Get("/test/{path:.*}", fs.ServeHTTP)
			r.Get("/logo.svg", fs.ServeHTTP)
			r.Get("/favicon.svg", fs.ServeHTTP)

			r.Get("/", s.serveIndexFallback)
		}
	})

	// Protected routes (JWT required)
	r.Group(func(r chi.Router) {
		r.Use(JWTAuth(s.jwtSecret, s.db))
		r.Use(JSONContentType)

		// Auth helpers
		r.Get("/api/v1/auth/my-orgs", s.handleMyOrgs)
		r.Post("/api/v1/auth/switch-org", s.handleSwitchOrg)

		// System
		r.Get("/api/v1/system/nats-config", s.handleNATSConfig)
		if s.config.ExposeNATSInternalConfig {
			r.With(s.requireSystemAdmin()).
				Get("/api/v1/system/nats-internal-config", s.handleNATSInternalConfig)
		}

		// Node operations
		r.Route("/api/v1/nodes", func(r chi.Router) {
			r.Use(OrgSandbox(prefix + "/api/v1/nodes/"))
			r.Group(func(r chi.Router) {
				r.Use(s.requireUIPermission("nodes", "read"))
				r.Get("/*", s.handleGetNode)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.requireUIPermission("nodes", "write"))
				r.Post("/", s.handleCreateNode)
				r.Put("/*", s.handleUpdateNode)
				r.Delete("/*", s.handleDeleteNode)
			})
		})

		// Tag operations
		r.Route("/api/v1/tags", func(r chi.Router) {
			r.Use(OrgSandbox(prefix + "/api/v1/tags/"))
			r.Group(func(r chi.Router) {
				r.Use(s.requireUIPermission("tags", "read"))
				r.Get("/*", s.handleGetTag)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.requireUIPermission("tags", "write"))
				r.Post("/", s.handleCreateTag)
				r.Put("/*", s.handleUpdateTag)
				r.Delete("/*", s.handleDeleteTag)
			})
		})

		// Tag pipeline debug (separate route to avoid wildcard conflict)
		r.Group(func(r chi.Router) {
			r.Use(OrgSandbox(prefix + "/api/v1/debug/tags/"))
			r.Use(s.requireUIPermission("tags", "write"))
			r.Post("/api/v1/debug/tags/*", s.handleDebugTagPipeline)
		})

		r.With(s.requireUIPermission("tags", "write")).
			Post("/api/v1/commands/{deviceName}", s.handleCommand)

		// Block schema discovery
		r.Get("/api/v1/blocks/schemas", s.handleGetBlockSchemas)

		// Dashboard operations (requires database)
		if s.dashboardHandlers != nil {
			registerDashboardRoutes := func(r chi.Router) {
				// Any authenticated user can load dashboard navigation/content.
				r.Get("/", s.dashboardHandlers.HandleListDashboards)
				r.Get("/{id}", s.dashboardHandlers.HandleGetDashboard)
				r.Group(func(r chi.Router) {
					r.Use(s.requireUIPermission("dashboards-setup", "edit"))
					r.Post("/", s.dashboardHandlers.HandleCreateDashboard)
					r.Put("/{id}", s.dashboardHandlers.HandleUpdateDashboard)
					r.Delete("/{id}", s.dashboardHandlers.HandleDeleteDashboard)
				})
			}
			r.Route("/api/v1/dashboards", registerDashboardRoutes)
			// Compatibility alias for clients that have not moved to dashboard terminology yet.
			r.Route("/api/v1/panels", registerDashboardRoutes)
		}

		// Permission operations (requires database)
		if s.permissionHandlers != nil {
			r.Route("/api/v1/permissions", func(r chi.Router) {
				// Open - fetching your own permissions
				r.Get("/", s.permissionHandlers.HandleGetMyPermissions)
				r.With(s.requireAnyUIPermission("permissions", "view", "manage")).
					Get("/roles", s.permissionHandlers.HandleListRolePermissions)
				r.With(s.requireUIPermission("permissions", "manage")).
					Put("/roles/{role}", s.permissionHandlers.HandleUpdateRolePermissions)
			})
		}

		// Log query (requires database)
		if s.logHandlers != nil {
			r.With(s.requireUIPermission("logs", "read")).
				Get("/api/v1/logs", s.logHandlers.HandleQueryLogs)
			r.With(s.requireUIPermission("logs", "write")).
				Post("/api/v1/logs", s.logHandlers.HandleCreateLog)
		}

		// API key management for the current organisation.
		if s.ingestHandler != nil {
			r.Route("/api/v1/api-keys", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("organisations", "view", "change")).
					Get("/", s.ingestHandler.HandleListAPIKeys)
				r.With(s.requireUIPermission("organisations", "change")).
					Post("/", s.ingestHandler.HandleCreateAPIKey)
				r.With(s.requireUIPermission("organisations", "change")).
					Delete("/{id}", s.ingestHandler.HandleDeleteAPIKey)
			})
		}

		// User management (requires database)
		if s.userHandlers != nil {
			r.With(s.requireAnyUIPermission("users", "view", "manage")).
				Get("/api/v1/roles", s.userHandlers.HandleListRoles)
			r.Route("/api/v1/users", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("users", "view", "manage")).
					Get("/", s.userHandlers.HandleListUsers)
				r.With(s.requireUIPermission("users", "manage")).
					Post("/", s.userHandlers.HandleCreateUser)
				r.With(s.requireAnyUIPermission("users", "view", "manage")).
					Get("/{id}", s.userHandlers.HandleGetUser)
				r.With(s.requireUIPermission("users", "manage")).
					Put("/{id}", s.userHandlers.HandleUpdateUser)
				r.With(s.requireUIPermission("users", "manage")).
					Post("/{id}/reset-password", s.userHandlers.HandleResetPassword)
			})
		}

		// Current user profile (requires database)
		if s.meHandlers != nil {
			r.Route("/api/v1/me", func(r chi.Router) {
				r.Get("/", s.meHandlers.HandleGetMe)
				r.With(s.requireUIPermission("profile", "change")).
					Put("/", s.meHandlers.HandleUpdateMe)
				r.With(s.requireUIPermission("profile", "change")).
					Post("/change-password", s.meHandlers.HandleChangePassword)
			})
		}

		// Metrics (time-series) query endpoints (requires database)
		if s.metricHandlers != nil {
			r.Get("/api/v1/metrics/*", func(w http.ResponseWriter, req *http.Request) {
				rest := chi.URLParam(req, "*")
				if strings.HasSuffix(rest, "/since") {
					s.metricHandlers.HandleQuerySince(w, req)
				} else {
					s.metricHandlers.HandleQueryRange(w, req)
				}
			})
		}

		// PDF report templates (requires database)
		if s.reportHandlers != nil {
			r.Route("/api/v1/reports/templates", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("reports", "view", "manage")).
					Get("/", s.reportHandlers.HandleListTemplates)
				r.With(s.requireUIPermission("reports", "manage")).
					Post("/", s.reportHandlers.HandleCreateTemplate)
				r.With(s.requireAnyUIPermission("reports", "view", "manage")).
					Get("/{id}", s.reportHandlers.HandleGetTemplate)
				r.With(s.requireUIPermission("reports", "manage")).
					Put("/{id}", s.reportHandlers.HandleUpdateTemplate)
				r.With(s.requireUIPermission("reports", "manage")).
					Delete("/{id}", s.reportHandlers.HandleDeleteTemplate)
				r.With(s.requireAnyUIPermission("reports", "view", "manage")).
					Post("/{id}/preview", s.reportHandlers.HandlePreviewTemplate)
			})
			r.With(s.requireAnyUIPermission("reports", "view", "manage")).
				Post("/api/v1/reports/generate", s.reportHandlers.HandleGeneratePDF)
		}

		// Notification management (requires database)
		if s.notificationHandlers != nil {
			r.Route("/api/v1/notifications", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("notifications", "view", "manage")).
					Get("/profiles", s.notificationHandlers.HandleListProfiles)
				r.With(s.requireUIPermission("notifications", "manage")).
					Post("/profiles", s.notificationHandlers.HandleCreateProfile)
				r.With(s.requireAnyUIPermission("notifications", "view", "manage")).
					Get("/profiles/{id}", s.notificationHandlers.HandleGetProfile)
				r.With(s.requireUIPermission("notifications", "manage")).
					Put("/profiles/{id}", s.notificationHandlers.HandleUpdateProfile)
				r.With(s.requireUIPermission("notifications", "manage")).
					Delete("/profiles/{id}", s.notificationHandlers.HandleDeleteProfile)
				r.With(s.requireAnyUIPermission("notifications", "view", "manage")).
					Get("/channels", s.notificationHandlers.HandleGetChannels)
				r.With(s.requireUIPermission("notifications", "manage")).
					Put("/channels", s.notificationHandlers.HandleSaveChannels)
			})
		}

		// Tag scripts (requires database)
		if s.tagCalcHandlers != nil {
			r.Route("/api/v1/tagcalcs", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("tagcalcs", "view", "manage")).
					Get("/", s.tagCalcHandlers.HandleList)
				r.With(s.requireUIPermission("tagcalcs", "manage")).
					Post("/", s.tagCalcHandlers.HandleCreate)
				r.With(s.requireUIPermission("tagcalcs", "manage")).
					Post("/test", s.tagCalcHandlers.HandleTest)
				r.With(s.requireAnyUIPermission("tagcalcs", "view", "manage")).
					Get("/{id}", s.tagCalcHandlers.HandleGet)
				r.With(s.requireUIPermission("tagcalcs", "manage")).
					Put("/{id}", s.tagCalcHandlers.HandleUpdate)
				r.With(s.requireUIPermission("tagcalcs", "manage")).
					Delete("/{id}", s.tagCalcHandlers.HandleDelete)
			})
		}

		// Scheduler (requires database)
		if s.scheduleHandlers != nil {
			r.Route("/api/v1/schedules", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("scheduler", "view", "manage")).
					Get("/", s.scheduleHandlers.HandleList)
				r.With(s.requireUIPermission("scheduler", "manage")).
					Post("/", s.scheduleHandlers.HandleCreate)
				r.With(s.requireAnyUIPermission("scheduler", "view", "manage")).
					Get("/{id}", s.scheduleHandlers.HandleGet)
				r.With(s.requireUIPermission("scheduler", "manage")).
					Put("/{id}", s.scheduleHandlers.HandleUpdate)
				r.With(s.requireUIPermission("scheduler", "manage")).
					Delete("/{id}", s.scheduleHandlers.HandleDelete)
				r.With(s.requireUIPermission("scheduler", "manage")).
					Post("/{id}/run", s.scheduleHandlers.HandleRunNow)
				r.With(s.requireAnyUIPermission("scheduler", "view", "manage")).
					Get("/{id}/history", s.scheduleHandlers.HandleHistory)
			})
		}

		// Organisation management (requires database)
		if s.orgHandlers != nil {
			r.Route("/api/v1/organisations", func(r chi.Router) {
				r.With(s.requireAnyUIPermission("organisations", "view", "change")).
					Get("/", s.orgHandlers.HandleListOrganisations)
				r.With(s.requireUIPermission("organisations", "change"), s.requireSystemAdmin()).
					Post("/", s.orgHandlers.HandleCreateOrganisation)
				r.With(s.requireAnyUIPermission("organisations", "view", "change"), s.requireTargetOrgParam("name")).
					Get("/{name}", s.orgHandlers.HandleGetOrganisation)
				r.With(s.requireUIPermission("organisations", "change"), s.requireTargetOrgParam("name")).
					Put("/{name}", s.orgHandlers.HandleUpdateOrganisation)
				r.With(s.requireUIPermission("organisations", "change"), s.requireSystemAdmin()).
					Delete("/{name}", s.orgHandlers.HandleDeleteOrganisation)
			})
		}
	})
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	log.Printf("API server starting on %s", addr)

	if s.config.TLS.Enabled {
		if s.config.TLS.CertFile == "" || s.config.TLS.KeyFile == "" {
			log.Printf("TLS enabled but no certificates provided, using self-signed")
			return s.startWithSelfSignedTLS(addr)
		}
		log.Printf("Using TLS certificates: %s, %s", s.config.TLS.CertFile, s.config.TLS.KeyFile)
		return s.newHTTPServer(addr).ListenAndServeTLS(s.config.TLS.CertFile, s.config.TLS.KeyFile)
	}

	return s.newHTTPServer(addr).ListenAndServe()
}

func (s *Server) newHTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func (s *Server) maxRequestBodyBytes() int64 {
	if s.config.MaxRequestBodyBytes > 0 {
		return s.config.MaxRequestBodyBytes
	}
	return 8 << 20
}

// startWithSelfSignedTLS starts the server with self-signed certificates
func (s *Server) startWithSelfSignedTLS(addr string) error {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate self-signed cert: %w", err)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	return server.ListenAndServeTLS("", "")
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	// Currently no-op - could add graceful shutdown if needed
	return nil
}

// Router returns the chi router for testing
func (s *Server) Router() chi.Router {
	return s.router
}

// SetTagCalcHandlers injects the tag calc handlers after the engine is started.
// Must be called before the server starts accepting requests.
func (s *Server) SetTagCalcHandlers(h *api.TagCalcHandlers) {
	s.tagCalcHandlers = h
	s.setupRoutes()
}

// SetScheduleHandlers injects the scheduler handlers after the engine is started.
// Must be called before the server starts accepting requests.
func (s *Server) SetScheduleHandlers(h *api.ScheduleHandlers) {
	s.scheduleHandlers = h
	s.setupRoutes()
}

// SetNotificationHandler injects the notification handler so channels can be reloaded.
func (s *Server) SetNotificationHandler(h *events.NotificationHandler) {
	s.notifHandler = h
}

// SetEventsPublisher injects the publisher used by REST endpoints that need to
// add entries to the event log.
func (s *Server) SetEventsPublisher(p *events.Publisher) {
	s.eventPublisher = p
	if s.logHandlers != nil {
		s.logHandlers.Publisher = p
	}
}

// SetNATSBrowserConfig sets the WebSocket NATS credentials served to browsers.
func (s *Server) SetNATSBrowserConfig(cfg NATSBrowserConfig) {
	s.natsCfgMu.Lock()
	s.natsBrowserConfig = cfg
	s.natsCfgMu.Unlock()
}

// SetNATSInternalConfig sets the internal NATS credentials for test harness connections.
func (s *Server) SetNATSInternalConfig(cfg NATSInternalConfig) {
	s.natsCfgMu.Lock()
	s.natsInternalConfig = cfg
	s.natsCfgMu.Unlock()
}

// JSONContentType middleware sets Content-Type to application/json
func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// handleNATSConfig returns the WebSocket NATS credentials for browser clients.
func (s *Server) handleNATSConfig(w http.ResponseWriter, r *http.Request) {
	s.natsCfgMu.RLock()
	cfg := s.natsBrowserConfig
	s.natsCfgMu.RUnlock()

	if cfg.NATSWSURL == "" && cfg.NATSWSPath == "" && s.config.StaticServeMode != "proxy" {
		cfg.NATSWSURL = s.directNATSWebSocketURL(r)
	}

	json.NewEncoder(w).Encode(cfg)
}

func (s *Server) directNATSWebSocketURL(r *http.Request) string {
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	scheme := "ws"
	if r.Header.Get("X-Forwarded-Proto") == "https" || s.config.TLS.Enabled {
		scheme = "wss"
	}
	port := os.Getenv("NATS_WS_PORT")
	if port == "" {
		port = "9222"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, hostname, port)
}

// handleNATSInternalConfig returns the internal NATS credentials for test harness connections.
func (s *Server) handleNATSInternalConfig(w http.ResponseWriter, r *http.Request) {
	s.natsCfgMu.RLock()
	cfg := s.natsInternalConfig
	s.natsCfgMu.RUnlock()
	json.NewEncoder(w).Encode(cfg)
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "healthy",
		"service":    "xact-rtdb-api",
		"timestamp":  time.Now().Unix(),
		"timezone":   serverTimezone(),
		"appVersion": strings.TrimSpace(s.config.AppVersion),
		"goVersion":  runtime.Version(),
	})
}

// serverTimezone returns the IANA timezone name (e.g. "America/New_York").
// Go's time.Local.String() often returns "Local" which is unusable by browsers,
// so we resolve it from the OS instead.
func serverTimezone() string {
	// Explicit TZ env var takes priority on all platforms.
	if tz := os.Getenv("TZ"); tz != "" && tz != "Local" {
		return tz
	}

	// Linux / macOS: read from OS files.
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		const prefix = "zoneinfo/"
		if i := strings.Index(target, prefix); i != -1 {
			return target[i+len(prefix):]
		}
	}

	// Windows: query the OS timezone ID and convert to IANA.
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("powershell", "-NoProfile", "-Command",
			`(Get-TimeZone).Id`).Output(); err == nil {
			winID := strings.TrimSpace(string(out))
			if iana, ok := windowsToIANA[winID]; ok {
				return iana
			}
		}
	}

	// Last resort: abbreviated zone name from Go runtime (e.g. "EST").
	name, _ := time.Now().Zone()
	return name
}

// windowsToIANA maps Windows timezone IDs to IANA names (source: Unicode CLDR).
var windowsToIANA = map[string]string{
	"UTC":                             "UTC",
	"Dateline Standard Time":          "Etc/GMT+12",
	"Hawaiian Standard Time":          "Pacific/Honolulu",
	"Alaskan Standard Time":           "America/Anchorage",
	"Pacific Standard Time":           "America/Los_Angeles",
	"Mountain Standard Time":          "America/Denver",
	"US Mountain Standard Time":       "America/Phoenix",
	"Central Standard Time":           "America/Chicago",
	"Canada Central Standard Time":    "America/Regina",
	"Central America Standard Time":   "America/Guatemala",
	"Eastern Standard Time":           "America/New_York",
	"US Eastern Standard Time":        "America/Indianapolis",
	"SA Pacific Standard Time":        "America/Bogota",
	"Atlantic Standard Time":          "America/Halifax",
	"SA Western Standard Time":        "America/La_Paz",
	"Newfoundland Standard Time":      "America/St_Johns",
	"E. South America Standard Time":  "America/Sao_Paulo",
	"SA Eastern Standard Time":        "America/Cayenne",
	"Argentina Standard Time":         "America/Buenos_Aires",
	"Greenland Standard Time":         "America/Godthab",
	"Azores Standard Time":            "Atlantic/Azores",
	"Cape Verde Standard Time":        "Atlantic/Cape_Verde",
	"GMT Standard Time":               "Europe/London",
	"Greenwich Standard Time":         "Atlantic/Reykjavik",
	"W. Europe Standard Time":         "Europe/Berlin",
	"Central European Standard Time":  "Europe/Warsaw",
	"Romance Standard Time":           "Europe/Paris",
	"Central Europe Standard Time":    "Europe/Budapest",
	"W. Central Africa Standard Time": "Africa/Lagos",
	"GTB Standard Time":               "Europe/Bucharest",
	"E. Europe Standard Time":         "Europe/Chisinau",
	"South Africa Standard Time":      "Africa/Johannesburg",
	"FLE Standard Time":               "Europe/Kiev",
	"Israel Standard Time":            "Asia/Jerusalem",
	"Egypt Standard Time":             "Africa/Cairo",
	"E. Africa Standard Time":         "Africa/Nairobi",
	"Arabic Standard Time":            "Asia/Baghdad",
	"Arab Standard Time":              "Asia/Riyadh",
	"Russian Standard Time":           "Europe/Moscow",
	"Iran Standard Time":              "Asia/Tehran",
	"Arabian Standard Time":           "Asia/Dubai",
	"Azerbaijan Standard Time":        "Asia/Baku",
	"Afghanistan Standard Time":       "Asia/Kabul",
	"West Asia Standard Time":         "Asia/Tashkent",
	"India Standard Time":             "Asia/Kolkata",
	"Sri Lanka Standard Time":         "Asia/Colombo",
	"Nepal Standard Time":             "Asia/Kathmandu",
	"Central Asia Standard Time":      "Asia/Almaty",
	"Bangladesh Standard Time":        "Asia/Dhaka",
	"SE Asia Standard Time":           "Asia/Bangkok",
	"China Standard Time":             "Asia/Shanghai",
	"Taipei Standard Time":            "Asia/Taipei",
	"Singapore Standard Time":         "Asia/Singapore",
	"W. Australia Standard Time":      "Australia/Perth",
	"Tokyo Standard Time":             "Asia/Tokyo",
	"Korea Standard Time":             "Asia/Seoul",
	"Cen. Australia Standard Time":    "Australia/Adelaide",
	"AUS Central Standard Time":       "Australia/Darwin",
	"AUS Eastern Standard Time":       "Australia/Sydney",
	"E. Australia Standard Time":      "Australia/Brisbane",
	"West Pacific Standard Time":      "Pacific/Port_Moresby",
	"Tasmania Standard Time":          "Australia/Hobart",
	"Central Pacific Standard Time":   "Pacific/Guadalcanal",
	"New Zealand Standard Time":       "Pacific/Auckland",
	"Fiji Standard Time":              "Pacific/Fiji",
	"Tonga Standard Time":             "Pacific/Tongatapu",
	"Samoa Standard Time":             "Pacific/Apia",
	"Mexico Standard Time":            "America/Mexico_City",
	"Mountain Standard Time (Mexico)": "America/Chihuahua",
	"Pacific Standard Time (Mexico)":  "America/Tijuana",
	"Turkey Standard Time":            "Europe/Istanbul",
	"Pakistan Standard Time":          "Asia/Karachi",
	"Morocco Standard Time":           "Africa/Casablanca",
}

// Bootstrap re-syncs all organisations from the database into the RTDB tree.
// Call this once after the tree has been restored from persistence and the
// onChange callback has been set, so that bounding-box tag values are
// re-populated and the tree is ready to serve live data.
func (s *Server) Bootstrap(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	orgs, err := s.db.ListOrganisations(ctx)
	if err != nil {
		return fmt.Errorf("org bootstrap: %w", err)
	}
	syncer := s.makeOrgNodeSyncer()
	for i := range orgs {
		syncer(orgs[i].Name, orgs[i].DisplayName, orgs[i].Area)
	}
	log.Printf("org bootstrap: synced %d organisation(s) into RTDB", len(orgs))
	return nil
}

// makeOrgNodeSyncer returns a callback that keeps the RTDB tree in sync with
// the organisation record. It is called after every successful org create/update.
func (s *Server) makeOrgNodeSyncer() func(name, displayName string, area *sqldb.OrgArea) {
	return func(name, displayName string, area *sqldb.OrgArea) {
		// Ensure the top-level org node exists (idempotent; also creates meta + coord tags)
		if err := s.tree.CreateOrganisationNode(name, ""); err != nil {
			log.Printf("org node sync %q: create: %v", name, err)
			return
		}

		// Set the node description to the display name
		node, err := s.tree.FindNode(name)
		if err != nil {
			log.Printf("org node sync %q: find: %v", name, err)
			return
		}
		node.SetDescription(displayName)

		// Publish node change to NATS
		if s.treeSync != nil {
			if err := s.treeSync.PublishChange(name, node); err != nil {
				log.Printf("org node sync %q: publish node: %v", name, err)
			}
		}

		// Write bounding-box coordinate tags.
		// SetLeafValue fires onChange, which publishes to NATS and marks the
		// tree dirty for persistence - no separate manual publish needed.
		if area != nil {
			coords := []struct {
				tag string
				val float64
			}{
				{"north", area.North},
				{"south", area.South},
				{"east", area.East},
				{"west", area.West},
			}
			for _, c := range coords {
				tagPath := name + "/meta/" + c.tag
				if err := s.tree.SetLeafValue(tagPath, c.val); err != nil {
					log.Printf("org node sync %q: set %s: %v", name, c.tag, err)
				}
			}
		}
	}
}

// makeOrgNodeDeleter returns a callback that removes an organisation's RTDB
// subtree when the organisation is deleted from the database.
func (s *Server) makeOrgNodeDeleter() func(name string) {
	return func(name string) {
		if err := s.tree.DeleteNode(name); err != nil {
			log.Printf("org node delete %q: %v", name, err)
		}
	}
}

// handleAPIDocs returns API documentation
func (s *Server) handleAPIDocs(w http.ResponseWriter, r *http.Request) {
	docs := map[string]interface{}{
		"title":   "XACT REST API",
		"version": "1.0.0",
		"endpoints": []map[string]string{
			{"method": "GET", "path": "/health", "description": "Health check"},
			{"method": "POST", "path": "/login", "description": "Login (dev mode - any credentials accepted)"},
			{"method": "POST", "path": "/api/v1/nodes/", "description": "Create a new current-organisation node"},
			{"method": "GET", "path": "/api/v1/nodes/*path", "description": "Get current-organisation node metadata and children"},
			{"method": "PUT", "path": "/api/v1/nodes/*path", "description": "Update current-organisation node metadata"},
			{"method": "DELETE", "path": "/api/v1/nodes/*path", "description": "Delete current-organisation node (cascade)"},
			{"method": "POST", "path": "/api/v1/tags/", "description": "Create a new current-organisation tag"},
			{"method": "GET", "path": "/api/v1/tags/*path", "description": "Get current-organisation tag metadata and value"},
			{"method": "PUT", "path": "/api/v1/tags/*path", "description": "Update current-organisation tag metadata/value"},
			{"method": "DELETE", "path": "/api/v1/tags/*path", "description": "Delete current-organisation tag"},
			{"method": "GET", "path": "/api/v1/api-keys", "description": "List API keys for the current organisation"},
			{"method": "POST", "path": "/api/v1/api-keys", "description": "Create an API key for the current organisation"},
			{"method": "DELETE", "path": "/api/v1/api-keys/{id}", "description": "Delete an API key from the current organisation"},
		},
	}
	json.NewEncoder(w).Encode(docs)
}

// serveIndexFallback serves index.html for SPA routing (fallback for non-API paths)
func (s *Server) serveIndexFallback(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	indexPath := filepath.Join(s.config.StaticDir, "index.html")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}
