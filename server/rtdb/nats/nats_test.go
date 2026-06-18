package nats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/rtdb/tree"
)

type testProcessBlock struct{}

func (testProcessBlock) GetType() string { return "stalecheck" }
func (testProcessBlock) Init(tree.Leaf)  {}
func (testProcessBlock) Close(tree.Leaf) {}
func (testProcessBlock) Process(_ tree.Leaf, value any) (any, error) {
	return value, nil
}
func (testProcessBlock) GetParameters() json.RawMessage {
	return json.RawMessage(`{"timeoutSeconds":30}`)
}
func (testProcessBlock) SetParameters(json.RawMessage) error { return nil }
func (testProcessBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{Type: "stalecheck"}
}

func requireEmbeddedServer(t *testing.T) *testEmbeddedServer {
	t.Helper()
	cfg := testDefaultConfig()
	cfg.StoreDir = t.TempDir()

	server, err := newTestEmbeddedServer(cfg)
	if err != nil {
		if strings.Contains(err.Error(), "NATS server failed to start") {
			t.Skipf("embedded NATS server is not available in this environment: %v", err)
		}
		t.Fatalf("Failed to create embedded server: %v", err)
	}
	return server
}

func TestEmbeddedServer(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	// Test that server is running
	if server.ClientURL() == "" {
		t.Error("Server URL should not be empty")
	}

	// Test basic publish/subscribe
	received := make(chan bool, 1)
	sub, err := server.Subscribe("test.subject", func(msg *nats.Msg) {
		received <- true
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Publish
	err = server.Publish("test.subject", []byte("test"))
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case <-received:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestHistoryStore(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	// Create history store with native KV history (keep 10 versions)
	hs, err := NewHistoryStore(server.JetStream(), 10)
	if err != nil {
		t.Fatalf("Failed to create history store: %v", err)
	}

	// Record some values - NATS KV handles versioning
	path := "device1.temperature"
	for i := 0; i < 5; i++ {
		val := float64(20 + i)
		err := hs.Record(path, val)
		if err != nil {
			t.Fatalf("Failed to record value: %v", err)
		}
	}

	// Get current value
	current, err := hs.Get(path)
	if err != nil {
		t.Fatalf("Failed to get current value: %v", err)
	}
	if current == nil {
		t.Fatal("Current value should not be nil")
	}
	// Type assert to check the value
	if floatVal, ok := current.Value.(float64); !ok || floatVal != 24 { // Last value (20+4)
		t.Errorf("Expected current value 24, got %v", current.Value)
	}

	// Get history from NATS KV native versioning
	history, err := hs.GetHistory(path)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(history) != 5 {
		t.Errorf("Expected 5 history entries from NATS KV, got %d", len(history))
	}

	// Check values are in order (oldest to newest)
	for i, entry := range history {
		expected := float64(20 + i)
		if floatVal, ok := entry.Value.(float64); !ok || floatVal != expected {
			t.Errorf("History[%d]: expected %f, got %v", i, expected, entry.Value)
		}
	}
}

func TestHistoryStoreDepthLimit(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	// Create history store with small depth
	hs, err := NewHistoryStore(server.JetStream(), 3)
	if err != nil {
		t.Fatalf("Failed to create history store: %v", err)
	}

	// Record more values than depth
	path := "device1.temperature"
	for i := 0; i < 10; i++ {
		val := float64(100 + i)
		hs.Record(path, val)
	}

	// Get history - should be limited to depth
	history, _ := hs.GetHistory(path)
	if len(history) > 3 {
		t.Errorf("Expected max 3 history entries, got %d", len(history))
	}

	// The last value in history should be the most recent (109)
	if len(history) > 0 {
		if floatVal, ok := history[len(history)-1].Value.(float64); !ok || floatVal != 109 {
			t.Errorf("Expected last value 109, got %v", history[len(history)-1].Value)
		}
	}
}

func TestTreeSync(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	// Create tree sync - empty basePath means path IS the topic
	sync := NewTreeSync(server.Conn(), "")

	// Test subscribing - subscribe to the path as topic
	received := make(chan string, 1)
	sub, err := sync.SubscribeToTree(func(path string, data []byte) {
		received <- path
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Test publishing - topic should be the path
	val := 25.5
	err = sync.PublishValue("device1.temp", val, "")
	if err != nil {
		t.Fatalf("Failed to publish value: %v", err)
	}

	// Publish again
	err = sync.PublishValue("device1.temp", val, "")
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for message
	select {
	case path := <-received:
		if path != "device1.temp" {
			t.Errorf("Expected path device1.temp, got %s", path)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for message")
	}
}

func TestTreeSyncPublishChangeIncludesNodeTemplateName(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	sync := NewTreeSync(server.Conn(), "")

	received := make(chan []byte, 1)
	sub, err := server.Subscribe("default.Templates.AirQualityStandard", func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	node := tree.NewNode("AirQualityStandard")
	node.SetDescription("Template for standard air quality devices")
	node.SetTemplateName("Templates.AirQualityStandard")
	if err := sync.PublishChange("default.Templates.AirQualityStandard", node); err != nil {
		t.Fatalf("PublishChange: %v", err)
	}

	select {
	case data := <-received:
		var event struct {
			Type         string `json:"type"`
			Name         string `json:"name"`
			Description  string `json:"description"`
			TemplateName string `json:"templateName"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if event.Type != "node" {
			t.Fatalf("type = %q, want node", event.Type)
		}
		if event.TemplateName != "Templates.AirQualityStandard" {
			t.Fatalf("templateName = %q, want Templates.AirQualityStandard", event.TemplateName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for node change")
	}
}

func TestTreeSyncPublishChangeIncludesTagPipeline(t *testing.T) {
	server := requireEmbeddedServer(t)
	defer server.Shutdown()

	sync := NewTreeSync(server.Conn(), "")

	received := make(chan []byte, 1)
	sub, err := server.Subscribe("default.Device.temperature", func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	leaf := tree.NewFloatLeaf("temperature", tree.TagConfig{Name: "temperature", Type: tree.TypeFloat}, tree.TagShared{
		Description: "Temperature",
		Units:       "degC",
		Pipeline:    []tree.ProcessBlock{testProcessBlock{}},
	})
	if err := sync.PublishChange("default.Device.temperature", leaf); err != nil {
		t.Fatalf("PublishChange: %v", err)
	}

	select {
	case data := <-received:
		var event struct {
			Type   string `json:"type"`
			Shared struct {
				Description string                      `json:"description"`
				Units       string                      `json:"units"`
				Pipeline    []tree.ProcessBlockEnvelope `json:"pipeline"`
			} `json:"shared"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if event.Type != "leaf" {
			t.Fatalf("type = %q, want leaf", event.Type)
		}
		if event.Shared.Description != "Temperature" {
			t.Fatalf("shared.description = %q", event.Shared.Description)
		}
		if event.Shared.Units != "degC" {
			t.Fatalf("shared.units = %q", event.Shared.Units)
		}
		if len(event.Shared.Pipeline) != 1 {
			t.Fatalf("pipeline length = %d, want 1; data=%s", len(event.Shared.Pipeline), data)
		}
		if event.Shared.Pipeline[0].Type != "stalecheck" {
			t.Fatalf("pipeline type = %q, want stalecheck", event.Shared.Pipeline[0].Type)
		}
		var params map[string]int
		if err := json.Unmarshal(event.Shared.Pipeline[0].Parameters, &params); err != nil {
			t.Fatalf("unmarshal pipeline params: %v", err)
		}
		if params["timeoutSeconds"] != 30 {
			t.Fatalf("timeoutSeconds = %d, want 30", params["timeoutSeconds"])
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for tree change")
	}
}

func TestTreeSyncSubjectForPathNormalizesEmptySegments(t *testing.T) {
	sync := NewTreeSync(nil, "rtdb.tree.")

	tests := map[string]string{
		"default.BENCH.Bench000099.status":      "rtdb.tree.default.BENCH.Bench000099.status",
		".default.BENCH.Bench000099.status":     "rtdb.tree.default.BENCH.Bench000099.status",
		"/default/BENCH/Bench000099/status":     "rtdb.tree.default.BENCH.Bench000099.status",
		"//default//BENCH//Bench000099//status": "rtdb.tree.default.BENCH.Bench000099.status",
		">":                                     "rtdb.tree.>",
	}

	for path, want := range tests {
		if got := sync.SubjectForPath(path); got != want {
			t.Fatalf("SubjectForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
