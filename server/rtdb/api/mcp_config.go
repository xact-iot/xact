package api

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xact-iot/xact/mcp"
)

// MCPConfigFromEnv builds embedded MCP configuration from environment
// variables. MCP is disabled by default; write tools have a separate gate.
func MCPConfigFromEnv(appVersion string) mcp.Config {
	timeout := envDurationSeconds("MCP_TOOL_TIMEOUT_SECONDS", 30*time.Second)
	maxPayload := envInt64("MCP_MAX_PAYLOAD_BYTES", 1<<20)
	route := strings.TrimSpace(os.Getenv("MCP_ROUTE"))
	if route == "" {
		route = "/api/v1/mcp"
	}
	return mcp.Config{
		Enabled:         envBool("MCP_ENABLED", false),
		Route:           route,
		WriteTools:      envBool("MCP_WRITE_TOOLS_ENABLED", false),
		ToolTimeout:     timeout,
		MaxPayloadBytes: maxPayload,
		ServerName:      "xact",
		ServerVersion:   appVersion,
		DocsRoot:        strings.TrimSpace(os.Getenv("MCP_DOCS_ROOT")),
		ExamplesRoot:    strings.TrimSpace(os.Getenv("MCP_EXAMPLES_ROOT")),
	}
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func envDurationSeconds(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
