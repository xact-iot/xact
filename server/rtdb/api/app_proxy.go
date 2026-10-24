package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/applications"
)

// appProxy forwards {base}/{prefix}/... to {upstream}/{prefix}/... for an
// application that declares an http block in its manifest. Only XACT's base
// path is stripped. HTTP requests need a valid XACT session; WebSocket
// upgrades on the declared paths are left to the application to
// authenticate, after an Origin check.
type appProxy struct {
	id        string
	prefix    string
	base      string
	target    *url.URL
	maxBody   int64
	timeout   time.Duration
	wsPaths   map[string]bool
	transport http.RoundTripper
}

func newAppProxy(m applications.Manifest, base string) (*appProxy, error) {
	h := m.HTTP
	target, err := url.Parse(h.Upstream)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(h.TimeoutSeconds) * time.Second
	p := &appProxy{
		id:      m.ID,
		prefix:  h.Prefix,
		base:    strings.TrimRight(base, "/"),
		target:  &url.URL{Scheme: target.Scheme, Host: target.Host},
		maxBody: int64(h.MaxBodyMB) << 20,
		timeout: timeout,
		wsPaths: map[string]bool{},
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			ExpectContinueTimeout: time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   32,
			DisableCompression:    true,
		},
	}
	for _, path := range h.WebSocketPaths {
		p.wsPaths[path] = true
	}
	return p, nil
}

// loadAppProxies reads the http blocks once at startup, like the NATS service
// identities; manifest changes take effect on restart.
func (s *Server) loadAppProxies() map[string]*appProxy {
	manifests, err := applications.Load(s.pluginDir)
	if err != nil {
		log.Printf("application proxy: manifests unavailable: %v", err)
		return nil
	}
	mcpSegment := ""
	if s.config.MCP.Enabled {
		mcpSegment, _, _ = strings.Cut(strings.TrimPrefix(s.config.MCP.Route, "/"), "/")
	}
	proxies := map[string]*appProxy{}
	for _, m := range manifests {
		if m.HTTP == nil {
			continue
		}
		if m.HTTP.Prefix == mcpSegment {
			log.Printf("application proxy: %s prefix %q collides with MCP_ROUTE; proxy disabled", m.ID, m.HTTP.Prefix)
			continue
		}
		p, err := newAppProxy(m, s.config.ProxyPath)
		if err != nil {
			log.Printf("application proxy: %s: %v", m.ID, err)
			continue
		}
		proxies[p.prefix] = p
		log.Printf("application proxy: %s/%s/ -> %s/%s/", p.base, p.prefix, p.target, p.prefix)
	}
	return proxies
}

func (s *Server) registerAppProxies(r chi.Router) {
	prefixes := make([]string, 0, len(s.appProxies))
	for prefix := range s.appProxies {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		h := s.appProxyHandler(s.appProxies[prefix])
		r.Handle("/"+prefix, h)
		r.Handle("/"+prefix+"/*", h)
	}
}

// appProxyFor returns the proxy that the router will select for r, using the
// same raw path that chi routes on.
func (s *Server) appProxyFor(r *http.Request) *appProxy {
	if len(s.appProxies) == 0 {
		return nil
	}
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	rest, ok := strings.CutPrefix(path, strings.TrimRight(s.config.ProxyPath, "/")+"/")
	if !ok {
		return nil
	}
	prefix, _, _ := strings.Cut(rest, "/")
	return s.appProxies[prefix]
}

// exemptAppProxies skips the server-wide body limit for proxied requests,
// which enforce their application's max_body_mb instead.
func (s *Server) exemptAppProxies(limit func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.appProxyFor(r) != nil {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

func (s *Server) appProxyHandler(p *appProxy) http.Handler {
	authenticated := JWTAuth(s.jwtSecret, s.db)(http.HandlerFunc(p.serveHTTP))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrade := upgradeRequested(r)
		if upgrade == "" {
			authenticated.ServeHTTP(w, r)
			return
		}
		// Browsers cannot send Authorization on a WebSocket; the application
		// authenticates the upgrade itself, for example with a ticket.
		if !strings.EqualFold(upgrade, "websocket") || !p.wsPaths[p.relativePath(r)] {
			proxyError(w, http.StatusBadRequest, "upgrade not allowed on this path")
			return
		}
		if !s.allowedWebSocketOrigin(r) {
			proxyError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		// Hijacking clears the server's deadlines, so long-lived sockets are
		// not cut off; frames are copied in both directions as they arrive.
		p.reverseProxy(w).ServeHTTP(w, r)
	})
}

func (p *appProxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > p.maxBody {
		proxyError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	// Replace the server's whole-request deadlines with idle deadlines so that
	// large uploads and downloads continue while bytes are moving. The server
	// clears the read deadline itself once the body has been read.
	ctl := http.NewResponseController(w)
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = http.MaxBytesReader(w, &idleReader{ReadCloser: r.Body, ctl: ctl, timeout: p.timeout}, p.maxBody)
	}
	_ = ctl.SetWriteDeadline(time.Time{})
	iw := &idleWriter{ResponseWriter: w, ctl: ctl, timeout: p.timeout}
	p.reverseProxy(iw).ServeHTTP(iw, r)
	_ = ctl.SetWriteDeadline(time.Now().Add(p.timeout))
}

func (p *appProxy) reverseProxy(w http.ResponseWriter) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite:       p.rewrite,
		Transport:     p.transport,
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			res.Header.Del("Set-Cookie")
			// The application's headers replace XACT defaults such as the
			// security and CORS headers instead of duplicating them.
			for k := range res.Header {
				w.Header().Del(k)
			}
			return nil
		},
		ErrorHandler: p.handleError,
	}
}

