// Package visualscripts implements XACT-owned visual scripting contracts,
// validation, compilation, and instance-aware asynchronous execution.
package visualscripts

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound   = errors.New("visual script not found")
	ErrConflict   = errors.New("visual script revision conflict")
	ErrQueueFull  = errors.New("visual script instance queue is full")
	ErrNotRunning = errors.New("visual script is not running")
	ErrNotLeader  = errors.New("visual script execution is handled by the cluster leader")
)

const SchemaVersion = 1

type GraphDocument struct {
	SchemaVersion int             `json:"schemaVersion"`
	Settings      GraphSettings   `json:"settings"`
	Nodes         []GraphNode     `json:"nodes"`
	Edges         []GraphEdge     `json:"edges"`
	Annotations   json.RawMessage `json:"annotations,omitempty"`
}

type GraphSettings struct {
	MaxConcurrency int    `json:"maxConcurrency"`
	QueueLimit     int    `json:"queueLimit"`
	ErrorPolicy    string `json:"errorPolicy"`
	TraceLevel     string `json:"traceLevel"`
}

type GraphNode struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	TypeVersion int             `json:"typeVersion"`
	Position    Position        `json:"position"`
	Config      json.RawMessage `json:"config"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type GraphEdge struct {
	ID   string       `json:"id"`
	From EdgeEndpoint `json:"from"`
	To   EdgeEndpoint `json:"to"`
}

type EdgeEndpoint struct {
	NodeID string `json:"nodeId"`
	Port   string `json:"port"`
}

type Message struct {
	ID               string         `json:"msgId"`
	CorrelationID    string         `json:"correlationId"`
	OrgName          string         `json:"orgName"`
	ScriptID         string         `json:"scriptId"`
	ActiveRevision   int            `json:"activeRevision"`
	TriggerNodeID    string         `json:"triggerNodeId"`
	InstanceKey      string         `json:"instanceKey"`
	TriggerTimestamp time.Time      `json:"triggerTimestamp"`
	DevicePath       string         `json:"devicePath,omitempty"`
	TagPath          string         `json:"tagPath,omitempty"`
	Value            any            `json:"value,omitempty"`
	Fields           map[string]any `json:"fields,omitempty"`
}

type Script struct {
	ID             string    `json:"id"`
	OrgName        string    `json:"-"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	DesiredState   string    `json:"desiredState"`
	LatestRevision int       `json:"latestRevision"`
	ActiveRevision *int      `json:"activeRevision,omitempty"`
	BackupRevision *int      `json:"-"`
	HasBackup      bool      `json:"hasBackup"`
	Simulation     bool      `json:"simulation"`
	Activate       bool      `json:"activate"`
	CreatedBy      int       `json:"createdBy,omitempty"`
	UpdatedBy      int       `json:"updatedBy,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	RuntimeState   string    `json:"runtimeState,omitempty"`
	OutOfDate      bool      `json:"outOfDate"`
}

type Revision struct {
	ScriptID         string          `json:"scriptId"`
	OrgName          string          `json:"-"`
	Revision         int             `json:"revision"`
	SchemaVersion    int             `json:"schemaVersion"`
	Graph            GraphDocument   `json:"graph"`
	GraphHash        string          `json:"graphHash"`
	ValidationStatus string          `json:"validationStatus"`
	Diagnostics      []Diagnostic    `json:"diagnostics"`
	CreatedBy        int             `json:"createdBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	Capabilities     []string        `json:"capabilities,omitempty"`
	RawGraph         json.RawMessage `json:"-"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	NodeID   string `json:"nodeId,omitempty"`
	EdgeID   string `json:"edgeId,omitempty"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
}

