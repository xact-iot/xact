package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

type publicTagValue struct {
	Value       any    `json:"value"`
	Units       string `json:"units,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Timestamp   int64  `json:"timestamp,omitempty"`
}

type publicWidgetSpec struct {
	Type   string `json:"type"`
	Config struct {
		TagPrefix  string `json:"tagPrefix"`
		TagPath    string `json:"tagPath"`
		MaxTagPath string `json:"maxTagPath"`
		Layers     []struct {
			Enabled             bool   `json:"enabled"`
			PathPattern         string `json:"pathPattern"`
			IconRotationEnabled bool   `json:"iconRotationEnabled"`
			IconRotationTag     string `json:"iconRotationTag"`
			IconRules           []struct {
				Tag string `json:"tag"`
			} `json:"iconRules"`
		} `json:"layers"`
	} `json:"config"`
}

func (s *Server) handlePublicDashboardData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org := chi.URLParam(r, "org")
	if org == "" {
		org = "default"
	}
	// RTDB paths are dot separated; a public URL must select exactly one org.
	if strings.ContainsAny(org, "./\\:* >") {
		http.NotFound(w, r)
		return
	}
	if s.db == nil || s.tree == nil {
		http.NotFound(w, r)
		return
	}
	organisation, err := s.db.GetOrganisation(r.Context(), org)
	if err != nil {
		http.Error(w, "unable to load dashboard", http.StatusInternalServerError)
		return
	}
	if organisation == nil || !organisation.Active {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	dashboard, err := s.db.GetDashboard(r.Context(), org, id)
	if err != nil {
		http.Error(w, "unable to load dashboard", http.StatusInternalServerError)
		return
	}
	public, err := sqldb.PublicDashboard(dashboard)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var widgets []publicWidgetSpec
	if err := json.Unmarshal(public.Widgets, &widgets); err != nil {
		http.NotFound(w, r)
		return
	}

	paths := make(map[string]struct{})
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(strings.SplitN(path, ":", 2)[0]), ".")
		if path == "" || strings.ContainsAny(path, "*/\\ >") || strings.Contains(path, "..") {
			return
		}
		if !strings.HasPrefix(path, org+".") {
			path = org + "." + path
		}
		if len(paths) < 5000 {
			paths[path] = struct{}{}
		}
	}
	for _, widget := range widgets {
		c := widget.Config
		switch widget.Type {
		case "big-number-widget", "gauge-widget":
			if c.TagPath != "" {
				prefix := strings.Trim(c.TagPrefix, ".")
				if prefix == "" || strings.HasPrefix(c.TagPath, org+".") || strings.HasPrefix(c.TagPath, prefix+".") {
					add(c.TagPath)
				} else if strings.Contains(prefix, "*") {
					for _, base := range s.expandPublicPattern(org, prefix, 1000) {
						add(base + "." + c.TagPath)
					}
				} else {
					add(prefix + "." + c.TagPath)
				}
			}
			if c.MaxTagPath != "" {
				add(c.MaxTagPath)
			}
		case "area-map-widget":
			for _, layer := range c.Layers {
				if !layer.Enabled || layer.PathPattern == "" {
					continue
				}
				for _, device := range s.expandPublicPattern(org, layer.PathPattern, 1000) {
					add(device + ".meta.lat")
					add(device + ".meta.lon")
					if layer.IconRotationEnabled && layer.IconRotationTag != "" {
						add(publicDeviceTag(org, device, layer.IconRotationTag))
					}
					for _, rule := range layer.IconRules {
						if rule.Tag != "" {
							add(publicDeviceTag(org, device, rule.Tag))
						}
					}
				}
			}
		}
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	values := make(map[string]publicTagValue, len(names))
	for _, path := range names {
		leaf, err := s.tree.FindLeaf(path)
		if err != nil {
			continue
		}
		shared := leaf.GetShared()
		values[path] = publicTagValue{
			Value: leaf.GetAnyValue(), Units: shared.Units,
			Description: leaf.GetDescription(), Status: leaf.GetState(),
			Timestamp: leaf.GetUpdatedTime().UnixMilli(),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(values)
}

func publicDeviceTag(org, device, tag string) string {
	if strings.HasPrefix(tag, org+".") {
		return tag
	}
	return device + "." + tag
}

func (s *Server) expandPublicPattern(org, pattern string, max int) []string {
	pattern = strings.Trim(pattern, ".")
	if pattern == "" || strings.ContainsAny(pattern, "/\\: >") || strings.Contains(pattern, "..") {
		return nil
	}
	if !strings.HasPrefix(pattern, org+".") {
		pattern = org + "." + pattern
	}
	parts := strings.Split(pattern, ".")
	result := make([]string, 0)
	visited := 0
	var walk func(prefix string, index int)
	walk = func(prefix string, index int) {
		visited++
		if visited > 10000 || len(result) >= max {
			return
		}
		if index == len(parts) {
			result = append(result, prefix)
			return
		}
		if parts[index] != "*" {
			next := parts[index]
			if prefix != "" {
				next = prefix + "." + next
			}
			walk(next, index+1)
			return
		}
		node, err := s.tree.FindNode(prefix)
		if err != nil {
			return
		}
		names := make([]string, 0)
		for name, child := range node.GetChildren() {
			if child.IsNode() {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			walk(prefix+"."+name, index+1)
		}
	}
	walk("", 0)
	return result
}
