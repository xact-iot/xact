package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

const mobileAppConfigName = "mobile-app"

type mobileAppConfig struct {
	DeviceParentNodes    []string `json:"deviceParentNodes"`
	DefaultDashboardName string   `json:"defaultDashboardName"`
}

type mobileReleaseResponse struct {
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	Notes       string `json:"notes,omitempty"`
}

func (s *Server) handleMobileReleaseWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleMobileRelease, nil, mobileReleaseResponse{}, "mobile")
}

// handleMobileRelease returns the configured self-hosted Android release.
// Authentication is applied by the protected route group. Deployments opt in
// with MOBILE_APP_VERSION and MOBILE_APK_URL; an absent configuration is a 404
// so older and non-mobile installations remain compatible.
func (s *Server) handleMobileRelease(w http.ResponseWriter, _ *http.Request) {
	version := strings.TrimSpace(os.Getenv("MOBILE_APP_VERSION"))
	downloadURL := strings.TrimSpace(os.Getenv("MOBILE_APK_URL"))
	if version == "" || downloadURL == "" {
		http.Error(w, `{"error":"mobile release not configured"}`, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(mobileReleaseResponse{
		Version:     version,
		DownloadURL: downloadURL,
		Notes:       strings.TrimSpace(os.Getenv("MOBILE_RELEASE_NOTES")),
	})
}

func (s *Server) handleMobileAppConfigWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleMobileAppConfig, nil, mobileAppConfig{}, "mobile")
}

func (s *Server) handleUpdateMobileAppConfigWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleUpdateMobileAppConfig, mobileAppConfig{}, mobileAppConfig{}, "mobile")
}

// mobileConfigOrg returns the current organisation unless a SystemAdmin has
// explicitly selected another one. Other roles cannot use the org override.
func mobileConfigOrg(r *http.Request) (string, bool) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.TenantID) == "" {
		return "", false
	}
	requested := strings.TrimSpace(r.URL.Query().Get("org"))
	if requested == "" || requested == claims.TenantID {
		return claims.TenantID, true
	}
	if !claimsHasSystemAdmin(claims) {
		return "", false
	}
	return requested, true
}

func (s *Server) handleMobileAppConfig(w http.ResponseWriter, r *http.Request) {
	org, ok := mobileConfigOrg(r)
	if !ok {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	if s.db == nil {
		http.Error(w, `{"error":"database unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	raw, err := s.db.LoadConfig(r.Context(), org, mobileAppConfigName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	config := mobileAppConfig{DeviceParentNodes: []string{}}
	if len(raw) > 0 && json.Unmarshal(raw, &config) != nil {
		http.Error(w, `{"error":"invalid stored mobile app configuration"}`, http.StatusInternalServerError)
		return
	}
	if config.DeviceParentNodes == nil {
		config.DeviceParentNodes = []string{}
	}
	_ = json.NewEncoder(w).Encode(config)
}

func (s *Server) handleUpdateMobileAppConfig(w http.ResponseWriter, r *http.Request) {
	org, ok := mobileConfigOrg(r)
	if !ok {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	if s.db == nil {
		http.Error(w, `{"error":"database unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	var config mobileAppConfig
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	config.DefaultDashboardName = strings.TrimSpace(config.DefaultDashboardName)
	if len(config.DefaultDashboardName) > 200 || len(config.DeviceParentNodes) > 100 {
		http.Error(w, `{"error":"configuration is too large"}`, http.StatusBadRequest)
		return
	}
	seen := make(map[string]bool, len(config.DeviceParentNodes))
	parents := make([]string, 0, len(config.DeviceParentNodes))
	for _, value := range config.DeviceParentNodes {
		path := strings.Trim(strings.TrimSpace(value), ". /")
		if path == "" || len(path) > 500 {
			http.Error(w, `{"error":"device parent nodes must be valid paths"}`, http.StatusBadRequest)
			return
		}
		// Persist org-relative paths so the same shape is returned to web and
		// mobile clients without leaking another organisation's root.
		path = strings.ReplaceAll(path, "/", ".")
		if path == org {
			path = ""
		} else if strings.HasPrefix(path, org+".") {
			path = strings.TrimPrefix(path, org+".")
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		parents = append(parents, path)
	}
	config.DeviceParentNodes = parents
	raw, _ := json.Marshal(config)
	if err := s.db.SaveConfig(r.Context(), org, mobileAppConfigName, raw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(config)
}
