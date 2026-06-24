package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	natsgo "github.com/nats-io/nats.go"
)

const benchmarkAPIKeyName = "XACT Benchmark"

type payload map[string]map[string]any

type config struct {
	method        string
	mode          string
	messages      int64
	duration      time.Duration
	rate          float64
	concurrency   int
	devices       int
	tenant        string
	zone          string
	deviceType    string
	provision     bool
	provisionOnly bool
	provisionWait time.Duration
	reportEvery   time.Duration

	restURL      string
	restAPIKey   string
	restUsername string
	restPassword string
	restInsecure bool
	timeout      time.Duration

	mqttURL      string
	mqttUsername string
	mqttPassword string
	mqttClientID string
	mqttQoS      int

	natsURL        string
	natsUsername   string
	natsPassword   string
	natsFlushEvery int64
}

type sender interface {
	Send(ctx context.Context, deviceName string, p payload) error
	Close() error
	Name() string
}

type result struct {
	produced int64
	ok       int64
	failed   int64
	elapsed  time.Duration
	firstErr error
}

func main() {
	cfg := parseFlags()
	if err := cfg.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	s, err := newSender(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", cfg.method, err)
		os.Exit(1)
	}
	defer s.Close()

	printConfig(cfg, s.Name())

	if cfg.provision {
		if err := provision(ctx, cfg, s); err != nil {
			fmt.Fprintf(os.Stderr, "provision: %v\n", err)
			os.Exit(1)
		}
		if cfg.provisionWait > 0 {
			time.Sleep(cfg.provisionWait)
		}
	}
	if cfg.provisionOnly {
		fmt.Println("provision complete")
		return
	}

	res := runBenchmark(ctx, cfg, s)
	printResult(res)
	if res.failed > 0 {
		os.Exit(1)
	}
}

func parseFlags() config {
	cfg := config{
		method:         "nats",
		mode:           "burst",
		messages:       10000,
		duration:       30 * time.Second,
		concurrency:    maxInt(1, runtime.NumCPU()),
		devices:        100,
		tenant:         "default",
		deviceType:     "BENCH",
		provision:      true,
		provisionWait:  2 * time.Second,
		reportEvery:    5 * time.Second,
		restURL:        envDefault("XACT_URL", "https://127.0.0.1:8443/xact"),
		restAPIKey:     os.Getenv("XACT_API_KEY"),
		restUsername:   envDefault("XACT_USERNAME", "admin"),
		restPassword:   envDefault("XACT_PASSWORD", "admin"),
		restInsecure:   true,
		timeout:        10 * time.Second,
		mqttURL:        envDefault("MQTT_BROKER", "tcp://127.0.0.1:1883"),
		mqttUsername:   envDefault("MQTT_USERNAME", "benchmark"),
		mqttPassword:   envDefault("MQTT_BROKER_PASSWORD", "xact"),
		mqttClientID:   fmt.Sprintf("xact-benchmark-%d", time.Now().UnixNano()),
		mqttQoS:        1,
		natsURL:        envDefault("NATS_URL", natsgo.DefaultURL),
		natsUsername:   envDefault("NATS_USERNAME", "internal"),
		natsPassword:   os.Getenv("NATS_INTERNAL_PASSWORD"),
		natsFlushEvery: 1000,
	}

	flag.StringVar(&cfg.method, "method", cfg.method, "ingest method: mqtt, rest, or nats")
	flag.StringVar(&cfg.mode, "mode", cfg.mode, "run mode: burst or sustained")
	flag.Int64Var(&cfg.messages, "messages", cfg.messages, "messages to send in burst mode")
	flag.DurationVar(&cfg.duration, "duration", cfg.duration, "duration for sustained mode")
	flag.Float64Var(&cfg.rate, "rate", cfg.rate, "target messages/second; 0 means maximum possible")
	flag.IntVar(&cfg.concurrency, "concurrency", cfg.concurrency, "parallel send workers")
	flag.IntVar(&cfg.devices, "devices", cfg.devices, "number of benchmark devices to cycle through")
	flag.StringVar(&cfg.tenant, "tenant", cfg.tenant, "organisation/tenant name")
	flag.StringVar(&cfg.zone, "zone", cfg.zone, "optional zone name")
	flag.StringVar(&cfg.deviceType, "device-type", cfg.deviceType, "device type used in ingest route")
	flag.BoolVar(&cfg.provision, "provision", cfg.provision, "send deterministic provisioning payloads before benchmark")
	flag.BoolVar(&cfg.provisionOnly, "provision-only", cfg.provisionOnly, "send provisioning payloads and exit")
	flag.DurationVar(&cfg.provisionWait, "provision-wait", cfg.provisionWait, "wait after provisioning before benchmark")
	flag.DurationVar(&cfg.reportEvery, "report-every", cfg.reportEvery, "progress report interval; 0 disables progress")

	flag.StringVar(&cfg.restURL, "rest-url", cfg.restURL, "XACT REST base URL, including /xact when used")
	flag.StringVar(&cfg.restAPIKey, "api-key", cfg.restAPIKey, "REST ingest API key; if empty, login is used to create/reuse one")
	flag.StringVar(&cfg.restUsername, "username", cfg.restUsername, "XACT username for automatic API key setup")
	flag.StringVar(&cfg.restPassword, "password", cfg.restPassword, "XACT password for automatic API key setup")
	flag.BoolVar(&cfg.restInsecure, "rest-insecure", cfg.restInsecure, "skip TLS certificate verification for REST")
	flag.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "network operation timeout")

	flag.StringVar(&cfg.mqttURL, "mqtt-url", cfg.mqttURL, "MQTT broker URL")
	flag.StringVar(&cfg.mqttUsername, "mqtt-username", cfg.mqttUsername, "MQTT username")
	flag.StringVar(&cfg.mqttPassword, "mqtt-password", cfg.mqttPassword, "MQTT password")
	flag.StringVar(&cfg.mqttClientID, "mqtt-client-id", cfg.mqttClientID, "MQTT client ID")
	flag.IntVar(&cfg.mqttQoS, "mqtt-qos", cfg.mqttQoS, "MQTT publish QoS")

	flag.StringVar(&cfg.natsURL, "nats-url", cfg.natsURL, "NATS server URL")
	flag.StringVar(&cfg.natsUsername, "nats-username", cfg.natsUsername, "NATS username; credentials are used only when username and password are both set")
	flag.StringVar(&cfg.natsPassword, "nats-password", cfg.natsPassword, "NATS password")
	flag.Int64Var(&cfg.natsFlushEvery, "nats-flush-every", cfg.natsFlushEvery, "flush NATS connection every N messages; 0 flushes only at the end")

	flag.Parse()
	return cfg
}

