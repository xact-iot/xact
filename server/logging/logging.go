// Package logging provides a simple console logger that replaces the standard
// library log/slog functions. It writes colorized, structured output to stderr.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Config controls the console logger behaviour.
type Config struct {
	ShowTime bool // Print date/time prefix (default true)
	Colorize bool // Enable ANSI color output (default true)
}

// DefaultConfig returns sensible defaults (show time + colorize enabled).
func DefaultConfig() Config {
	return Config{
		ShowTime: true,
		Colorize: true,
	}
}

// Logger writes structured log lines to the console.
type Logger struct {
	cfg Config
	mu  sync.Mutex
	w   io.Writer
}

// New creates a Logger with the given config, writing to stderr.
func New(cfg Config) *Logger {
	return &Logger{cfg: cfg, w: os.Stderr}
}

// Debug logs at DEBUG level.
func (l *Logger) Debug(module, org, msg string, args ...any) {
	l.log("DEBUG", module, org, msg, args...)
}

// Info logs at INFO level.
func (l *Logger) Info(module, org, msg string, args ...any) {
	l.log("INFO", module, org, msg, args...)
}

// Warn logs at WARN level.
func (l *Logger) Warn(module, org, msg string, args ...any) {
	l.log("WARN", module, org, msg, args...)
}

// Error logs at ERROR level.
func (l *Logger) Error(module, org, msg string, args ...any) {
	l.log("ERROR", module, org, msg, args...)
}

// Critical logs at CRITICAL level - highest severity for system-threatening conditions.
func (l *Logger) Critical(module, org, msg string, args ...any) {
	l.log("CRITICAL", module, org, msg, args...)
}

// Log writes a line at the given severity. Used by the events package to
// echo events to the console.
func (l *Logger) Log(severity, context, msg string, args ...any) {
	l.log(severity, context, "", msg, args...)
}

// ANSI color codes
const (
	colorReset     = "\033[0m"
	colorCyan      = "\033[36m"
	colorGreen     = "\033[32m"
	colorYellow    = "\033[33m"
	colorRed       = "\033[31m"
	colorBrightRed = "\033[91m"
)

func (l *Logger) log(severity, module, org, msg string, args ...any) {
	// Capture caller: skip log, Debug/Info/…, actual caller
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	fs := runtime.CallersFrames(pcs[:])
	f, _ := fs.Next()

	l.mu.Lock()
	defer l.mu.Unlock()

	color := l.severityColor(severity)

	if l.cfg.ShowTime {
		ts := time.Now().Format("15:04:05.000")
		if l.cfg.Colorize {
			fmt.Fprintf(l.w, "%s %s%-8s%s", ts, color, severity, colorReset)
		} else {
			fmt.Fprintf(l.w, "%s %-8s", ts, severity)
		}
	} else {
		if l.cfg.Colorize {
			fmt.Fprintf(l.w, "%s%-8s%s", color, severity, colorReset)
		} else {
			fmt.Fprintf(l.w, "%-8s", severity)
		}
	}

	if module != "" {
		fmt.Fprintf(l.w, " [%s]", module)
	}
	if org != "" {
		fmt.Fprintf(l.w, " {%s}", org)
	}

	fmt.Fprintf(l.w, " %s", msg)

	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok {
			fmt.Fprintf(l.w, " %s=%v", k, args[i+1])
		}
	}

	// Append call site: filename:line (not full path)
	if f.File != "" {
		fmt.Fprintf(l.w, " @ %s:%d", filepath.Base(f.File), f.Line)
	}

	fmt.Fprintln(l.w)
}

func (l *Logger) severityColor(severity string) string {
	if !l.cfg.Colorize {
		return ""
	}
	switch severity {
	case "CRITICAL":
		return colorBrightRed
	case "ERROR":
		return colorRed
	case "WARN":
		return colorYellow
	case "INFO":
		return colorGreen
	default:
		return colorCyan
	}
}
