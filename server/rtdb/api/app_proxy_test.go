package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xact-iot/xact/applications"
	"github.com/xact-iot/xact/rtdb/tree"
)

const proxyTestSecret = "test-secret"

// newProxyTestServer installs a traffic manifest whose http block points at
// upstream and serves XACT through a real listener, so deadlines, streaming
// and hijacking behave as in production.
func newProxyTestServer(t *testing.T, cfg ServerConfig, httpBlock string, configure func(*http.Server)) (*Server, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "applications"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"traffic","name":"Traffic","resource":{"id":"traffic-studio","permissions":[{"name":"view"}]},"operations":["get_status"],"http":` + httpBlock + `}`
	if err := os.WriteFile(filepath.Join(root, "applications", "traffic.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyPath == "" {
		cfg.ProxyPath = "/xact"
	}
	s := NewServer(cfg, tree.NewTreeWithOperations(nil), nil, nil, proxyTestSecret, newTestDB("admin", "password"), root)
	if s.appProxies["traffic"] == nil {
		t.Fatal("traffic proxy not loaded")
	}
	front := httptest.NewUnstartedServer(s.Router())
	if configure != nil {
		configure(front.Config)
	}
	front.Start()
	t.Cleanup(front.Close)
	return s, front
}

func proxyTestToken(t *testing.T) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, JWTClaims{
		UserID: "1", Username: "admin", TenantID: "default", Roles: []string{"Admin"},
		AllowedOrgs: []string{"default"}, TokenVersion: 1,
	}).SignedString([]byte(proxyTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func proxyRequest(t *testing.T, method, url string, body io.Reader, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func proxyErrorMessage(t *testing.T, res *http.Response) string {
	t.Helper()
	var body struct{ Message string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("error body: %v", err)
	}
	return body.Message
}

type seenRequests struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (s *seenRequests) add(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r.Clone(r.Context()))
}

func (s *seenRequests) all() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func TestAppProxyPathMappingAndHeaders(t *testing.T) {
	var seen seenRequests
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Set-Cookie", "app=1")
		fmt.Fprint(w, "a,b\n")
	}))
	defer upstream.Close()
	_, front := newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"`+upstream.URL+`"}`, nil)
	token := proxyTestToken(t)
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Cookie":        "xact=secret",
	}

	res := proxyRequest(t, http.MethodGet, front.URL+"/xact/traffic/api/v1/networks?bbox=1,2&zoom=3", nil, headers)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "a,b\n" {
		t.Fatalf("proxied response: %d %q", res.StatusCode, body)
	}
	if got := res.Header.Values("Content-Type"); len(got) != 1 || got[0] != "text/csv" {
		t.Fatalf("content type not taken from application: %v", got)
	}
	if got := res.Header.Values("X-Frame-Options"); len(got) != 1 || got[0] != "SAMEORIGIN" {
		t.Fatalf("application header should replace XACT default: %v", got)
	}
	if res.Header.Get("Set-Cookie") != "" {
		t.Fatal("Set-Cookie forwarded to browser")
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("XACT security headers missing where the application sets none")
	}

	got := seen.all()[0]
	if got.URL.Path != "/traffic/api/v1/networks" || got.URL.RawQuery != "bbox=1,2&zoom=3" {
		t.Fatalf("path mapping: %s?%s", got.URL.Path, got.URL.RawQuery)
	}
	if got.Header.Get("Authorization") != "Bearer "+token {
		t.Fatal("Authorization not forwarded unchanged")
	}
	if got.Header.Get("Cookie") != "" {
		t.Fatalf("cookie forwarded: %v", got.Header)
	}
	frontHost := strings.TrimPrefix(front.URL, "http://")
	if got.Header.Get("X-Forwarded-For") != "127.0.0.1" || got.Header.Get("X-Forwarded-Host") != frontHost || got.Header.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("forwarding headers: %v", got.Header)
	}
	if got.Host != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatalf("upstream Host = %q", got.Host)
	}

	// Front proxy headers describe the browser-facing address. The client
	// address is the one XACT's RealIP middleware resolved.
	headers["X-Real-IP"] = "203.0.113.9"
	headers["X-Forwarded-For"] = "203.0.113.9"
	headers["X-Forwarded-Host"] = "xact.example.com"
	headers["X-Forwarded-Proto"] = "https"
	proxyRequest(t, http.MethodPost, front.URL+"/xact/traffic", strings.NewReader("{}"), headers)
	proxyRequest(t, http.MethodGet, front.URL+"/xact/traffic/api/v1/files/a%2Fb", nil, headers)
	all := seen.all()
	if got := all[1]; got.Method != http.MethodPost || got.URL.Path != "/traffic" ||
		got.Header.Get("X-Forwarded-For") != "203.0.113.9" || got.Header.Get("X-Real-Ip") != "" || got.Header.Get("X-Forwarded-Host") != "xact.example.com" || got.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("prefix root or front proxy headers: %s %s %v", got.Method, got.URL.Path, got.Header)
	}
	if got := all[2]; got.URL.EscapedPath() != "/traffic/api/v1/files/a%2Fb" {
		t.Fatalf("escaped path not preserved: %s", got.URL.EscapedPath())
	}
}