func (cfg config) validate() error {
	switch cfg.method {
	case "mqtt", "rest", "nats":
	default:
		return fmt.Errorf("unknown method %q", cfg.method)
	}
	switch cfg.mode {
	case "burst", "sustained":
	default:
		return fmt.Errorf("unknown mode %q", cfg.mode)
	}
	if cfg.messages <= 0 && cfg.mode == "burst" && !cfg.provisionOnly {
		return errors.New("messages must be > 0 in burst mode")
	}
	if cfg.duration <= 0 && cfg.mode == "sustained" && !cfg.provisionOnly {
		return errors.New("duration must be > 0 in sustained mode")
	}
	if cfg.rate < 0 {
		return errors.New("rate cannot be negative")
	}
	if cfg.concurrency <= 0 {
		return errors.New("concurrency must be > 0")
	}
	if cfg.devices <= 0 {
		return errors.New("devices must be > 0")
	}
	if cfg.tenant == "" || cfg.deviceType == "" {
		return errors.New("tenant and device-type are required")
	}
	if strings.ContainsAny(cfg.tenant+cfg.zone+cfg.deviceType, ". >*") {
		return errors.New("tenant, zone, and device-type must be valid NATS/MQTT path tokens without spaces, dots, >, or *")
	}
	if cfg.mqttQoS < 0 || cfg.mqttQoS > 2 {
		return errors.New("mqtt-qos must be 0, 1, or 2")
	}
	return nil
}

func newSender(ctx context.Context, cfg config) (sender, error) {
	switch cfg.method {
	case "mqtt":
		return newMQTTSender(cfg)
	case "rest":
		return newRESTSender(ctx, cfg)
	case "nats":
		return newNATSSender(cfg)
	default:
		return nil, fmt.Errorf("unknown method %q", cfg.method)
	}
}

