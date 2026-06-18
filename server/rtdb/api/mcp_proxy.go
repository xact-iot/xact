package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xact-iot/xact/mcp"
)

func (s *Server) mcpAPIContext(ctx context.Context) (any, error) {
	claims, _ := GetClaimsFromContext(ctx)
	out := map[string]any{
		"openapi":       s.OpenAPIDocument(),
		"basePath":      s.config.ProxyPath,
		"auth":          "MCP requests are executed by xact_api_proxy as the authenticated MCP caller; JWTs are not exposed.",
		"serverVersion": s.config.AppVersion,
	}
	if claims != nil {
		out["currentOrg"] = claims.TenantID
		out["roles"] = claims.Roles
		out["allowedOrgs"] = claims.AllowedOrgs
		out["tokenType"] = claims.TokenType
	}
	return out, nil
}

func (s *Server) mcpAPIProxy(ctx context.Context, req mcp.APIProxyRequest) (mcp.APIProxyResponse, error) {
	op, method, path, err := s.resolveProxyOperation(req)
	if err != nil {
		return mcp.APIProxyResponse{}, err
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		if !s.config.MCP.WriteTools {
			return mcp.APIProxyResponse{}, errors.New("MCP write tools are disabled")
		}
		if !req.Confirm {
			return mcp.APIProxyResponse{}, errors.New("confirm=true is required for mutating API proxy calls")
		}
	}
	if strings.HasPrefix(path, "/api/v1/mcp") {
		return mcp.APIProxyResponse{}, errors.New("MCP endpoint cannot be called through xact_api_proxy")
	}
	path, err = applyProxyPathParams(path, req.PathParams)
	if err != nil {
		return mcp.APIProxyResponse{}, err
	}
	targets := s.proxyDispatchTargets(path)
	if len(req.Query) > 0 {
		values := url.Values{}
		for key, value := range req.Query {
			addQueryValue(values, key, value)
		}
		if encoded := values.Encode(); encoded != "" {
			for i := range targets {
				targets[i] += "?" + encoded
			}
		}
	}

	body := []byte(nil)
	if len(req.Body) > 0 && string(req.Body) != "null" {
		body = req.Body
	}
	authToken := ""
	if !op.Public {
		token, err := s.internalJWTForContext(ctx)
		if err != nil {
			return mcp.APIProxyResponse{}, err
		}
		authToken = token
	}

	rr := s.dispatchProxyRequest(ctx, method, targets, body, authToken)
	resp := rr.Result()
	defer resp.Body.Close()

	var responseBody any
	raw := rr.Body.Bytes()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &responseBody); err != nil {
			responseBody = strings.TrimSpace(string(raw))
		}
	}
	headers := map[string]any{}
	for key, values := range resp.Header {
		if len(values) == 1 {
			headers[key] = values[0]
		} else if len(values) > 1 {
			headers[key] = values
		}
	}
	return mcp.APIProxyResponse{Status: resp.StatusCode, Headers: headers, Body: responseBody}, nil
}

func (s *Server) dispatchProxyRequest(ctx context.Context, method string, targets []string, body []byte, authToken string) *httptest.ResponseRecorder {
	var last *httptest.ResponseRecorder
	dispatchCtx := proxyDispatchContext(ctx)
	for _, target := range targets {
		httpReq := httptest.NewRequest(method, target, bytes.NewReader(body))
		httpReq = httpReq.WithContext(dispatchCtx)
		if len(body) > 0 {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		if authToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+authToken)
		}
		rr := httptest.NewRecorder()
		s.router.ServeHTTP(rr, httpReq)
		if rr.Code != http.StatusNotFound {
			return rr
		}
		last = rr
	}
	return last
}

func proxyDispatchContext(ctx context.Context) context.Context {
	dispatchCtx := context.Background()
	if claims, ok := GetClaimsFromContext(ctx); ok {
		dispatchCtx = context.WithValue(dispatchCtx, claimsContextKey, claims)
	}
	return dispatchCtx
}

