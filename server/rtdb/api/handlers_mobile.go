package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

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
