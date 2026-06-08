package backups

import (
	"database/sql"
	"testing"
)

func TestCSVValueStringPreservesByteBackedDashboardJSON(t *testing.T) {
	widgets := []byte(`[{"id":"yt","type":"html-widget","config":{"html":"<div style=\"width:100%;height:100%;overflow:hidden;\"><iframe src=\"https://www.youtube.com/embed/vytmBNhc9ig?autoplay=1&mute=1&controls=0&rel=0&playsinline=1&iv_load_policy=3\" allow=\"autoplay; encrypted-media; picture-in-picture\" allowfullscreen></iframe></div>"}}]`)

	if got := csvValueString(widgets); got != string(widgets) {
		t.Fatalf("csvValueString([]byte) = %q, want original JSON", got)
	}

	raw := sql.RawBytes(widgets)
	if got := csvValueString(raw); got != string(widgets) {
		t.Fatalf("csvValueString(sql.RawBytes) = %q, want original JSON", got)
	}
}
