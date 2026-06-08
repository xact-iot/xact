package scheduler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/xact-iot/xact/backups"
	eventspkg "github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/reporting"
	xactnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/sqldb"
)

// RunContext holds everything an executor needs to run one task.
type RunContext struct {
	DB               sqldb.DB
	NC               *nats.Conn
	Org              string
	Task             sqldb.ScheduledTask
	FiredAt          time.Time
	AllowUnsafeTasks bool
}

// Run dispatches to the appropriate executor based on task type.
// Returns the output file path (empty for shell/yaegi tasks) and any error.
func Run(ctx context.Context, rc RunContext) (string, error) {
	switch rc.Task.TaskType {
	case "report":
		return runReport(ctx, rc)
	case "backup":
		return runBackup(ctx, rc)
	case "shell":
		if !rc.AllowUnsafeTasks {
			return "", fmt.Errorf("scheduler task type %q is disabled by server configuration", rc.Task.TaskType)
		}
		return "", runShell(ctx, rc)
	case "yaegi":
		if !rc.AllowUnsafeTasks {
			return "", fmt.Errorf("scheduler task type %q is disabled by server configuration", rc.Task.TaskType)
		}
		return "", runYaegi(ctx, rc)
	case "command":
		return "", runCommand(ctx, rc)
	default:
		return "", fmt.Errorf("unknown task type %q", rc.Task.TaskType)
	}
}

func IsUnsafeTaskType(taskType string) bool {
	switch taskType {
	case "shell", "yaegi":
		return true
	default:
		return false
	}
}

// ── Task config types ─────────────────────────────────────────────────────────

type reportTaskConfig struct {
	TemplateID string            `json:"templateId"`
	Variables  map[string]string `json:"variables"`
	OutputDir  string            `json:"outputDir"`
}

type backupTaskConfig struct {
	OutputDir string `json:"outputDir"`
	KeepCount int    `json:"keepCount"` // 0 = unlimited
}

type shellTaskConfig struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds, 0 = 300
}

type yaegiTaskConfig struct {
	Script  string `json:"script"`
	Timeout int    `json:"timeout"` // seconds, 0 = 60
}

type commandTaskConfig struct {
	DeviceName string `json:"deviceName"`
	TagPath    string `json:"tagPath"`
	Value      any    `json:"value"`
	Timeout    int    `json:"timeout"` // seconds, 0 = 10
}

type commandResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ── Report ────────────────────────────────────────────────────────────────────

func runReport(ctx context.Context, rc RunContext) (string, error) {
	var cfg reportTaskConfig
	if err := json.Unmarshal(rc.Task.TaskConfig, &cfg); err != nil {
		return "", fmt.Errorf("parsing report config: %w", err)
	}

	tmpl, err := rc.DB.GetPDFTemplate(ctx, rc.Org, cfg.TemplateID)
	if err != nil {
		return "", fmt.Errorf("fetching template: %w", err)
	}
	if tmpl == nil {
		return "", fmt.Errorf("template %q not found", cfg.TemplateID)
	}

	vars, err := reporting.ParseVariables(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing variables: %w", err)
	}

	resolved := reporting.ResolveVariables(ctx, vars, reporting.ResolveContext{
		OrgName:        rc.Org,
		OrgDisplayName: reportOrgDisplayName(ctx, rc.DB, rc.Org),
		ReportName:     tmpl.Name,
	})
	// Override with any caller-supplied variable values.
	for k, v := range cfg.Variables {
		resolved[k] = v
	}

	substituted, err := reporting.SubstituteTemplate(tmpl.TemplateJSON, resolved)
	if err != nil {
		return "", fmt.Errorf("substituting variables: %w", err)
	}

	pdfBytes, err := reporting.GeneratePDF(ctx, substituted, reporting.GenerateContext{
		OrgName: rc.Org,
		TagPathsQueryer: func(ctx context.Context, orgName string, tagPaths []string, start, end time.Time) ([]sqldb.MetricSeries, error) {
			return rc.DB.QueryMetricsByTagPaths(ctx, orgName, tagPaths, start, end)
		},
		EventsQueryer: func(ctx context.Context, filter sqldb.EventFilter) ([]eventspkg.EventEntry, error) {
			return rc.DB.QueryEvents(ctx, filter)
		},
	})
	if err != nil {
		return "", fmt.Errorf("generating PDF: %w", err)
	}

	outDir := cfg.OutputDir
	outDir, err = resolveSchedulerOutputDir(outDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}

	stamp := rc.FiredAt.UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.pdf", sanitiseFilename(tmpl.Name), stamp)
	outPath := filepath.Join(outDir, filename)
	if err := os.WriteFile(outPath, pdfBytes, 0o644); err != nil {
		return "", fmt.Errorf("writing PDF: %w", err)
	}
	return outPath, nil
}