func provision(ctx context.Context, cfg config, s sender) error {
	start := time.Now()
	for i := 0; i < cfg.devices; i++ {
		if err := s.Send(ctx, deviceName(i), buildProvisionPayload(i)); err != nil {
			return fmt.Errorf("%s: %w", deviceName(i), err)
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("provisioned %d devices in %s (%.1f msg/s)\n",
		cfg.devices, elapsed.Round(time.Millisecond), float64(cfg.devices)/elapsed.Seconds())
	return nil
}

func runBenchmark(ctx context.Context, cfg config, s sender) result {
	jobs := make(chan int64, cfg.concurrency*8)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	feedCtx := ctx
	cancelFeed := func() {}
	if cfg.mode == "sustained" {
		feedCtx, cancelFeed = context.WithTimeout(ctx, cfg.duration)
	}
	defer cancelFeed()

	var ok atomic.Int64
	var failed atomic.Int64
	var firstErr error
	var firstErrOnce sync.Once
	var workers sync.WaitGroup

	start := time.Now()
	for i := 0; i < cfg.concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for seq := range jobs {
				devIdx := int(seq % int64(cfg.devices))
				if err := s.Send(runCtx, deviceName(devIdx), buildValuePayload(devIdx, seq)); err != nil {
					failed.Add(1)
					firstErrOnce.Do(func() { firstErr = err })
					continue
				}
				ok.Add(1)
			}
		}()
	}

	var progressDone chan struct{}
	if cfg.reportEvery > 0 {
		progressDone = make(chan struct{})
		go reportProgress(progressDone, cfg.reportEvery, start, &ok, &failed)
	}

	produced := feedJobs(feedCtx, cfg, jobs, start)
	close(jobs)
	workers.Wait()
	elapsed := time.Since(start)
	if progressDone != nil {
		close(progressDone)
	}

	return result{
		produced: produced,
		ok:       ok.Load(),
		failed:   failed.Load(),
		elapsed:  elapsed,
		firstErr: firstErr,
	}
}

func feedJobs(ctx context.Context, cfg config, jobs chan<- int64, start time.Time) int64 {
	if cfg.rate == 0 {
		return feedJobsUnpaced(ctx, cfg, jobs, start)
	}
	return feedJobsPaced(ctx, cfg, jobs, start)
}

func feedJobsUnpaced(ctx context.Context, cfg config, jobs chan<- int64, start time.Time) int64 {
	var produced int64
	for {
		if cfg.mode == "burst" && produced >= cfg.messages {
			return produced
		}
		if cfg.mode == "sustained" && time.Since(start) >= cfg.duration {
			return produced
		}
		select {
		case <-ctx.Done():
			return produced
		case jobs <- produced:
			produced++
		}
	}
}

func feedJobsPaced(ctx context.Context, cfg config, jobs chan<- int64, start time.Time) int64 {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var produced int64
	for {
		if cfg.mode == "burst" && produced >= cfg.messages {
			return produced
		}
		if cfg.mode == "sustained" && time.Since(start) >= cfg.duration {
			return produced
		}

		target := int64(math.Floor(time.Since(start).Seconds() * cfg.rate))
		if cfg.mode == "burst" && target > cfg.messages {
			target = cfg.messages
		}
		for produced < target {
			select {
			case <-ctx.Done():
				return produced
			case jobs <- produced:
				produced++
			}
		}

		select {
		case <-ctx.Done():
			return produced
		case <-ticker.C:
		}
	}
}

func reportProgress(done <-chan struct{}, interval time.Duration, start time.Time, ok, failed *atomic.Int64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			elapsed := time.Since(start).Seconds()
			total := ok.Load() + failed.Load()
			if elapsed > 0 {
				fmt.Printf("progress: sent=%d ok=%d failed=%d rate=%.1f msg/s\n",
					total, ok.Load(), failed.Load(), float64(total)/elapsed)
			}
		}
	}
}

func printConfig(cfg config, senderName string) {
	fmt.Printf("xact benchmark: method=%s sender=%s mode=%s devices=%d concurrency=%d\n",
		cfg.method, senderName, cfg.mode, cfg.devices, cfg.concurrency)
	fmt.Printf("ingest method: %s\n", cfg.method)
	fmt.Printf("payload: %s\n", payloadSummary())
	if cfg.mode == "burst" {
		fmt.Printf("target: messages=%d", cfg.messages)
	} else {
		fmt.Printf("target: duration=%s", cfg.duration)
	}
	if cfg.rate > 0 {
		fmt.Printf(" rate=%.1f msg/s\n", cfg.rate)
	} else {
		fmt.Printf(" rate=max\n")
	}
}

