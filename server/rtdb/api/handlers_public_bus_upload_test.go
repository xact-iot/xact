package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicBusUploadStagesZIP(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PUBLIC_BUS_IMPORT_DIR", root)
	var body bytes.Buffer
	archive := zip.NewWriter(&body)
	entry, err := archive.Create("stops.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("stop_id,stop_name\n1,Main\n")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: &busTestDB{newRoleTestDB("admin", "password", "SystemAdmin")}}
	call := func(name string, content []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/public_bus/upload?scope=default&filename="+name, bytes.NewReader(content))
		ctx := context.WithValue(req.Context(), claimsContextKey, &JWTClaims{UserID: "1", TenantID: "default", Roles: []string{"SystemAdmin"}})
		rec := httptest.NewRecorder()
		server.handlePublicBusUpload(rec, req.WithContext(ctx))
		return rec
	}
	rec := call("feed.zip", body.Bytes())
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Archive string `json:"archive"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, result.Archive))
	if err != nil || !bytes.Equal(stored, body.Bytes()) {
		t.Fatalf("staged archive mismatch: %v", err)
	}
	if rec := call("bad.zip", []byte("not a ZIP")); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid ZIP status: %d", rec.Code)
	}
}
