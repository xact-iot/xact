package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

func limitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func trustedPluginDir(pluginDir string) string {
	pluginDir = strings.TrimSpace(pluginDir)
	if pluginDir == "" {
		return ""
	}

	checks := []string{pluginDir}
	for _, sub := range []string{"authentication", "widgets", "map-layer", "themes"} {
		checks = append(checks, filepath.Join(pluginDir, sub))
	}

	for _, path := range checks {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			log.Printf("plugin directory %s is not a directory; plugin loading disabled", path)
			return ""
		}
		mode := info.Mode().Perm()
		if mode&0o002 != 0 {
			log.Printf("plugin directory %s is world-writable; plugin loading disabled", path)
			return ""
		}
		if mode&0o020 != 0 {
			log.Printf("plugin directory %s is group-writable; ensure the group is trusted", path)
		}
	}
	return pluginDir
}

func authPluginExecutionEnabled() bool {
	return parseAPIEnvBool(os.Getenv("ENABLE_AUTH_PLUGIN"), false)
}

func parseAPIEnvBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
