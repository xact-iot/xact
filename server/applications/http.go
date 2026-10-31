package applications

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// HTTP configures the optional application proxy. Requests to
// {base}/{prefix}/... are forwarded unchanged, apart from the base path, to
// {upstream}/{prefix}/...; WebSocketPaths are relative to the prefix.
type HTTP struct {
	Prefix         string   `json:"prefix"`
	Upstream       string   `json:"upstream,omitempty"`
	MaxBodyMB      int      `json:"max_body_mb,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	WebSocketPaths []string `json:"websocket_paths,omitempty"`
}

const (
	DefaultMaxBodyMB      = 8
	MaxBodyMBLimit        = 2048
	DefaultTimeoutSeconds = 60
	TimeoutSecondsLimit   = 3600
	maxWebSocketPaths     = 16
)

var (
	httpPrefix    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	websocketPath = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
)

// reservedPrefixes are XACT's own first path segments under its base path,
// plus the Vite dev server's. A test in rtdb/api keeps this list in step with
// the router.
var reservedPrefixes = map[string]bool{
	"api": true, "api-docs": true, "assets": true, "health": true, "icons": true,
	"login": true, "manual": true, "mcp": true, "plugins": true, "public": true,
	"node_modules": true, "src": true,
}

// ReservedPrefix reports whether an application may not use prefix. Prefixes
// starting with "ws" are reserved because deployments route {base}/ws* to the
// NATS WebSocket listener.
func ReservedPrefix(prefix string) bool {
	return reservedPrefixes[prefix] || strings.HasPrefix(prefix, "ws")
}

// validateHTTP checks the proxy block and fills in defaults.
func validateHTTP(h *HTTP) error {
	if !httpPrefix.MatchString(h.Prefix) {
		return fmt.Errorf("http prefix %q must be 1-64 lowercase letters, digits, '-' or '_'", h.Prefix)
	}
	if ReservedPrefix(h.Prefix) {
		return fmt.Errorf("http prefix %q is reserved by XACT", h.Prefix)
	}
	u, e := url.Parse(h.Upstream)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("http upstream %q must be an absolute http(s) URL with no path, such as http://127.0.0.1:8093", h.Upstream)
	}
	if h.MaxBodyMB == 0 {
		h.MaxBodyMB = DefaultMaxBodyMB
	}
	if h.MaxBodyMB < 1 || h.MaxBodyMB > MaxBodyMBLimit {
		return fmt.Errorf("http max_body_mb must be between 1 and %d", MaxBodyMBLimit)
	}
	if h.TimeoutSeconds == 0 {
		h.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if h.TimeoutSeconds < 1 || h.TimeoutSeconds > TimeoutSecondsLimit {
		return fmt.Errorf("http timeout_seconds must be between 1 and %d", TimeoutSecondsLimit)
	}
	if len(h.WebSocketPaths) > maxWebSocketPaths {
		return fmt.Errorf("http allows at most %d websocket_paths", maxWebSocketPaths)
	}
	seen := map[string]bool{}
	for _, p := range h.WebSocketPaths {
		if len(p) > 256 || !websocketPath.MatchString(p) || path.Clean(p) != p || seen[p] {
			return fmt.Errorf("http websocket path %q must be a unique clean path such as /ws", p)
		}
		seen[p] = true
	}
	return nil
}