func reportOrgDisplayName(ctx context.Context, db sqldb.DB, org string) string {
	if db == nil {
		return org
	}
	o, err := db.GetOrganisation(ctx, org)
	if err != nil || o == nil || o.DisplayName == "" {
		return org
	}
	return o.DisplayName
}

// ── Backup ────────────────────────────────────────────────────────────────────

func runBackup(ctx context.Context, rc RunContext) (string, error) {
	var cfg backupTaskConfig
	if err := json.Unmarshal(rc.Task.TaskConfig, &cfg); err != nil {
		return "", fmt.Errorf("parsing backup config: %w", err)
	}

	outDir := cfg.OutputDir
	outDir, err := resolveSchedulerOutputDir(outDir)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("resolving output dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("creating backup output dir: %w", err)
	}

	stamp := rc.FiredAt.UTC().Format("20060102-150405")
	outPath := filepath.Join(absDir, fmt.Sprintf("backup-%s.tar.gz", stamp))
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating backup file: %w", err)
	}
	defer f.Close()

	if err := backups.Backup(ctx, rc.DB.BackupAdapter(), f); err != nil {
		os.Remove(outPath)
		return "", fmt.Errorf("backup failed: %w", err)
	}

	if cfg.KeepCount > 0 {
		if err := pruneBackups(absDir, cfg.KeepCount); err != nil {
			// Non-fatal: log but don't fail the backup task.
			_ = err
		}
	}

	return outPath, nil
}

// pruneBackups deletes the oldest backup-*.tar.gz files in dir until at most
// keep files remain.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "backup-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	// os.ReadDir returns entries sorted by name; since filenames embed a
	// timestamp (backup-20060102-150405.tar.gz) alphabetical order == chronological.
	if len(files) <= keep {
		return nil
	}

	for _, f := range files[:len(files)-keep] {
		os.Remove(f)
	}
	return nil
}

// ── Shell ─────────────────────────────────────────────────────────────────────

func runShell(ctx context.Context, rc RunContext) error {
	var cfg shellTaskConfig
	if err := json.Unmarshal(rc.Task.TaskConfig, &cfg); err != nil {
		return fmt.Errorf("parsing shell config: %w", err)
	}
	if cfg.Command == "" {
		return fmt.Errorf("shell command is empty")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 300
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
	workDir, err := schedulerWorkDir()
	if err != nil {
		return err
	}
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w\noutput: %s", err, out.String())
	}
	return nil
}

// ── Yaegi ─────────────────────────────────────────────────────────────────────

func runYaegi(ctx context.Context, rc RunContext) error {
	var cfg yaegiTaskConfig
	if err := json.Unmarshal(rc.Task.TaskConfig, &cfg); err != nil {
		return fmt.Errorf("parsing yaegi config: %w", err)
	}
	if cfg.Script == "" {
		return fmt.Errorf("yaegi script is empty")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	_ = ctx

	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)

	if _, err := i.Eval(cfg.Script); err != nil {
		return fmt.Errorf("compiling script: %w", err)
	}

	v, err := i.Eval("script.Run")
	if err != nil {
		return fmt.Errorf("resolving script.Run: %w", err)
	}

	fn, ok := v.Interface().(func() error)
	if !ok {
		return fmt.Errorf("script.Run has wrong type %s, want func() error", reflect.TypeOf(v.Interface()))
	}

	return fn()
}

// ── Command ──────────────────────────────────────────────────────────────────

