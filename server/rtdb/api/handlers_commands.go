package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/events"
	xactnats "github.com/xact-iot/xact/rtdb/nats"
)

type commandRequest struct {
	TagPath string `json:"tagPath"`
	Value   any    `json:"value"`
	Timeout int    `json:"timeout"`
}

type commandResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (s *Server) handleCommandWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleCommand, commandRequest{}, commandResponse{}, "commands")
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if s.nc == nil {
		http.Error(w, "NATS connection is unavailable", http.StatusServiceUnavailable)
		return
	}
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok || claims.TenantID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceName := strings.TrimSpace(chi.URLParam(r, "deviceName"))
	if !validCommandSubjectComponent(deviceName) {
		http.Error(w, "invalid deviceName", http.StatusBadRequest)
		return
	}

	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.TagPath = strings.TrimSpace(req.TagPath)
	if req.TagPath == "" || req.Value == nil {
		http.Error(w, "tagPath and value are required", http.StatusBadRequest)
		return
	}
	if v, ok := req.Value.(string); ok && strings.TrimSpace(v) == "" {
		http.Error(w, "value is required", http.StatusBadRequest)
		return
	}

	relativePath := commandRelativePath(claims.TenantID, deviceName, req.TagPath)
	if relativePath == "" {
		http.Error(w, "tagPath is required", http.StatusBadRequest)
		return
	}
	fullPath := claims.TenantID + "." + deviceName + "." + relativePath
	if !claimsPathInTenant(claims, fullPath) {
		http.Error(w, "path is outside your organisation scope", http.StatusForbidden)
		return
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()

	id := newCommandID()
	payload, err := json.Marshal(map[string]any{
		"id":         id,
		relativePath: req.Value,
	})
	if err != nil {
		http.Error(w, "failed to marshal command", http.StatusInternalServerError)
		return
	}

	subject := xactnats.CommandSubjectPrefix + claims.TenantID + "." + deviceName
	msg, err := s.nc.RequestWithContext(ctx, subject, payload)
	if err != nil {
		s.auditCommand(r.Context(), claims, events.Error, deviceName, relativePath, subject, "Command request failed", err.Error())
		http.Error(w, fmt.Sprintf("command request failed: %v", err), http.StatusGatewayTimeout)
		return
	}

	var resp commandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		s.auditCommand(r.Context(), claims, events.Error, deviceName, relativePath, subject, "Command response was invalid", string(msg.Data))
		http.Error(w, "invalid command response", http.StatusBadGateway)
		return
	}
	if resp.ID != "" && resp.ID != id {
		s.auditCommand(r.Context(), claims, events.Error, deviceName, relativePath, subject, "Command response id mismatch", resp.Message)
		http.Error(w, "command response id mismatch", http.StatusBadGateway)
		return
	}
	severity := events.Info
	status := "succeeded"
	if !resp.Success {
		severity = events.Error
		status = "failed"
	}
	result := resp.Message
	if result == "" {
		result = status
	}
	s.auditCommand(r.Context(), claims, severity, deviceName, relativePath, subject, fmt.Sprintf("Command %s for %s.%s", status, deviceName, relativePath), result)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) auditCommand(ctx context.Context, claims *JWTClaims, severity events.Severity, deviceName, relativePath, subject, message, result string) {
	if claims == nil {
		return
	}
	userID, _ := strconv.Atoi(claims.UserID)
	s.auditSecurityEvent(ctx, claims.TenantID, userID, severity, "commands", message, map[string]any{
		"deviceName": deviceName,
		"tagPath":    relativePath,
		"subject":    subject,
		"result":     result,
	})
}

func commandRelativePath(org, deviceName, tagPath string) string {
	path := strings.Trim(strings.TrimSpace(tagPath), ". /")
	path = strings.ReplaceAll(path, "/", ".")
	for _, prefix := range []string{
		strings.Trim(strings.TrimSpace(org), ".") + "." + strings.Trim(strings.TrimSpace(deviceName), ".") + ".",
		strings.Trim(strings.TrimSpace(deviceName), ".") + ".",
	} {
		if prefix != "." && strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func validCommandSubjectComponent(value string) bool {
	if value == "" || strings.ContainsAny(value, "*>/\\") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func newCommandID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