func (p *appProxy) rewrite(pr *httputil.ProxyRequest) {
	in, out := pr.In, pr.Out
	out.URL.Scheme = p.target.Scheme
	out.URL.Host = p.target.Host
	out.URL.Path = strings.TrimPrefix(in.URL.Path, p.base)
	out.URL.RawPath = strings.TrimPrefix(in.URL.RawPath, p.base)
	out.Host = ""
	// Authorization is forwarded unchanged so the application can recheck the
	// live session; XACT does not use cookies.
	out.Header.Del("Cookie")
	out.Header.Del("X-Real-Ip")
	out.Header.Del("True-Client-Ip")
	if ip := proxyClientIP(in.RemoteAddr); ip != "" {
		out.Header.Set("X-Forwarded-For", ip)
	}
	out.Header.Set("X-Forwarded-Host", requestHost(in))
	out.Header.Set("X-Forwarded-Proto", requestScheme(in))
}

func (p *appProxy) relativePath(r *http.Request) string {
	return strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, p.base), "/"+p.prefix)
}

func (p *appProxy) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	var opErr *net.OpError
	var netErr net.Error
	switch {
	case errors.As(err, &tooLarge):
		proxyError(w, http.StatusRequestEntityTooLarge, "request body too large")
	case errors.Is(err, context.Canceled):
		proxyError(w, http.StatusBadGateway, "request cancelled")
	case errors.As(err, &opErr) && opErr.Op == "dial":
		log.Printf("application proxy %s: %s %s: %v", p.id, r.Method, r.URL.Path, err)
		proxyError(w, http.StatusBadGateway, "application unavailable")
	case errors.As(err, &netErr) && netErr.Timeout():
		log.Printf("application proxy %s: %s %s: %v", p.id, r.Method, r.URL.Path, err)
		proxyError(w, http.StatusGatewayTimeout, "application did not respond in time")
	default:
		log.Printf("application proxy %s: %s %s: %v", p.id, r.Method, r.URL.Path, err)
		proxyError(w, http.StatusBadGateway, "application unavailable")
	}
}

func proxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": http.StatusText(status), "message": message})
}

// allowedWebSocketOrigin accepts XACT's own origin, as seen in Host or the
// front proxy's X-Forwarded-Host, and the configured CORS origins. Browsers
// always send Origin on WebSockets and cannot forge these headers.
func (s *Server) allowedWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	if origin == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	for _, allowed := range s.config.AllowedOrigins {
		if allowed != "*" && strings.EqualFold(strings.TrimRight(allowed, "/"), origin) {
			return true
		}
	}
	want := canonicalHost(u.Host, u.Scheme)
	for _, host := range []string{r.Host, firstHeaderValue(r.Header.Get("X-Forwarded-Host"))} {
		if host != "" && canonicalHost(host, u.Scheme) == want {
			return true
		}
	}
	return false
}

func canonicalHost(hostport, scheme string) string {
	hostport = strings.ToLower(hostport)
	host, port, err := net.SplitHostPort(hostport)
	if err != nil || !((scheme == "http" && port == "80") || (scheme == "https" && port == "443")) {
		return hostport
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func upgradeRequested(r *http.Request) string {
	for _, v := range r.Header.Values("Connection") {
		for _, token := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return r.Header.Get("Upgrade")
			}
		}
	}
	return ""
}

func firstHeaderValue(v string) string {
	first, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(first)
}

// requestHost and requestScheme describe the browser-facing address, trusting
// the front proxy's headers as the rest of XACT does.
func requestHost(r *http.Request) string {
	if host := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); host != "" {
		return host
	}
	return r.Host
}

func requestScheme(r *http.Request) string {
	switch proto := strings.ToLower(firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))); proto {
	case "http", "https":
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// proxyClientIP accepts RemoteAddr with or without a port; chi's RealIP
// middleware stores a bare address.
func proxyClientIP(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}

// idleReader extends the read deadline before each read of a proxied
// request body, so a stalled upload is dropped but a long one is not.
type idleReader struct {
	io.ReadCloser
	ctl     *http.ResponseController
	timeout time.Duration
}

func (b *idleReader) Read(p []byte) (int, error) {
	_ = b.ctl.SetReadDeadline(time.Now().Add(b.timeout))
	return b.ReadCloser.Read(p)
}

// idleWriter extends the write deadline before each write to the client, so a
// stalled client is dropped but a long download is not.
type idleWriter struct {
	http.ResponseWriter
	ctl     *http.ResponseController
	timeout time.Duration
}

func (w *idleWriter) extend() { _ = w.ctl.SetWriteDeadline(time.Now().Add(w.timeout)) }

func (w *idleWriter) WriteHeader(status int) {
	w.extend()
	w.ResponseWriter.WriteHeader(status)
}

func (w *idleWriter) Write(p []byte) (int, error) {
	w.extend()
	return w.ResponseWriter.Write(p)
}

func (w *idleWriter) FlushError() error {
	w.extend()
	return w.ctl.Flush()
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