func printResult(res result) {
	rate := 0.0
	if res.elapsed > 0 {
		rate = float64(res.ok) / res.elapsed.Seconds()
	}
	fmt.Printf("summary: produced=%d ok=%d failed=%d elapsed=%s rate=%.1f msg/s\n",
		res.produced, res.ok, res.failed, res.elapsed.Round(time.Millisecond), rate)
	if res.firstErr != nil {
		fmt.Printf("first error: %v\n", res.firstErr)
	}
}

func envDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func deviceName(i int) string {
	return fmt.Sprintf("Bench%06d", i+1)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// REST sender

type restSender struct {
	cfg    config
	client *http.Client
	apiKey string
	token  string
}

func newRESTSender(ctx context.Context, cfg config) (*restSender, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.restInsecure} //nolint:gosec
	s := &restSender{
		cfg: cfg,
		client: &http.Client{
			Timeout:   cfg.timeout,
			Transport: tr,
		},
		apiKey: cfg.restAPIKey,
	}
	if s.apiKey == "" {
		if err := s.ensureAPIKey(ctx); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *restSender) Name() string { return strings.TrimRight(s.cfg.restURL, "/") }
func (s *restSender) Close() error { return nil }

func (s *restSender) Send(ctx context.Context, deviceName string, p payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ingestURL(deviceName), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "ApiKey "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("REST ingest HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (s *restSender) ensureAPIKey(ctx context.Context) error {
	if err := s.ensureToken(ctx); err != nil {
		return err
	}
	keys, err := s.listAPIKeys(ctx)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.Name == benchmarkAPIKeyName && key.Key != "" {
			s.apiKey = key.Key
			return nil
		}
	}
	key, err := s.createAPIKey(ctx)
	if err != nil {
		return err
	}
	if key.Key == "" {
		return fmt.Errorf("created API key %q but response did not include key value", benchmarkAPIKeyName)
	}
	s.apiKey = key.Key
	return nil
}

func (s *restSender) ensureToken(ctx context.Context) error {
	if s.token != "" {
		return nil
	}
	body, err := json.Marshal(map[string]string{"username": s.cfg.restUsername, "password": s.cfg.restPassword})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL()+"/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	var login struct {
		Token string `json:"token"`
		User  struct {
			TenantID string `json:"tenant_id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return err
	}
	if login.Token == "" {
		return errors.New("login response did not include token")
	}
	s.token = login.Token
	if s.cfg.tenant != "" && login.User.TenantID != s.cfg.tenant {
		if err := s.switchOrg(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *restSender) switchOrg(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{"org": s.cfg.tenant})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL()+"/api/v1/auth/switch-org", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switch org to %q HTTP %d: %s", s.cfg.tenant, resp.StatusCode, readErrorBody(resp))
	}
	var switched struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&switched); err != nil {
		return err
	}
	if switched.Token == "" {
		return errors.New("switch-org response did not include token")
	}
	s.token = switched.Token
	return nil
}

func (s *restSender) listAPIKeys(ctx context.Context) ([]apiKeyResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.baseURL()+"/api/v1/api-keys", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list API keys HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	var keys []apiKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *restSender) createAPIKey(ctx context.Context) (*apiKeyResponse, error) {
	body, err := json.Marshal(map[string]string{"name": benchmarkAPIKeyName})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL()+"/api/v1/api-keys", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create API key HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	var key apiKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *restSender) ingestURL(deviceName string) string {
	if s.cfg.zone == "" {
		return fmt.Sprintf("%s/api/v1/ingest/%s/%s/%s",
			s.baseURL(), url.PathEscape(s.cfg.tenant), url.PathEscape(s.cfg.deviceType), url.PathEscape(deviceName))
	}
	return fmt.Sprintf("%s/api/v1/ingest/%s/zone/%s/%s/%s",
		s.baseURL(), url.PathEscape(s.cfg.tenant), url.PathEscape(s.cfg.zone),
		url.PathEscape(s.cfg.deviceType), url.PathEscape(deviceName))
}

func (s *restSender) baseURL() string {
	return strings.TrimRight(s.cfg.restURL, "/")
}

type apiKeyResponse struct {
	ID      int    `json:"id"`
	OrgName string `json:"orgName"`
	Name    string `json:"name"`
	Key     string `json:"key"`
}

func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// MQTT sender

type mqttSender struct {
	cfg    config
	client mqtt.Client
}

func newMQTTSender(cfg config) (*mqttSender, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.mqttURL)
	opts.SetClientID(cfg.mqttClientID)
	opts.SetUsername(cfg.mqttUsername)
	opts.SetPassword(cfg.mqttPassword)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(time.Second)
	opts.SetMaxReconnectInterval(10 * time.Second)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(cfg.timeout) {
		return nil, errors.New("MQTT connect timed out")
	}
	if err := token.Error(); err != nil {
		return nil, err
	}
	return &mqttSender{cfg: cfg, client: client}, nil
}

func (s *mqttSender) Name() string { return s.cfg.mqttURL }

func (s *mqttSender) Close() error {
	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(250)
	}
	return nil
}

func (s *mqttSender) Send(_ context.Context, deviceName string, p payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	token := s.client.Publish(s.topic(deviceName), byte(s.cfg.mqttQoS), false, body)
	if !token.WaitTimeout(s.cfg.timeout) {
		return errors.New("MQTT publish timed out")
	}
	return token.Error()
}

func (s *mqttSender) topic(deviceName string) string {
	if s.cfg.zone == "" {
		return fmt.Sprintf("xact/data/%s/%s/%s", s.cfg.tenant, s.cfg.deviceType, deviceName)
	}
	return fmt.Sprintf("xact/data/%s/zone/%s/%s/%s", s.cfg.tenant, s.cfg.zone, s.cfg.deviceType, deviceName)
}

// NATS sender

type natsSender struct {
	cfg       config
	nc        *natsgo.Conn
	published atomic.Int64
}

type ingestEvent struct {
	Tenant            string  `json:"tenant"`
	Zone              string  `json:"zone"`
	DeviceType        string  `json:"device_type"`
	DeviceName        string  `json:"device_name"`
	TagData           tagData `json:"tag_data"`
	PublishedUnixNano int64   `json:"published_unix_nano,omitempty"`
}

type ingestResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type tagData struct {
	Groups      map[string]map[string]any `json:"Groups"`
	DirectTags  map[string]any            `json:"DirectTags"`
	TSUnixMilli int64                     `json:"TSUnixMilli"`
}

func newNATSSender(cfg config) (*natsSender, error) {
	opts := []natsgo.Option{
		natsgo.Name("xact-benchmark"),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(time.Second),
	}
	if cfg.natsUsername != "" && cfg.natsPassword != "" {
		opts = append(opts, natsgo.UserInfo(cfg.natsUsername, cfg.natsPassword))
	}
	nc, err := natsgo.Connect(cfg.natsURL, opts...)
	if err != nil {
		return nil, err
	}
	return &natsSender{cfg: cfg, nc: nc}, nil
}

func (s *natsSender) Name() string { return s.cfg.natsURL }

func (s *natsSender) Close() error {
	if s.nc != nil {
		if err := s.nc.FlushTimeout(s.cfg.timeout); err != nil {
			s.nc.Close()
			return err
		}
		s.nc.Close()
	}
	return nil
}

func (s *natsSender) Send(_ context.Context, deviceName string, p payload) error {
	evt := ingestEvent{
		Tenant:            s.cfg.tenant,
		Zone:              s.cfg.zone,
		DeviceType:        s.cfg.deviceType,
		DeviceName:        deviceName,
		PublishedUnixNano: time.Now().UnixNano(),
		TagData: tagData{
			Groups:      p,
			DirectTags:  map[string]any{},
			TSUnixMilli: time.Now().UnixMilli(),
		},
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	reply, err := s.nc.Request(s.subject(deviceName), body, s.cfg.timeout)
	if err != nil {
		return err
	}
	var resp ingestResponse
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return err
	}
	if resp.Status != "accepted" {
		if resp.Error != "" {
			return fmt.Errorf("NATS ingest %s: %s", resp.Status, resp.Error)
		}
		return fmt.Errorf("NATS ingest %s", resp.Status)
	}
	s.published.Add(1)
	return nil
}

func (s *natsSender) subject(deviceName string) string {
	if s.cfg.zone == "" {
		return fmt.Sprintf("xact.internal.ingest_request.%s.%s.%s", s.cfg.tenant, s.cfg.deviceType, deviceName)
	}
	return fmt.Sprintf("xact.internal.ingest_request.%s.zone.%s.%s.%s", s.cfg.tenant, s.cfg.zone, s.cfg.deviceType, deviceName)
}
