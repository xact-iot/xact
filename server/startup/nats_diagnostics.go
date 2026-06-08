package main

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

type natsStartupDiagnostics struct {
	ClientHost    string
	ClientPort    int
	WebSocketHost string
	WebSocketPort int
	StoreDir      string
	LogFile       string
}

func ensureParentDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func logNATSStartupDiagnostics(reason string, srv *natsserver.Server, cfg natsStartupDiagnostics) {
	running := false
	clientURL := ""
	if srv != nil {
		running = srv.Running()
		clientURL = srv.ClientURL()
	}

	log.Printf("%s: running=%t clientURL=%s client=%s:%d websocket=%s:%d store=%s log=%s",
		reason,
		running,
		clientURL,
		cfg.ClientHost,
		cfg.ClientPort,
		cfg.WebSocketHost,
		cfg.WebSocketPort,
		cfg.StoreDir,
		cfg.LogFile,
	)

	logTCPListenDiagnostic("NATS client listener", cfg.ClientHost, cfg.ClientPort, running)
	logTCPListenDiagnostic("NATS WebSocket listener", cfg.WebSocketHost, cfg.WebSocketPort, running)
	logFileTail(cfg.LogFile, 40)
}

func logTCPListenDiagnostic(label, host string, port int, serverRunning bool) {
	if port <= 0 {
		log.Printf("%s diagnostic skipped: invalid port %d", label, port)
		return
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if serverRunning {
		log.Printf("%s diagnostic: server reports running; %s may already be owned by embedded NATS", label, addr)
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("%s diagnostic: %s is not available for binding: %v", label, addr, err)
		return
	}
	if err := ln.Close(); err != nil {
		log.Printf("%s diagnostic: %s appears available, but close failed: %v", label, addr, err)
		return
	}
	log.Printf("%s diagnostic: %s appears available for binding at diagnostic time", label, addr)
}

func logFileTail(path string, maxLines int) {
	tail, err := tailFile(path, maxLines)
	if err != nil {
		log.Printf("NATS log tail unavailable for %s: %v", path, err)
		return
	}
	if tail == "" {
		log.Printf("NATS log %s is empty", path)
		return
	}
	log.Printf("Last lines from %s:\n%s", path, tail)
}

func tailFile(path string, maxLines int) (string, error) {
	if strings.TrimSpace(path) == "" || maxLines <= 0 {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return "", nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}
