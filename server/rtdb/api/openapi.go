package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
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

type openAPIRouteSpec struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Public      bool
	RequestBody map[string]any
	Responses   map[string]any
	Tags        []string
}

type openAPIHandler = openapischema.Handler

type apiRoutes struct {
	router chi.Router
	specs  *[]openAPIRouteSpec
	base   string
	public bool
}

func newAPIRoutes(router chi.Router, specs *[]openAPIRouteSpec, base string, public bool) apiRoutes {
	return apiRoutes{router: router, specs: specs, base: cleanRoutePath(base), public: public}
}

func (r apiRoutes) Group(fn func(apiRoutes)) {
	r.router.Group(func(cr chi.Router) {
		fn(apiRoutes{router: cr, specs: r.specs, base: r.base, public: r.public})
	})
}

func (r apiRoutes) Route(pattern string, fn func(apiRoutes)) {
	r.router.Route(pattern, func(cr chi.Router) {
		fn(apiRoutes{
			router: cr,
			specs:  r.specs,
			base:   joinRoutePath(r.base, pattern),
			public: r.public,
		})
	})
}

func (r apiRoutes) With(middlewares ...func(http.Handler) http.Handler) apiRoutes {
	return apiRoutes{router: r.router.With(middlewares...), specs: r.specs, base: r.base, public: r.public}
}

func (r apiRoutes) Use(middlewares ...func(http.Handler) http.Handler) {
	r.router.Use(middlewares...)
}

func (r apiRoutes) Get(pattern string, handler any) {
	r.register(http.MethodGet, pattern, handler)
}

func (r apiRoutes) Post(pattern string, handler any) {
	r.register(http.MethodPost, pattern, handler)
}

func (r apiRoutes) Put(pattern string, handler any) {
	r.register(http.MethodPut, pattern, handler)
}

func (r apiRoutes) Delete(pattern string, handler any) {
	r.register(http.MethodDelete, pattern, handler)
}

func (r apiRoutes) register(method, pattern string, handler any) {
	h := normalizeOpenAPIHandler(handler)
	r.router.MethodFunc(method, pattern, h.Handler)
	path := canonicalRoutePattern(joinRoutePath(r.base, pattern))
	*r.specs = append(*r.specs, openAPIRouteSpec{
		Method:      method,
		Path:        path,
		OperationID: operationID(strings.ToLower(method), path),
		Summary:     operationSummary(method, path),
		Public:      r.public,
		RequestBody: h.RequestBody,
		Responses:   h.Responses,
		Tags:        h.Tags,
	})
}

func normalizeOpenAPIHandler(handler any) openAPIHandler {
	switch h := handler.(type) {
	case openAPIHandler:
		return h
	case http.HandlerFunc:
		return openAPIHandler{Handler: h, Responses: defaultOpenAPIResponses()}
	case func(http.ResponseWriter, *http.Request):
		return openAPIHandler{Handler: h, Responses: defaultOpenAPIResponses()}
	default:
		panic(fmt.Sprintf("unsupported API handler type %T", handler))
	}
}

func handlerWithSchema(handler http.HandlerFunc, request any, response any, tags ...string) openAPIHandler {
	h := openAPIHandler{
		Handler:   handler,
		Responses: responseSchema(http.StatusOK, response),
		Tags:      tags,
	}
	if request != nil {
		h.RequestBody = jsonRequestBody(request)
	}
	return h
}

func handlerWithResponses(handler http.HandlerFunc, responses map[int]any, tags ...string) openAPIHandler {
	return openAPIHandler{
		Handler:   handler,
		Responses: responseSchemas(responses),
		Tags:      tags,
	}
}

func buildOpenAPIRegistry(specs []openAPIRouteSpec) *openAPIRegistry {
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

	for _, spec := range specs {
		method := strings.ToLower(spec.Method)
		paths := reg.doc["paths"].(map[string]any)
		item, _ := paths[spec.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[spec.Path] = item
		}
		op := map[string]any{
			"operationId": spec.OperationID,
			"summary":     spec.Summary,
			"responses":   defaultOpenAPIResponses(),
		}
		if len(spec.Responses) > 0 {
			op["responses"] = spec.Responses
		}
		if len(spec.RequestBody) > 0 {
			op["requestBody"] = spec.RequestBody
		}
		if len(spec.Tags) > 0 {
			op["tags"] = spec.Tags
		}
		if spec.Public {
			op["security"] = []any{}
		}
		if strings.Contains(spec.Path, "{") {
			op["parameters"] = pathParameters(spec.Path)
		}
		item[method] = op
		reg.operation[spec.OperationID] = openAPIOperation{
			Method:      strings.ToUpper(spec.Method),
			Path:        spec.Path,
			OperationID: spec.OperationID,
			Public:      spec.Public,
		}
	}

	sortOpenAPIPaths(reg.doc)
	return reg
}

func defaultOpenAPIResponses() map[string]any {
	return map[string]any{"200": map[string]any{"description": "Successful response"}}
}

func jsonRequestBody(v any) map[string]any {
	return openapischema.JSONRequestBody(v)
}

func responseSchema(status int, v any) map[string]any {
	return openapischema.ResponseSchema(status, v)
}

func responseSchemas(responses map[int]any) map[string]any {
	return openapischema.ResponseSchemas(responses)
}

func canonicalOpenAPIPath(route, proxyPath string) (string, bool) {
	if proxyPath != "" {
		if route == proxyPath {
			route = "/"
		} else if strings.HasPrefix(route, proxyPath+"/") {
			route = strings.TrimPrefix(route, proxyPath)
		}
	}
	route = canonicalRoutePattern(route)
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

func canonicalRoutePattern(path string) string {
	path = strings.ReplaceAll(path, "/*", "/{path}")
	path = strings.ReplaceAll(path, "{path:.*}", "{path}")
	path = cleanRoutePath(path)
	if path != "/" {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func cleanRoutePath(path string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func joinRoutePath(base, pattern string) string {
	base = cleanRoutePath(base)
	if pattern == "" || pattern == "/" {
		if base == "" {
			return "/"
		}
		return strings.TrimRight(base, "/")
	}
	if base == "" || base == "/" {
		return cleanRoutePath(pattern)
	}
	return cleanRoutePath(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(pattern, "/"))
}

func operationID(method, path string) string {
	if path == "/api/v1/openapi.json" {
		return method + "_api_v1_openapi_json"
	}
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