func TestAppProxyRequiresXACTSession(t *testing.T) {
	var seen seenRequests
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen.add(r) }))
	defer upstream.Close()
	_, front := newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"`+upstream.URL+`","websocket_paths":["/ws"]}`, nil)

	for name, header := range map[string]map[string]string{
		"no token":       {},
		"invalid token":  {"Authorization": "Bearer not-a-token"},
		"cookie only":    {"Cookie": "xact_auth_token=" + proxyTestToken(t)},
		"plain GET ws":   {},
		"basic password": {"Authorization": "Basic YWRtaW46cGFzc3dvcmQ="},
	} {
		path := "/xact/traffic/api/v1/networks"
		if name == "plain GET ws" {
			path = "/xact/traffic/ws"
		}
		if res := proxyRequest(t, http.MethodGet, front.URL+path, nil, header); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", name, res.StatusCode)
		}
	}
	if n := len(seen.all()); n != 0 {
		t.Fatalf("unauthenticated requests reached the application: %d", n)
	}
	if res := proxyRequest(t, http.MethodGet, front.URL+"/xact/other/api", nil, map[string]string{"Authorization": "Bearer " + proxyTestToken(t)}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unregistered prefix: %d", res.StatusCode)
	}
}

func TestAppProxyBodyLimit(t *testing.T) {
	var seen seenRequests
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			return
		}
		fmt.Fprint(w, n)
	}))
	defer upstream.Close()
	// XACT's own limit is 1 MiB; the application allows 2 MiB.
	_, front := newProxyTestServer(t, ServerConfig{MaxRequestBodyBytes: 1 << 20}, `{"prefix":"traffic","upstream":"`+upstream.URL+`","max_body_mb":2}`, nil)
	auth := map[string]string{"Authorization": "Bearer " + proxyTestToken(t)}

	res := proxyRequest(t, http.MethodPost, front.URL+"/xact/traffic/api/v1/uploads", bytes.NewReader(make([]byte, 3<<19)), auth)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != fmt.Sprint(3<<19) {
		t.Fatalf("body within application limit: %d %s", res.StatusCode, body)
	}

	res = proxyRequest(t, http.MethodPost, front.URL+"/xact/traffic/api/v1/uploads", bytes.NewReader(make([]byte, 3<<20)), auth)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversized body: %d", res.StatusCode)
	}
	if len(seen.all()) != 1 {
		t.Fatal("oversized body with Content-Length reached the application")
	}

	// A chunked body has no Content-Length and is cut off while streaming.
	chunked := io.MultiReader(bytes.NewReader(make([]byte, 3<<20)))
	res = proxyRequest(t, http.MethodPost, front.URL+"/xact/traffic/api/v1/uploads", chunked, auth)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked oversized body: %d", res.StatusCode)
	}

	// Other XACT routes keep the server-wide limit.
	res = proxyRequest(t, http.MethodPost, front.URL+"/xact/api/v1/logs", bytes.NewReader(make([]byte, 3<<19)), auth)
	if res.StatusCode == http.StatusOK {
		t.Fatal("server-wide body limit no longer applies to XACT routes")
	}
}

func TestAppProxyStreamsAndOutlivesServerTimeouts(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, n)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "first\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			return
		}
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintf(w, "chunk %d\n", i)
			w.(http.Flusher).Flush()
		}
	}))
	defer upstream.Close()
	// The front server's whole-request timeouts are far shorter than the transfers.
	_, front := newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"`+upstream.URL+`","timeout_seconds":1}`, func(s *http.Server) {
		s.ReadTimeout = 300 * time.Millisecond
		s.WriteTimeout = 300 * time.Millisecond
	})
	auth := map[string]string{"Authorization": "Bearer " + proxyTestToken(t)}

	res := proxyRequest(t, http.MethodGet, front.URL+"/xact/traffic/api/v1/export", nil, auth)
	reader := bufio.NewReader(res.Body)
	first := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if line != "first\n" {
			t.Fatalf("first chunk = %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first chunk was buffered by the proxy")
	}
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.HasSuffix(string(rest), "chunk 9\n") {
		t.Fatalf("long download cut off: %v %q", err, rest)
	}

	// A slow chunked upload that outlasts ReadTimeout.
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 8; i++ {
			time.Sleep(100 * time.Millisecond)
			pw.Write(make([]byte, 1024))
		}
		pw.Close()
	}()
	res = proxyRequest(t, http.MethodPost, front.URL+"/xact/traffic/api/v1/uploads", pr, auth)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "8192" {
		t.Fatalf("slow upload: %d %s", res.StatusCode, body)
	}
}

func TestAppProxyUpstreamDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, front := newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"http://`+addr+`"}`, nil)
	res := proxyRequest(t, http.MethodGet, front.URL+"/xact/traffic/api/v1/networks", nil, map[string]string{"Authorization": "Bearer " + proxyTestToken(t)})
	if res.StatusCode != http.StatusBadGateway || proxyErrorMessage(t, res) != "application unavailable" {
		t.Fatalf("upstream down: %d", res.StatusCode)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer slow.Close()
	_, front = newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"`+slow.URL+`","timeout_seconds":1}`, nil)
	res = proxyRequest(t, http.MethodGet, front.URL+"/xact/traffic/api/v1/networks", nil, map[string]string{"Authorization": "Bearer " + proxyTestToken(t)})
	if res.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("upstream timeout: %d", res.StatusCode)
	}
}

// wsUpstream accepts any upgrade, greets the client and echoes bytes back.
func wsUpstream(t *testing.T, seen *seenRequests) *httptest.Server {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "upgrade required", http.StatusUpgradeRequired)
			return
		}
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nhello")
		brw.Flush()
		buf := make([]byte, 64)
		for {
			n, err := brw.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func dialUpgrade(t *testing.T, front *httptest.Server, path string, header map[string]string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var req strings.Builder
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n", path, strings.TrimPrefix(front.URL, "http://"))
	for k, v := range header {
		fmt.Fprintf(&req, "%s: %s\r\n", k, v)
	}
	req.WriteString("\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn, br, res
}

func TestAppProxyWebSocketPassThrough(t *testing.T) {
	var seen seenRequests
	upstream := wsUpstream(t, &seen)
	_, front := newProxyTestServer(t, ServerConfig{AllowedOrigins: []string{"http://localhost:3000"}},
		`{"prefix":"traffic","upstream":"`+upstream.URL+`","websocket_paths":["/ws"]}`, func(s *http.Server) {
			s.ReadTimeout = 300 * time.Millisecond
			s.WriteTimeout = 300 * time.Millisecond
		})
	self := "http://" + strings.TrimPrefix(front.URL, "http://")

	conn, br, res := dialUpgrade(t, front, "/xact/traffic/ws?ticket=abc", map[string]string{"Origin": self, "Cookie": "xact=secret"})
	if res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade with own origin: %d", res.StatusCode)
	}
	greeting := make([]byte, 5)
	if _, err := io.ReadFull(br, greeting); err != nil || string(greeting) != "hello" {
		t.Fatalf("server frame: %v %q", err, greeting)
	}
	// The socket outlives the server's read and write timeouts.
	time.Sleep(500 * time.Millisecond)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 4)
	if _, err := io.ReadFull(br, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("client frame: %v %q", err, echo)
	}
	got := seen.all()[0]
	if got.URL.Path != "/traffic/ws" || got.URL.RawQuery != "ticket=abc" || got.Header.Get("Origin") != self || got.Header.Get("Cookie") != "" {
		t.Fatalf("upgrade forwarded as %s?%s %v", got.URL.Path, got.URL.RawQuery, got.Header)
	}

	for name, origin := range map[string]string{"configured origin": "http://localhost:3000", "front proxy host": "https://xact.example.com"} {
		_, _, res := dialUpgrade(t, front, "/xact/traffic/ws?ticket=abc", map[string]string{"Origin": origin, "X-Forwarded-Host": "xact.example.com"})
		if res.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("%s: %d", name, res.StatusCode)
		}
	}

	before := len(seen.all())
	for name, tc := range map[string]struct {
		path, origin string
		status       int
	}{
		"foreign origin":     {"/xact/traffic/ws", "http://evil.example", http.StatusForbidden},
		"lookalike origin":   {"/xact/traffic/ws", self + ".evil.example", http.StatusForbidden},
		"null origin":        {"/xact/traffic/ws", "null", http.StatusForbidden},
		"missing origin":     {"/xact/traffic/ws", "", http.StatusForbidden},
		"non-websocket path": {"/xact/traffic/api/v1/networks", self, http.StatusBadRequest},
	} {
		header := map[string]string{}
		if tc.origin != "" {
			header["Origin"] = tc.origin
		}
		if _, _, res := dialUpgrade(t, front, tc.path, header); res.StatusCode != tc.status {
			t.Fatalf("%s: status %d, want %d", name, res.StatusCode, tc.status)
		}
	}
	if len(seen.all()) != before {
		t.Fatal("rejected upgrade reached the application")
	}
}