func (s *Server) proxyDispatchTargets(path string) []string {
	targets := []string{}
	add := func(target string) {
		if target == "" {
			return
		}
		for _, existing := range targets {
			if existing == target {
				return
			}
		}
		targets = append(targets, target)
	}
	withSlashVariants := func(target string) {
		add(target)
		if target != "/" {
			trimmed := strings.TrimRight(target, "/")
			add(trimmed)
			add(trimmed + "/")
		}
	}
	if s.config.ProxyPath != "" {
		withSlashVariants(strings.TrimRight(s.config.ProxyPath, "/") + path)
	}
	withSlashVariants(path)
	return targets
}

func (s *Server) resolveProxyOperation(req mcp.APIProxyRequest) (openAPIOperation, string, string, error) {
	if s.openapi == nil {
		return openAPIOperation{}, "", "", errors.New("OpenAPI registry is not configured")
	}
	if req.OperationID != "" {
		op, ok := s.openapi.operation[req.OperationID]
		if !ok {
			return openAPIOperation{}, "", "", fmt.Errorf("unknown operationId %q", req.OperationID)
		}
		return op, op.Method, op.Path, nil
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	path := strings.TrimSpace(req.Path)
	if method == "" || path == "" {
		return openAPIOperation{}, "", "", errors.New("method and path are required when operationId is omitted")
	}
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "://") {
		return openAPIOperation{}, "", "", errors.New("path must be a local API path")
	}
	for _, op := range s.openapi.operation {
		if op.Method == method && equivalentProxyPath(op.Path, path) {
			return op, method, op.Path, nil
		}
	}
	return openAPIOperation{}, "", "", fmt.Errorf("%s %s is not present in the OpenAPI document", method, path)
}

func equivalentProxyPath(openAPIPath, requestPath string) bool {
	if openAPIPath == requestPath {
		return true
	}
	if openAPIPath != "/" && strings.TrimRight(openAPIPath, "/") == strings.TrimRight(requestPath, "/") {
		return true
	}
	return false
}

func applyProxyPathParams(path string, params map[string]any) (string, error) {
	out := path
	for strings.Contains(out, "{") {
		start := strings.Index(out, "{")
		end := strings.Index(out[start:], "}")
		if start < 0 || end < 0 {
			break
		}
		end += start
		name := out[start+1 : end]
		value, ok := params[name]
		if !ok {
			return "", fmt.Errorf("missing path parameter %q", name)
		}
		out = out[:start] + encodeProxyPathParam(name, fmt.Sprint(value)) + out[end+1:]
	}
	return out, nil
}

func encodeProxyPathParam(name, value string) string {
	value = strings.Trim(value, "/")
	if name == "path" {
		value = strings.ReplaceAll(value, ".", "/")
		parts := strings.Split(value, "/")
		for i, part := range parts {
			parts[i] = url.PathEscape(part)
		}
		return strings.Join(parts, "/")
	}
	return url.PathEscape(value)
}

func addQueryValue(values url.Values, key string, value any) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			values.Add(key, fmt.Sprint(item))
		}
	case []string:
		for _, item := range v {
			values.Add(key, item)
		}
	default:
		values.Set(key, fmt.Sprint(value))
	}
}

func (s *Server) internalJWTForContext(ctx context.Context) (string, error) {
	claims, ok := GetClaimsFromContext(ctx)
	if !ok {
		return "", errors.New("missing MCP caller claims")
	}
	tokenClaims := JWTClaims{
		UserID:       claims.UserID,
		Username:     claims.Username,
		TenantID:     claims.TenantID,
		Roles:        claims.Roles,
		AllowedOrgs:  claims.AllowedOrgs,
		TokenVersion: claims.TokenVersion,
		TokenType:    claims.TokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "xact-mcp-proxy",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
	return token.SignedString(s.jwtSecret)
}
