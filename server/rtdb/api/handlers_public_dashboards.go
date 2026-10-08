package api

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/rtdb/tree"
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
			ItemType            string `json:"itemType"`
			RouteCoordinatesTag string `json:"routeCoordinatesTag"`
			RouteNameTag        string `json:"routeNameTag"`
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
	arrayPaths := make(map[string]struct{})
	// Bound work without silently dropping later layers or half a coordinate pair.
	const maxPublicTags = 100000
	const maxPublicDevices = 20000
	tooLarge := false
	expand := func(pattern string) []string {
		devices, truncated := s.expandPublicPattern(org, pattern, maxPublicDevices)
		tooLarge = tooLarge || truncated
		return devices
	}
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(strings.SplitN(path, ":", 2)[0]), ".")
		if path == "" || strings.ContainsAny(path, "*/\\ >") || strings.Contains(path, "..") {
			return
		}
		if !strings.HasPrefix(path, org+".") {
			path = org + "." + path
		}
		paths[path] = struct{}{}
		tooLarge = tooLarge || len(paths) > maxPublicTags
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
					for _, base := range expand(prefix) {
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
				for _, device := range expand(layer.PathPattern) {
					if layer.ItemType == "route" {
						coordinates := strings.TrimSpace(layer.RouteCoordinatesTag)
						if coordinates == "" {
							coordinates = "route.coordinates"
						}
						name := strings.TrimSpace(layer.RouteNameTag)
						if name == "" {
							name = "route.name"
						}
						coordinatePath := publicDeviceTag(org, device, coordinates)
						add(coordinatePath)
						arrayPaths[coordinatePath] = struct{}{}
						add(publicDeviceTag(org, device, name))
						continue
					}
					add(device + ".meta.lat")
					add(device + ".meta.lon")
					add(device + ".meta.name")
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
	if tooLarge {
		http.Error(w, "public dashboard exceeds the supported tag or device limit", http.StatusRequestEntityTooLarge)
		return
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
			// Native ingest stores coordinate arrays as numbered leaves under
			// an array node. Publish the configured array as one snapshot value.
			if _, ok := arrayPaths[path]; ok {
				if coordinates, ok := s.publicRouteCoordinates(path); ok {
					values[path] = publicTagValue{Value: coordinates}
				}
			}
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

func (s *Server) publicRouteCoordinates(path string) ([]float64, bool) {
	node, err := s.tree.FindNode(path)
	if err != nil || !node.GetIsArray() {
		return nil, false
	}
	children := node.GetChildren()
	if len(children) < 4 || len(children)%2 != 0 {
		return nil, false
	}
	coordinates := make([]float64, len(children))
	for i := range coordinates {
		leaf, ok := children[strconv.Itoa(i)].(tree.Leaf)
		if !ok {
			return nil, false
		}
		var value float64
		switch raw := leaf.GetAnyValue().(type) {
		case float64:
			value = raw
		case int64:
			value = float64(raw)
		default:
			return nil, false
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, false
		}
		coordinates[i] = value
	}
	return coordinates, true
}

func publicDeviceTag(org, device, tag string) string {
	if strings.HasPrefix(tag, org+".") {
		return tag
	}
	// A tag chosen from one example device in a wildcard layer applies to
	// each device. Keep this in step with the map widget's resolveDeviceTag.
	if parentEnd := strings.LastIndex(device, "."); parentEnd >= 0 {
		parent := device[:parentEnd]
		relativeParent := strings.TrimPrefix(parent, org+".")
		if parent != org && strings.HasPrefix(tag, relativeParent+".") {
			afterParent := strings.TrimPrefix(tag, relativeParent+".")
			if childEnd := strings.Index(afterParent, "."); childEnd >= 0 {
				return device + "." + afterParent[childEnd+1:]
			}
		}
	}
	return device + "." + tag
}

func (s *Server) expandPublicPattern(org, pattern string, max int) ([]string, bool) {
	pattern = strings.Trim(pattern, ".")
	if pattern == "" || strings.ContainsAny(pattern, "/\\: >") || strings.Contains(pattern, "..") {
		return nil, false
	}
	if !strings.HasPrefix(pattern, org+".") {
		pattern = org + "." + pattern
	}
	parts := strings.Split(pattern, ".")
	result := make([]string, 0)
	visited := 0
	truncated := false
	var walk func(prefix string, index int)
	walk = func(prefix string, index int) {
		visited++
		if visited > 200000 {
			truncated = true
			return
		}
		if index == len(parts) {
			if len(result) >= max {
				truncated = true
				return
			}
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
	return result, truncated
}