func TestApplicationsListHidesProxyUpstream(t *testing.T) {
	s, _ := newProxyTestServer(t, ServerConfig{}, `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","max_body_mb":200,"websocket_paths":["/ws"]}`, nil)
	req := httptest.NewRequest(http.MethodGet, "/xact/api/v1/applications", nil)
	req.Header.Set("Authorization", "Bearer "+proxyTestToken(t))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "8093") || strings.Contains(rec.Body.String(), "upstream") {
		t.Fatalf("applications list: %d %s", rec.Code, rec.Body.String())
	}
	var list []applications.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].HTTP == nil ||
		list[0].HTTP.Prefix != "traffic" || list[0].HTTP.MaxBodyMB != 200 || len(list[0].HTTP.WebSocketPaths) != 1 {
		t.Fatalf("browser proxy fields: %v %s", err, rec.Body.String())
	}
}

// Every first path segment XACT serves under its base path must be reserved,
// or an application prefix could shadow it.
func TestAppProxyReservedPrefixesCoverXACTRoutes(t *testing.T) {
	s := NewServer(ServerConfig{ProxyPath: "/xact", StaticDir: t.TempDir()}, tree.NewTreeWithOperations(nil), nil, nil, proxyTestSecret, newTestDB("admin", "password"), "")
	seen := map[string]bool{}
	err := chi.Walk(s.Router(), func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		rest, ok := strings.CutPrefix(route, "/xact/")
		if !ok {
			return nil
		}
		segment, _, _ := strings.Cut(rest, "/")
		if segment != "" && !strings.Contains(segment, ".") && !applications.ReservedPrefix(segment) {
			t.Errorf("XACT route %s uses unreserved first segment %q; add it to applications.reservedPrefixes", route, segment)
		}
		seen[segment] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range []string{"api", "plugins", "assets", "public"} {
		if !seen[segment] {
			t.Fatalf("route walk missed %q", segment)
		}
	}

	s = &Server{config: ServerConfig{ProxyPath: "/xact"}}
	s.config.MCP.Enabled = true
	s.config.MCP.Route = "/traffic"
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "applications"), 0700)
	os.WriteFile(filepath.Join(root, "applications", "traffic.json"), []byte(`{"id":"traffic","resource":{"id":"t","permissions":[{"name":"view"}]},"operations":["get_status"],"http":{"prefix":"traffic","upstream":"http://127.0.0.1:8093"}}`), 0600)
	s.pluginDir = root
	if proxies := s.loadAppProxies(); proxies["traffic"] != nil {
		t.Fatal("proxy registered over the MCP route")
	}
}
