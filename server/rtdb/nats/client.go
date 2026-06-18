package nats

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/rtdb/tree"
)

// TreeSync handles NATS synchronization for tree changes
type TreeSync struct {
	nc       *nats.Conn
	basePath string
}

// NewTreeSync creates a new tree sync handler
func NewTreeSync(nc *nats.Conn, basePath string) *TreeSync {
	return &TreeSync{
		nc:       nc,
		basePath: basePath,
	}
}

// SubjectForPath creates a NATS subject for a tree path using dot separation
func (ts *TreeSync) SubjectForPath(path string) string {
	dotPath := normalizeSubjectPath(path)
	if dotPath == "" {
		return strings.TrimRight(ts.basePath, ".")
	}
	basePath := strings.TrimRight(ts.basePath, ".")
	if basePath == "" {
		return dotPath
	}
	return fmt.Sprintf("%s.%s", basePath, dotPath)
}

func normalizeSubjectPath(path string) string {
	dotPath := strings.ReplaceAll(path, "/", ".")
	segments := strings.FieldsFunc(dotPath, func(r rune) bool {
		return r == '.'
	})
	return strings.Join(segments, ".")
}

type treeChangeTagShared struct {
	Description       string                      `json:"description,omitempty"`
	Units             string                      `json:"units,omitempty"`
	Deadband          float64                     `json:"deadband,omitempty"`
	EnumValues        map[int]string              `json:"enumValues,omitempty"`
	Pipeline          []tree.ProcessBlockEnvelope `json:"pipeline,omitempty"`
	PipelineInherited bool                        `json:"pipelineInherited,omitempty"`
}

func treeChangeSharedForLeaf(leaf tree.Leaf) *treeChangeTagShared {
	shared := leaf.GetShared()
	out := &treeChangeTagShared{
		Description: leaf.GetDescription(),
		Units:       shared.Units,
		Deadband:    shared.Deadband,
		EnumValues:  shared.EnumValues,
	}
	if pipeline := leaf.GetPipeline(); len(pipeline) > 0 {
		if envelopes, err := tree.MarshalPipeline(pipeline); err == nil {
			out.Pipeline = envelopes
			out.PipelineInherited = len(shared.Pipeline) == 0
		}
	}
	return out
}

// PublishChange publishes a tree change to NATS
func (ts *TreeSync) PublishChange(path string, node tree.TreeNode) error {
	subject := ts.SubjectForPath(path)

	var data []byte
	var err error

	if node == nil {
		// Deletion
		data = []byte(`{"deleted":true}`)
	} else {
		// Serialize node info
		event := struct {
			Type         string          `json:"type"`
			Name         string          `json:"name"`
			Timestamp    int64           `json:"timestamp"`
			Description  string          `json:"description,omitempty"`
			TemplateName string          `json:"templateName,omitempty"`
			Config       *tree.TagConfig `json:"config,omitempty"`
			Shared       any             `json:"shared,omitempty"`
			ValueType    string          `json:"value_type,omitempty"`
			Value        any             `json:"value,omitempty"`
			Status       string          `json:"status"`
			IsArray      bool            `json:"isArray,omitempty"`
		}{
			Type:      "node",
			Name:      node.GetName(),
			Timestamp: time.Now().UnixMilli(),
		}

		if leaf, ok := node.(tree.Leaf); ok {
			event.Type = "leaf"
			config := leaf.GetConfig()
			event.Config = &config
			event.Shared = treeChangeSharedForLeaf(leaf)
			event.Description = leaf.GetDescription()
			event.ValueType = leaf.ValueType().String()
			event.Value = leaf.GetAnyValue()
			event.Status = leaf.GetState()
		} else if n, ok := node.(*tree.Node); ok {
			event.Description = n.GetDescription()
			event.TemplateName = n.GetTemplateName()
			event.IsArray = n.GetIsArray()
		}

		data, err = json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}
	}
	err = ts.nc.Publish(subject, data)
	return err
}

// PublishValue publishes a value change
func (ts *TreeSync) PublishValue(path string, value any, status string) error {
	subject := ts.SubjectForPath(path)
	fmt.Printf("subject %s\n", subject)
	data, err := json.Marshal(struct {
		Type   string `json:"type"`
		Value  any    `json:"value"`
		Status string `json:"status,omitempty"`
	}{
		Type:   "value",
		Value:  value,
		Status: status,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return ts.nc.Publish(subject, data)
}

// Subscribe subscribes to changes for a path pattern
func (ts *TreeSync) Subscribe(pathPattern string, handler func(path string, data []byte)) (*nats.Subscription, error) {
	subject := ts.SubjectForPath(pathPattern)

	return ts.nc.Subscribe(subject, func(msg *nats.Msg) {
		// Extract path from subject
		dotPath := msg.Subject[len(ts.basePath):]
		// path := "/" + strings.ReplaceAll(dotPath, ".", "/")
		handler(dotPath, msg.Data)
	})
}

// SubscribeToTree subscribes to all tree changes
func (ts *TreeSync) SubscribeToTree(handler func(path string, data []byte)) (*nats.Subscription, error) {
	subject := ts.SubjectForPath(">")

	return ts.nc.Subscribe(subject, func(msg *nats.Msg) {
		dotPath := msg.Subject[len(ts.basePath):]
		// path := "/" + strings.ReplaceAll(dotPath, ".", "/")
		handler(dotPath, msg.Data)
	})
}

// TreeRestorer restores tree state from NATS on startup
type TreeRestorer struct {
	nc       *nats.Conn
	basePath string
}

// NewTreeRestorer creates a new tree restorer
func NewTreeRestorer(nc *nats.Conn, basePath string) *TreeRestorer {
	return &TreeRestorer{
		nc:       nc,
		basePath: basePath,
	}
}

// RestoreNode restores a single node from NATS
func (tr *TreeRestorer) RestoreNode(path string) (*tree.Node, error) {
	dotPath := strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", ".")
	subject := tr.basePath + dotPath

	// Request current state
	msg, err := tr.nc.Request(subject, []byte(`{"action":"get"}`), NATSRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to restore node: %w", err)
	}

	// Parse response
	var response struct {
		Exists       bool     `json:"exists"`
		Children     []string `json:"children"`
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		TemplateName string   `json:"templateName"`
	}

	if err := json.Unmarshal(msg.Data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse restore response: %w", err)
	}

	if !response.Exists {
		return nil, nil
	}

	node := tree.NewNode(path)
	node.SetDescription(response.Description)
	node.SetTemplateName(response.TemplateName)

	return node, nil
}

// NATSRequestTimeout is the timeout for NATS requests
const NATSRequestTimeout = 5000 // 5 seconds in milliseconds