type ValidationResult struct {
	Valid       bool          `json:"valid"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
	Graph       GraphDocument `json:"graph,omitempty"`
	GraphHash   string        `json:"graphHash,omitempty"`
}

type PortDefinition struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	DataType string `json:"dataType"`
}

type ParameterDefinition struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
}

type NodeDefinition struct {
	Type           string                `json:"type"`
	TypeVersion    int                   `json:"typeVersion"`
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	Category       string                `json:"category"`
	Icon           string                `json:"icon"`
	Inputs         []PortDefinition      `json:"inputs"`
	Outputs        []PortDefinition      `json:"outputs"`
	Parameters     []ParameterDefinition `json:"parameters"`
	RequiredCaps   []string              `json:"requiredCapabilities,omitempty"`
	Available      bool                  `json:"available"`
	UnavailableWhy string                `json:"unavailableReason,omitempty"`
	OutputNode     bool                  `json:"outputNode,omitempty"`
	SimulationSafe bool                  `json:"simulationSafe,omitempty"`
}

type Run struct {
	RunID            string       `json:"runId"`
	OrgName          string       `json:"-"`
	ScriptID         string       `json:"scriptId"`
	ActiveRevision   int          `json:"activeRevision"`
	TriggerNodeID    string       `json:"triggerNodeId"`
	InstanceKey      string       `json:"instanceKey"`
	StartedAt        time.Time    `json:"startedAt"`
	CompletedAt      *time.Time   `json:"completedAt,omitempty"`
	Status           string       `json:"status"`
	DurationMS       int64        `json:"durationMs"`
	FirstErrorNodeID string       `json:"firstErrorNodeId,omitempty"`
	Message          string       `json:"message,omitempty"`
	NodesExecuted    int          `json:"nodesExecuted"`
	ActionsAttempted int          `json:"actionsAttempted"`
	Warnings         int          `json:"warnings"`
	DroppedTraces    int          `json:"droppedTraces"`
	Trace            []TraceEvent `json:"trace,omitempty"`
}

type TraceEvent struct {
	Sequence       int               `json:"sequence"`
	Timestamp      time.Time         `json:"timestamp"`
	NodeID         string            `json:"nodeId"`
	NodeType       string            `json:"nodeType"`
	Port           string            `json:"port,omitempty"`
	Status         string            `json:"status"`
	Value          any               `json:"value,omitempty"`
	Fields         map[string]any    `json:"fields,omitempty"`
	Message        string            `json:"message,omitempty"`
	FormattedTimes map[string]string `json:"formattedTimes,omitempty"`
}

type RuntimeStatus struct {
	ScriptID       string     `json:"scriptId"`
	DesiredState   string     `json:"desiredState"`
	RuntimeState   string     `json:"runtimeState"`
	ActiveRevision *int       `json:"activeRevision,omitempty"`
	LatestRevision int        `json:"latestRevision"`
	QueueDepth     int        `json:"queueDepth"`
	Sequence       uint64     `json:"sequence"`
	LastTriggerAt  *time.Time `json:"lastTriggerAt,omitempty"`
	LastCompleteAt *time.Time `json:"lastCompletionAt,omitempty"`
	ErrorSummary   string     `json:"errorSummary,omitempty"`
}

type Store interface {
	ListVisualScripts(context.Context, string) ([]Script, error)
	ListActivatedVisualScripts(context.Context) ([]Script, error)
	GetVisualScript(context.Context, string, string) (*Script, error)
	CreateVisualScript(context.Context, string, *Script) error
	UpdateVisualScript(context.Context, string, string, string, string, int) error
	DeleteVisualScript(context.Context, string, string) error
	ListVisualScriptRevisions(context.Context, string, string) ([]Revision, error)
	GetVisualScriptRevision(context.Context, string, string, int) (*Revision, error)
	CreateVisualScriptRevision(context.Context, string, string, int, *Revision) error
	SetVisualScriptActiveRevision(context.Context, string, string, *int) error
	SetVisualScriptDesiredState(context.Context, string, string, string) error
	SetVisualScriptOptions(context.Context, string, string, bool, bool, int) error
	SetVisualScriptBackupRevision(context.Context, string, string, *int, int) error
	AppendVisualScriptRun(context.Context, *Run) error
	CompleteVisualScriptRun(context.Context, *Run) error
	CancelIncompleteVisualScriptRuns(context.Context, time.Time, string) error
	ClearVisualScriptRuns(context.Context, string, string) error
	ListVisualScriptRuns(context.Context, string, string, int) ([]Run, error)
	GetVisualScriptRun(context.Context, string, string, string) (*Run, error)
}

type CreateScriptRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateScriptRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Simulation  *bool   `json:"simulation,omitempty"`
	Activate    *bool   `json:"activate,omitempty"`
}

type SaveRevisionRequest struct {
	BaseRevision int           `json:"baseRevision"`
	Graph        GraphDocument `json:"graph"`
}

type ValidateRequest struct {
	Graph GraphDocument `json:"graph"`
}

type DeployRequest struct {
	Revision int `json:"revision"`
}

type RunRequest struct {
	TriggerNodeID string         `json:"triggerNodeId,omitempty"`
	InstanceKey   string         `json:"instanceKey,omitempty"`
	DevicePath    string         `json:"devicePath,omitempty"`
	TagPath       string         `json:"tagPath,omitempty"`
	Value         any            `json:"value,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
}
