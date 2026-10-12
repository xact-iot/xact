package api

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handlePublicBusUpload stages a browser-selected ZIP for the database-backed
// public bus application. The following import_start request performs parsing.
func (s *Server) handlePublicBusUpload(w http.ResponseWriter, r *http.Request) {
	ctx, claims, ok := s.applicationContext(r.Context(), mustClaims(r.Context()))
	if !ok {
		unauthorized(w)
		return
	}
	if !s.checkUIPermission(ctx, "public-bus-config", "manage") {
		busError(w, http.StatusForbidden, "Bus configuration permission denied")
		return
	}
	if claims.TenantID != "default" || r.URL.Query().Get("scope") != "default" {
		busError(w, http.StatusBadRequest, "Unsupported public bus scope")
		return
	}
	filename := r.URL.Query().Get("filename")
	if len(filename) == 0 || len(filename) > 255 || filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\\\x00`) || !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		busError(w, http.StatusBadRequest, "Choose a GTFS ZIP file")
		return
	}
	root := strings.TrimSpace(os.Getenv("PUBLIC_BUS_IMPORT_DIR"))
	if root == "" {
		busError(w, http.StatusServiceUnavailable, "Public bus import directory is not configured")
		return
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		busError(w, http.StatusServiceUnavailable, "Public bus import directory unavailable")
		return
	}
	file, err := os.CreateTemp(root, ".upload-*.zip")
	if err != nil {
		busError(w, http.StatusServiceUnavailable, "Could not stage GTFS archive")
		return
	}
	defer os.Remove(file.Name())
	r.Body = http.MaxBytesReader(w, r.Body, maxBusGTFSZipBytes)
	size, copyErr := io.Copy(file, r.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		if copyErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(copyErr, &maxErr) {
				busError(w, http.StatusRequestEntityTooLarge, "GTFS ZIP exceeds 25 MB")
				return
			}
		}
		busError(w, http.StatusBadRequest, "Could not read GTFS ZIP")
		return
	}
	if size == 0 {
		busError(w, http.StatusBadRequest, "GTFS ZIP is empty")
		return
	}
	archive, err := zip.OpenReader(file.Name())
	if err != nil || len(archive.File) == 0 {
		if archive != nil {
			archive.Close()
		}
		busError(w, http.StatusBadRequest, "Invalid GTFS ZIP")
		return
	}
	archive.Close()
	id := busID()
	if id == "" {
		busError(w, http.StatusInternalServerError, "Could not identify GTFS archive")
		return
	}
	staged := id + ".zip"
	if err := os.Rename(file.Name(), filepath.Join(root, staged)); err != nil {
		busError(w, http.StatusServiceUnavailable, "Could not stage GTFS archive")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"archive": staged, "filename": filename})
}
