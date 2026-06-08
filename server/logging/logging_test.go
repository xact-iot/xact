package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogger_Format(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{cfg: Config{ShowTime: false, Colorize: false}, w: &buf}

	l.Info("api", "default", "Server started", "port", 8080)

	out := buf.String()
	for _, want := range []string{"INFO", "[api]", "{default}", "Server started", "port=8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestLogger_Color(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{cfg: Config{ShowTime: false, Colorize: true}, w: &buf}

	l.Error("db", "", "Connection failed")
	if !strings.Contains(buf.String(), colorRed) {
		t.Error("expected red color for ERROR")
	}

	buf.Reset()
	l.Critical("sys", "", "System failure")
	if !strings.Contains(buf.String(), colorBrightRed) {
		t.Error("expected bright red for CRITICAL")
	}
}

func TestLogger_ShowTime(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{cfg: Config{ShowTime: true, Colorize: false}, w: &buf}

	l.Debug("test", "", "tick")

	// Expect HH:MM:SS.mmm format
	out := buf.String()
	if len(out) < 12 || out[2] != ':' || out[5] != ':' {
		t.Errorf("expected timestamp prefix, got: %s", out)
	}
}

func TestLogger_CallerSite(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{cfg: Config{ShowTime: false, Colorize: false}, w: &buf}

	l.Info("test", "", "hello")

	out := buf.String()
	// Should contain "@ logging_test.go:" (filename only, not full path)
	if !strings.Contains(out, "@ logging_test.go:") {
		t.Errorf("expected caller site with filename only, got: %s", out)
	}
}