func runCommand(ctx context.Context, rc RunContext) error {
	var cfg commandTaskConfig
	if err := json.Unmarshal(rc.Task.TaskConfig, &cfg); err != nil {
		return fmt.Errorf("parsing command config: %w", err)
	}
	cfg.DeviceName = strings.TrimSpace(cfg.DeviceName)
	cfg.TagPath = strings.TrimSpace(cfg.TagPath)
	if cfg.DeviceName == "" {
		return fmt.Errorf("command deviceName is empty")
	}
	if cfg.TagPath == "" {
		return fmt.Errorf("command tagPath is empty")
	}
	if cfg.Value == nil {
		return fmt.Errorf("command value is required")
	}
	if s, ok := cfg.Value.(string); ok && strings.TrimSpace(s) == "" {
		return fmt.Errorf("command value is required")
	}
	if rc.NC == nil {
		return fmt.Errorf("NATS connection is unavailable")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	relativePath := commandRelativePath(rc.Org, cfg.DeviceName, cfg.TagPath)
	id := newCommandID()
	data, err := commandPayloadData(id, relativePath, cfg.Value)
	if err != nil {
		return fmt.Errorf("marshalling command payload: %w", err)
	}

	subject := xactnats.CommandSubjectPrefix + rc.Org + "." + cfg.DeviceName
	msg, err := rc.NC.RequestWithContext(ctx, subject, data)
	if err != nil {
		recordCommandEvent(ctx, rc, eventspkg.Error, cfg, subject, relativePath, "", fmt.Sprintf("Command failed: %v", err))
		return fmt.Errorf("command request failed: %w", err)
	}

	var resp commandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		recordCommandEvent(ctx, rc, eventspkg.Error, cfg, subject, relativePath, string(msg.Data), "Command failed: invalid response")
		return fmt.Errorf("parsing command response: %w", err)
	}
	if resp.ID != "" && resp.ID != id {
		message := fmt.Sprintf("Command failed: response id %q does not match request id %q", resp.ID, id)
		recordCommandEvent(ctx, rc, eventspkg.Error, cfg, subject, relativePath, resp.Message, message)
		return fmt.Errorf("command response id mismatch: got %q, want %q", resp.ID, id)
	}

	result := resp.Message
	if result == "" {
		if resp.Success {
			result = "The command succeeded"
		} else {
			result = "The command failed"
		}
	}
	severity := eventspkg.Info
	status := "succeeded"
	if !resp.Success {
		severity = eventspkg.Error
		status = "failed"
	}
	eventMessage := fmt.Sprintf("Scheduled command %q %s for %s.%s: %s", rc.Task.Name, status, cfg.DeviceName, relativePath, result)
	recordCommandEvent(ctx, rc, severity, cfg, subject, relativePath, result, eventMessage)
	if !resp.Success {
		return fmt.Errorf("command failed: %s", result)
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// sanitiseFilename replaces characters unsafe for filenames with underscores.
func sanitiseFilename(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c == '/' || c == '\\' || c == ':' || c == '*' || c == '?' || c == '"' || c == '<' || c == '>' || c == '|' {
			out[i] = '_'
		} else if c == ' ' {
			out[i] = '-'
		} else {
			out[i] = c
		}
	}
	return string(out)
}

func resolveSchedulerOutputDir(requested string) (string, error) {
	root := strings.TrimSpace(os.Getenv("SCHEDULER_OUTPUT_DIR"))
	if root == "" {
		root = "backups"
	}
	root = filepath.Clean(root)

	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "." {
		return root, nil
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("scheduler outputDir must be relative to SCHEDULER_OUTPUT_DIR")
	}
	clean := filepath.Clean(requested)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scheduler outputDir must stay under SCHEDULER_OUTPUT_DIR")
	}
	if clean == root {
		return root, nil
	}
	candidate := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving scheduler output root: %w", err)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving scheduler output dir: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scheduler outputDir must stay under SCHEDULER_OUTPUT_DIR")
	}
	return candidate, nil
}

func schedulerWorkDir() (string, error) {
	workDir := strings.TrimSpace(os.Getenv("SCHEDULER_WORK_DIR"))
	if workDir == "" {
		workDir = strings.TrimSpace(os.Getenv("SCHEDULER_OUTPUT_DIR"))
	}
	if workDir == "" {
		workDir = "backups"
	}
	workDir = filepath.Clean(workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("creating scheduler work dir: %w", err)
	}
	return workDir, nil
}

func commandRelativePath(org, deviceName, tagPath string) string {
	path := strings.Trim(strings.TrimSpace(tagPath), ". /")
	for _, prefix := range []string{
		strings.Trim(strings.TrimSpace(org), ".") + "." + strings.Trim(strings.TrimSpace(deviceName), ".") + ".",
		strings.Trim(strings.TrimSpace(deviceName), ".") + ".",
	} {
		if prefix != "." && strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func newCommandID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func commandPayloadData(id, relativePath string, value any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"id":         id,
		relativePath: value,
	})
}

func recordCommandEvent(ctx context.Context, rc RunContext, severity eventspkg.Severity, cfg commandTaskConfig, subject, relativePath, result, message string) {
	log.Printf("scheduler: %s", message)
	if rc.DB == nil {
		return
	}
	_ = rc.DB.InsertEventEntries(ctx, []eventspkg.EventEntry{{
		Timestamp: time.Now(),
		OrgName:   rc.Org,
		Severity:  string(severity),
		Device:    cfg.DeviceName,
		Message:   message,
		Params: map[string]any{
			"scheduleId":   rc.Task.ID,
			"schedule":     rc.Task.Name,
			"subject":      subject,
			"tagPath":      cfg.TagPath,
			"relativePath": relativePath,
			"value":        cfg.Value,
			"result":       result,
		},
	}})
}
