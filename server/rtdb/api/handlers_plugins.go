package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// widgetPluginMeta describes a discovered widget plugin.
type widgetPluginMeta struct {
	Name string `json:"name"` // filename without extension (e.g. "big-number")
	URL  string `json:"url"`  // path to fetch the plugin JS (e.g. "/plugins/widgets/big-number.js")
}

// handleListWidgetPlugins returns a JSON array of .js files found in the
// plugins/widgets directory. Returns an empty array if the directory is absent.
func (s *Server) handleListWidgetPlugins(w http.ResponseWriter, r *http.Request) {
	s.handleListPlugins(w, "widgets")
}

func (s *Server) handleListPlugins(w http.ResponseWriter, subdir string) {
	if s.pluginDir == "" {
		json.NewEncoder(w).Encode([]widgetPluginMeta{})
		return
	}

	pluginsDir := filepath.Join(s.pluginDir, subdir)
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		// Directory may not exist yet; return empty list
		json.NewEncoder(w).Encode([]widgetPluginMeta{})
		return
	}

	plugins := make([]widgetPluginMeta, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		slug := strings.TrimSuffix(name, ".js")
		plugins = append(plugins, widgetPluginMeta{
			Name: slug,
			URL:  "/plugins/" + subdir + "/" + name,
		})
	}

	json.NewEncoder(w).Encode(plugins)
}

// handleServeWidgetPlugin serves a single .js plugin file from the
// plugins/widgets directory. The Content-Type is set explicitly so that
// the router-wide JSONContentType middleware does not interfere.
func (s *Server) handleServeWidgetPlugin(w http.ResponseWriter, r *http.Request) {
	s.servePluginScript(w, r, "widgets")
}

// handleListMapLayerPlugins returns a JSON array of .js files found in the
// plugins/map-layer directory. These scripts register layer-level map plugins.
func (s *Server) handleListMapLayerPlugins(w http.ResponseWriter, r *http.Request) {
	s.handleListPlugins(w, "map-layer")
}

// handleServeMapLayerPlugin serves a single .js plugin file from the
// plugins/map-layer directory.
func (s *Server) handleServeMapLayerPlugin(w http.ResponseWriter, r *http.Request) {
	s.servePluginScript(w, r, "map-layer")
}

// handleListThemePlugins returns a JSON array of .js files found in the
// plugins/themes directory. Returns an empty array if the directory is absent.
func (s *Server) handleListThemePlugins(w http.ResponseWriter, r *http.Request) {
	s.handleListPlugins(w, "themes")
}

// handleServeThemePlugin serves a single .js plugin file from the
// plugins/themes directory.
func (s *Server) handleServeThemePlugin(w http.ResponseWriter, r *http.Request) {
	s.servePluginScript(w, r, "themes")
}

// servePluginScript is the shared implementation for serving plugin JS files
// from a subdirectory of the plugin directory.
func (s *Server) servePluginScript(w http.ResponseWriter, r *http.Request, subdir string) {
	if s.pluginDir == "" {
		http.Error(w, `{"error":"plugins not configured"}`, http.StatusNotFound)
		return
	}

	filename := chi.URLParam(r, "filename")

	// Security: only serve .js files, reject path traversal attempts
	if !strings.HasSuffix(filename, ".js") ||
		strings.ContainsAny(filename, `/\`) ||
		strings.Contains(filename, "..") {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	filePath := filepath.Join(s.pluginDir, subdir, filename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	// Override the application/json Content-Type set by the router middleware.
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}
