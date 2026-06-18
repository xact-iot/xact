package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

type openAPIOperation struct {
	Method      string
	Path        string
	OperationID string
	Public      bool
}

type openAPIRegistry struct {
	doc       map[string]any
	operation map[string]openAPIOperation
}

func buildOpenAPIRegistry(router chi.Routes, proxyPath string) *openAPIRegistry {
	reg := &openAPIRegistry{
		doc: map[string]any{
			"openapi": "3.0.0",
			"info": map[string]any{
				"title":       "XACT REST API",
				"description": "REST API for XACT server",
				"version":     "1.0.0",
			},
			"security": []map[string][]string{{"bearerAuth": []string{}}},
			"components": map[string]any{
				"securitySchemes": map[string]any{
					"bearerAuth": map[string]any{
						"type":         "http",
						"scheme":       "bearer",
						"bearerFormat": "JWT",
					},
				},
			},
			"paths": map[string]any{},
		},
		operation: map[string]openAPIOperation{},
	}

	_ = chi.Walk(router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path, ok := canonicalOpenAPIPath(route, proxyPath)
		if !ok {
			return nil
		}
		method = strings.ToLower(method)
		opID := operationID(method, path)
		public := publicOpenAPIPath(path)
		paths := reg.doc["paths"].(map[string]any)
		item, _ := paths[path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[path] = item
		}
		op := map[string]any{
			"operationId": opID,
			"summary":     operationSummary(method, path),
			"responses": map[string]any{
				"200": map[string]any{"description": "Successful response"},
			},
		}
		if public {
			op["security"] = []any{}
		}
		if strings.Contains(path, "{") {
			op["parameters"] = pathParameters(path)
		}
		item[method] = op
		reg.operation[opID] = openAPIOperation{
			Method:      strings.ToUpper(method),
			Path:        path,
			OperationID: opID,
			Public:      public,
		}
		return nil
	})

	sortOpenAPIPaths(reg.doc)
	return reg
}

func canonicalOpenAPIPath(route, proxyPath string) (string, bool) {
	if proxyPath != "" {
		if route == proxyPath {
			route = "/"
		} else if strings.HasPrefix(route, proxyPath+"/") {
			route = strings.TrimPrefix(route, proxyPath)
		}
	}
	route = strings.ReplaceAll(route, "/*", "/{path}")
	route = strings.ReplaceAll(route, "{path:.*}", "{path}")
	if route == "" {
		route = "/"
	}
	switch {
	case route == "/health", route == "/login", route == "/api-docs", route == "/openapi.json":
		return route, true
	case route == "/api/v1/openapi.json":
		return route, true
	case strings.HasPrefix(route, "/api/v1/"):
		return route, true
	default:
		return "", false
	}
}

func publicOpenAPIPath(path string) bool {
	return path == "/health" || path == "/login" || path == "/api-docs" || path == "/openapi.json" || path == "/api/v1/openapi.json" ||
		strings.HasPrefix(path, "/api/v1/bootstrap/") ||
		strings.HasPrefix(path, "/api/v1/plugins/") ||
		strings.HasPrefix(path, "/api/v1/ingest/")
}

func operationID(method, path string) string {
	parts := []string{method}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" || part == "api" || part == "v1" {
			continue
		}
		part = strings.Trim(part, "{}")
		part = strings.ReplaceAll(part, "-", "_")
		if part == "*" || part == "path" {
			part = "by_path"
		}
		parts = append(parts, sanitizeOperationPart(part))
	}
	if len(parts) == 1 {
		parts = append(parts, "root")
	}
	return strings.Join(parts, "_")
}

func sanitizeOperationPart(part string) string {
	var b strings.Builder
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "param"
	}
	return out
}

func operationSummary(method, path string) string {
	return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
}

func pathParameters(path string) []map[string]any {
	seen := map[string]bool{}
	params := []map[string]any{}
	for _, part := range strings.Split(path, "/") {
		if !strings.HasPrefix(part, "{") || !strings.HasSuffix(part, "}") {
			continue
		}
		name := strings.Trim(part, "{}")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		params = append(params, map[string]any{
			"name":     name,
			"in":       "path",
			"required": true,
			"schema":   map[string]any{"type": "string"},
		})
	}
	return params
}

func sortOpenAPIPaths(doc map[string]any) {
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return
	}
	ordered := map[string]any{}
	keys := make([]string, 0, len(paths))
	for key := range paths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ordered[key] = paths[key]
	}
	doc["paths"] = ordered
}

func cloneOpenAPIDoc(doc map[string]any) map[string]any {
	data, _ := json.Marshal(doc)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}
