// Package mcp exposes a small embedded MCP-compatible JSON-RPC endpoint for
// XACT. It intentionally stays close to existing XACT services rather than
// introducing a second process or HTTP client back into the same server.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xact-iot/xact/rtdb/ingest"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/scheduler"
	"github.com/xact-iot/xact/sqldb"
	webapi "github.com/xact-iot/xact/web/api"
)

type Config struct {
	Enabled         bool
	Route           string
	WriteTools      bool
	ToolTimeout     time.Duration
	MaxPayloadBytes int64
	ServerName      string
	ServerVersion   string
	DocsRoot        string
	ExamplesRoot    string
}

type Dependencies struct {
	Tree   *tree.TreeWithOperations
	DB     sqldb.DB
	Ingest interface {
		WriteDeviceData(tenant, zone, deviceType, deviceName string, data ingest.TagData) error
	}
	ScheduleHandlers *webapi.ScheduleHandlers
	TagCalcHandlers  *webapi.TagCalcHandlers
	TreePublisher    interface {
		PublishChange(path string, node tree.TreeNode) error
	}
	RequireAny func(ctx context.Context, resource string, actions ...string) bool
	CurrentOrg func(ctx context.Context) (string, bool)
	APIContext func(ctx context.Context) (any, error)
	APIProxy   func(ctx context.Context, req APIProxyRequest) (APIProxyResponse, error)
}

type APIProxyRequest struct {
	OperationID string          `json:"operationId,omitempty"`
	Method      string          `json:"method,omitempty"`
	Path        string          `json:"path,omitempty"`
	PathParams  map[string]any  `json:"pathParams,omitempty"`
	Query       map[string]any  `json:"query,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
	Confirm     bool            `json:"confirm,omitempty"`
}

type APIProxyResponse struct {
	Status  int            `json:"status"`
	Headers map[string]any `json:"headers,omitempty"`
	Body    any            `json:"body,omitempty"`
}

type Server struct {
	cfg  Config
	deps Dependencies
}

func New(cfg Config, deps Dependencies) *Server {
	if cfg.Route == "" {
		cfg.Route = "/api/v1/mcp"
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = 30 * time.Second
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = 1 << 20
	}
	if cfg.ServerName == "" {
		cfg.ServerName = "xact"
	}
	return &Server{cfg: cfg, deps: deps}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":         s.cfg.ServerName,
			"version":      s.cfg.ServerVersion,
			"route":        s.cfg.Route,
			"protocol":     "2024-11-05",
			"capabilities": s.capabilities(),
		})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxPayloadBytes)
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "parse error", err.Error()))
		return
	}
	if len(raw) > 0 && raw[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(raw, &batch); err != nil {
			writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "parse error", err.Error()))
			return
		}
		responses := make([]any, 0, len(batch))
		for _, item := range batch {
			if resp, ok := s.handleRPC(r.Context(), item); ok {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}
	resp, ok := s.handleRPC(r.Context(), raw)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) handleRPC(ctx context.Context, raw json.RawMessage) (any, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcError(nil, -32600, "invalid request", err.Error()), true
	}
	if len(req.ID) == 0 && strings.HasPrefix(req.Method, "notifications/") {
		return nil, false
	}
	id := json.RawMessage(nil)
	if len(req.ID) > 0 {
		id = req.ID
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ToolTimeout)
	defer cancel()

	var (
		result any
		err    error
	)
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    s.capabilities(),
			"serverInfo": map[string]string{
				"name":    s.cfg.ServerName,
				"version": s.cfg.ServerVersion,
			},
		}
	case "tools/list":
		result = map[string]any{"tools": s.tools()}
	case "tools/call":
		result, err = s.callTool(ctx, req.Params)
	case "resources/list":
		result = map[string]any{"resources": s.resources()}
	case "resources/read":
		result, err = s.readResource(ctx, req.Params)
	case "prompts/list":
		result = map[string]any{"prompts": []any{}}
	default:
		return rpcError(id, -32601, "method not found", req.Method), true
	}
	if err != nil {
		return rpcError(id, -32000, "tool error", err.Error()), true
	}
	return map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result}, true
}

func (s *Server) capabilities() map[string]any {
	return map[string]any{
		"tools":     map[string]any{"listChanged": false},
		"resources": map[string]any{"subscribe": false, "listChanged": false},
		"prompts":   map[string]any{"listChanged": false},
	}
}

type toolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, error) {
	var call toolCall
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, fmt.Errorf("invalid tool call params: %w", err)
	}
	var result any
	var err error
	switch call.Name {
	case "xact_get_api_context":
		result, err = s.toolGetAPIContext(ctx)
	case "xact_api_proxy":
		result, err = s.toolAPIProxy(ctx, call.Arguments)
	case "xact_get_rtdb_item":
		result, err = s.toolGetRTDBItem(ctx, call.Arguments)
	case "xact_create_node":
		result, err = s.toolCreateNode(ctx, call.Arguments)
	case "xact_update_node":
		result, err = s.toolUpdateNode(ctx, call.Arguments)
	case "xact_delete_node":
		result, err = s.toolDeleteNode(ctx, call.Arguments)
	case "xact_create_tag":
		result, err = s.toolCreateTag(ctx, call.Arguments)
	case "xact_update_tag":
		result, err = s.toolUpdateTag(ctx, call.Arguments)
	case "xact_delete_tag":
		result, err = s.toolDeleteTag(ctx, call.Arguments)
	case "xact_delete_rtdb_item":
		result, err = s.toolDeleteRTDBItem(ctx, call.Arguments)
	case "xact_get_tag":
		result, err = s.toolGetTag(ctx, call.Arguments)
	case "xact_browse_tree":
		result, err = s.toolBrowseTree(ctx, call.Arguments)
	case "xact_find_tags":
		result, err = s.toolFindTags(ctx, call.Arguments)
	case "xact_query_history":
		result, err = s.toolQueryHistory(ctx, call.Arguments)
	case "xact_get_block_schemas":
		result, err = s.toolGetBlockSchemas(ctx)
	case "xact_generate_ingest_driver_context":
		result, err = s.toolGenerateDriverContext(ctx, call.Arguments)
	case "xact_get_driver_examples":
		result, err = s.toolGetDriverExamples(ctx, call.Arguments)
	case "xact_validate_provisioning_plan":
		result, err = s.toolValidateProvisioningPlan(ctx, call.Arguments)
	case "xact_provision_device":
		result, err = s.toolProvisionDevice(ctx, call.Arguments)
	case "xact_provision_scheduler":
		result, err = s.toolProvisionScheduler(ctx, call.Arguments)
	case "xact_define_report":
		result, err = s.toolDefineReport(ctx, call.Arguments)
	case "xact_define_tag_calc":
		result, err = s.toolDefineTagCalc(ctx, call.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool %q", call.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolResult(result)
}

func toolResult(v any) (map[string]any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(data)}},
	}, nil
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		tool("xact_get_api_context", "Return the generated OpenAPI document and current API context. Use this to discover available REST-backed operations.", object(map[string]any{})),
		tool("xact_api_proxy", "Execute a local XACT REST API operation as the authenticated MCP caller. Prefer operationId plus pathParams/query/body. Mutating calls require confirm=true.", object(map[string]any{"operationId": stringSchema("OpenAPI operationId."), "method": stringSchema("HTTP method when operationId is not used."), "path": stringSchema("Canonical API path when operationId is not used."), "pathParams": mapSchema("Path parameter values."), "query": mapSchema("Query parameters."), "body": mapSchema("JSON request body."), "confirm": boolSchema("Required for POST, PUT, PATCH, and DELETE calls.")})),
		tool("xact_get_rtdb_item", "Fetch an RTDB node or tag by path.", object(map[string]any{"path": stringSchema("RTDB path, org-relative or absolute."), "depth": numberSchema("Node depth when the item is a node; -1 returns the full subtree.")}, "path")),
		tool("xact_create_node", "Create an RTDB node in the current organisation.", object(map[string]any{"path": stringSchema("Node path, org-relative or absolute."), "description": stringSchema("Node description."), "templateName": stringSchema("Template name."), "nodeType": stringSchema("Standard, Device, or Organisation."), "isArray": boolSchema("Mark as an array container."), "locked": boolSchema("Lock the node after creation."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_update_node", "Update RTDB node metadata, lock state, or name.", object(map[string]any{"path": stringSchema("Node path, org-relative or absolute."), "name": stringSchema("New node name within the same parent."), "description": stringSchema("Node description."), "templateName": stringSchema("Template name."), "nodeType": stringSchema("Standard, Device, or Organisation."), "isArray": boolSchema("Mark/unmark as an array container."), "locked": boolSchema("Set lock state."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_delete_node", "Delete an RTDB node and its descendants.", object(map[string]any{"path": stringSchema("Node path, org-relative or absolute."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_create_tag", "Create an RTDB tag in the current organisation.", object(map[string]any{"path": stringSchema("Tag path, org-relative or absolute."), "type": stringSchema("integer, float, string, boolean, enum, or numeric scalar type."), "config": mapSchema("Tag config; supports name and templateName."), "shared": mapSchema("Tag shared metadata: description, units, deadband, enumValues, pipeline."), "metadata": mapSchema("Alias for shared metadata."), "value": mapSchema("Initial value."), "dryRun": boolSchema("Preview mutation only when true.")}, "path", "type")),
		tool("xact_update_tag", "Update RTDB tag metadata, pipeline, value, or name.", object(map[string]any{"path": stringSchema("Tag path, org-relative or absolute."), "name": stringSchema("New tag name within the same parent."), "shared": mapSchema("Tag shared metadata: description, units, deadband, enumValues, pipeline."), "metadata": mapSchema("Alias for shared metadata."), "description": stringSchema("Tag description."), "units": stringSchema("Engineering units."), "deadband": numberSchema("Deadband."), "enumValues": mapSchema("Enum value labels."), "pipeline": arraySchema("Processing block envelopes."), "value": mapSchema("New tag value."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_delete_tag", "Delete an RTDB tag.", object(map[string]any{"path": stringSchema("Tag path, org-relative or absolute."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_delete_rtdb_item", "Delete an RTDB node or tag by path.", object(map[string]any{"path": stringSchema("RTDB path, org-relative or absolute."), "dryRun": boolSchema("Preview mutation only when true.")}, "path")),
		tool("xact_get_tag", "Fetch current value and metadata for an RTDB tag.", object(map[string]any{"path": stringSchema("Tag path, org-relative or absolute.")}, "path")),
		tool("xact_browse_tree", "Browse RTDB nodes and tags.", object(map[string]any{"path": stringSchema("Tree path, org-relative or absolute."), "depth": numberSchema("Depth; -1 returns the full subtree.")})),
		tool("xact_find_tags", "Search the RTDB tree for matching tags.", object(map[string]any{"query": stringSchema("Search phrase."), "types": arraySchema("Allowed value types."), "historicalOnly": boolSchema("Only return tags with history recorder blocks.")})),
		tool("xact_query_history", "Fetch historical metric series.", object(map[string]any{"devicePath": stringSchema("Device path relative to the organisation."), "metrics": arraySchema("Metric names."), "range": stringSchema("Relative range such as 24h."), "start": stringSchema("RFC3339 start time."), "end": stringSchema("RFC3339 end time.")}, "devicePath", "metrics")),
		tool("xact_get_block_schemas", "Return processing block schemas.", object(map[string]any{})),
		tool("xact_generate_ingest_driver_context", "Return driver-writing context for REST, MQTT, or NATS ingest.", object(map[string]any{"language": stringSchema("Driver language."), "tenant": stringSchema("Organisation."), "zone": stringSchema("Optional zone."), "deviceType": stringSchema("Device type."), "deviceName": stringSchema("Device name."), "method": stringSchema("rest, mqtt, or nats."), "tags": mapSchema("Tag model.")})),
		tool("xact_get_driver_examples", "Return repo driver examples.", object(map[string]any{"method": stringSchema("rest, mqtt, or nats."), "language": stringSchema("Language.")})),
		tool("xact_validate_provisioning_plan", "Validate a device provisioning plan without writing.", object(map[string]any{"tenant": stringSchema("Organisation."), "zone": stringSchema("Optional zone."), "deviceType": stringSchema("Device type."), "deviceName": stringSchema("Device name."), "description": stringSchema("Device description."), "tags": arraySchema("Tag definitions.")}, "tenant", "deviceType", "deviceName")),
		tool("xact_provision_device", "Provision a device through the ingest processor. Defaults to dryRun.", object(map[string]any{"tenant": stringSchema("Organisation."), "zone": stringSchema("Optional zone."), "deviceType": stringSchema("Device type."), "deviceName": stringSchema("Device name."), "description": stringSchema("Device description."), "tags": arraySchema("Tag definitions."), "dryRun": boolSchema("Preview only when true.")}, "tenant", "deviceType", "deviceName")),
		tool("xact_provision_scheduler", "CRUD and run operations for scheduler entries.", object(map[string]any{"operation": stringSchema("list, get, create, update, delete, run, history."), "id": stringSchema("Schedule ID."), "name": stringSchema("Task name."), "description": stringSchema("Task description."), "taskType": stringSchema("report, backup, shell, yaegi, command."), "taskConfig": mapSchema("Task config."), "schedule": stringSchema("5-field cron expression."), "enabled": boolSchema("Enabled flag."), "dryRun": boolSchema("Preview mutation only when true.")}, "operation")),
		tool("xact_define_report", "CRUD operations for PDF report templates.", object(map[string]any{"operation": stringSchema("list, get, create, update, delete."), "id": stringSchema("Template ID."), "name": stringSchema("Template name."), "description": stringSchema("Template description."), "templateJson": mapSchema("Report template JSON."), "variables": arraySchema("Report variables."), "dryRun": boolSchema("Preview mutation only when true.")}, "operation")),
		tool("xact_define_tag_calc", "CRUD and test operations for tag calculations.", object(map[string]any{"operation": stringSchema("list, get, create, update, test, disable, delete."), "id": numberSchema("Tag calc ID."), "name": stringSchema("Calculation name."), "description": stringSchema("Description."), "outputTag": stringSchema("Org-relative output tag."), "expression": stringSchema("Expression."), "intervalSeconds": numberSchema("Evaluation interval."), "enabled": boolSchema("Enabled flag."), "dryRun": boolSchema("Preview mutation only when true.")}, "operation")),
	}
}

func tool(name, desc string, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "description": desc, "inputSchema": schema}
}

func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func stringSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func numberSchema(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func boolSchema(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func arraySchema(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{}}
}
func mapSchema(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc, "additionalProperties": true}
}

func (s *Server) org(ctx context.Context) (string, error) {
	if s.deps.CurrentOrg != nil {
		if org, ok := s.deps.CurrentOrg(ctx); ok && org != "" {
			return org, nil
		}
	}
	return "", errors.New("missing organisation context")
}

func (s *Server) require(ctx context.Context, resource string, actions ...string) error {
	if s.deps.RequireAny == nil || !s.deps.RequireAny(ctx, resource, actions...) {
		return fmt.Errorf("forbidden: requires %s.%s", resource, strings.Join(actions, "|"))
	}
	return nil
}

func (s *Server) requireWrite(ctx context.Context, resource string, actions ...string) error {
	if !s.cfg.WriteTools {
		return errors.New("MCP write tools are disabled")
	}
	return s.require(ctx, resource, actions...)
}

func (s *Server) fullPath(ctx context.Context, path string) (string, string, error) {
	org, err := s.org(ctx)
	if err != nil {
		return "", "", err
	}
	p := strings.Trim(strings.ReplaceAll(path, "/", "."), ".")
	if p == "" {
		return org, org, nil
	}
	if p == org || strings.HasPrefix(p, org+".") {
		return p, org, nil
	}
	return org + "." + p, org, nil
}

func (s *Server) publishTreeChange(path string, node tree.TreeNode) {
	if s.deps.TreePublisher == nil {
		return
	}
	_ = s.deps.TreePublisher.PublishChange(path, node)
}

func (s *Server) toolGetAPIContext(ctx context.Context) (any, error) {
	if s.deps.APIContext == nil {
		return nil, errors.New("API context is not configured")
	}
	return s.deps.APIContext(ctx)
}

func (s *Server) toolAPIProxy(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.deps.APIProxy == nil {
		return nil, errors.New("API proxy is not configured")
	}
	var req APIProxyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" && req.OperationID == "" {
		return nil, errors.New("operationId or method/path is required")
	}
	if method != "" && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && !req.Confirm {
		return nil, errors.New("confirm=true is required for mutating API proxy calls")
	}
	if req.OperationID != "" && !req.Confirm {
		// The API server repeats this check after resolving the operationId; this
		// early guard keeps destructive intent explicit in MCP transcripts.
		req.Confirm = false
	}
	return s.deps.APIProxy(ctx, req)
}

func (s *Server) proxyAPICall(ctx context.Context, operationID string, pathParams map[string]any, body any, confirm bool) (any, error) {
	if s.deps.APIProxy == nil {
		return nil, errors.New("API proxy is not configured")
	}
	var rawBody json.RawMessage
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rawBody = data
	}
	resp, err := s.deps.APIProxy(ctx, APIProxyRequest{
		OperationID: operationID,
		PathParams:  pathParams,
		Body:        rawBody,
		Confirm:     confirm,
	})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("API %s failed with HTTP %d: %v", operationID, resp.Status, resp.Body)
	}
	return map[string]any{"status": resp.Status, "body": resp.Body}, nil
}

func tagSharedAPIMap(shared tree.TagShared) map[string]any {
	out := map[string]any{}
	if shared.Description != "" {
		out["description"] = shared.Description
	}
	if shared.Units != "" {
		out["units"] = shared.Units
	}
	if shared.Deadband != 0 {
		out["deadband"] = shared.Deadband
	}
	if len(shared.EnumValues) > 0 {
		out["enumValues"] = shared.EnumValues
	}
	return out
}

type rtdbNodeMutationRequest struct {
	Path         string  `json:"path"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	TemplateName *string `json:"templateName"`
	NodeType     string  `json:"nodeType"`
	IsDevice     bool    `json:"isDevice"`
	IsArray      *bool   `json:"isArray"`
	Locked       *bool   `json:"locked"`
	DryRun       *bool   `json:"dryRun"`
}

type rtdbTagSharedInput struct {
	Description *string                      `json:"description"`
	Units       *string                      `json:"units"`
	Deadband    *float64                     `json:"deadband"`
	EnumValues  map[int]string               `json:"enumValues"`
	Pipeline    *[]tree.ProcessBlockEnvelope `json:"pipeline"`
}

type rtdbTagMutationRequest struct {
	Path        string                       `json:"path"`
	Name        string                       `json:"name"`
	Type        json.RawMessage              `json:"type"`
	Config      json.RawMessage              `json:"config"`
	Shared      rtdbTagSharedInput           `json:"shared"`
	Metadata    rtdbTagSharedInput           `json:"metadata"`
	Description *string                      `json:"description"`
	Units       *string                      `json:"units"`
	Deadband    *float64                     `json:"deadband"`
	EnumValues  map[int]string               `json:"enumValues"`
	Pipeline    *[]tree.ProcessBlockEnvelope `json:"pipeline"`
	Value       json.RawMessage              `json:"value"`
	DryRun      *bool                        `json:"dryRun"`
}

func isDryRun(v *bool) bool {
	return v != nil && *v
}

func normalizeNodeType(raw string, legacyDevice bool) tree.NodeType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "device":
		return tree.NodeTypeDevice
	case "organisation", "organization":
		return tree.NodeTypeOrganisation
	case "standard":
		return tree.NodeTypeStandard
	default:
		if legacyDevice {
			return tree.NodeTypeDevice
		}
		return tree.NodeTypeStandard
	}
}

func parentAndChildName(path string) (string, string, error) {
	components := tree.ResolvePath(path)
	if len(components) == 0 {
		return "", "", errors.New("path must include a child name")
	}
	if len(components) == 1 {
		return "/", components[0], nil
	}
	return "/" + strings.Join(components[:len(components)-1], "/"), components[len(components)-1], nil
}

func replacePathName(path, name string) string {
	components := tree.ResolvePath(path)
	if len(components) == 0 {
		return path
	}
	components[len(components)-1] = name
	return strings.Join(components, ".")
}

func parseScalarType(raw json.RawMessage) (tree.ScalarType, error) {
	if len(raw) == 0 {
		return 0, errors.New("type is required")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "integer", "int":
			return tree.TypeInteger, nil
		case "float", "double", "number":
			return tree.TypeFloat, nil
		case "string":
			return tree.TypeString, nil
		case "boolean", "bool":
			return tree.TypeBoolean, nil
		case "enum":
			return tree.TypeEnum, nil
		default:
			return 0, fmt.Errorf("unknown scalar type %q", text)
		}
	}
	var num int
	if err := json.Unmarshal(raw, &num); err == nil {
		t := tree.ScalarType(num)
		if t < tree.TypeInteger || t > tree.TypeEnum {
			return 0, fmt.Errorf("unknown scalar type %d", num)
		}
		return t, nil
	}
	return 0, errors.New("type must be a string or numeric scalar type")
}

func tagConfigFromRaw(raw json.RawMessage, scalarType tree.ScalarType) (tree.TagConfig, error) {
	cfg := tree.TagConfig{Type: scalarType}
	if len(raw) == 0 || string(raw) == "null" {
		return cfg, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}
	if rawName, ok := m["name"]; ok {
		_ = json.Unmarshal(rawName, &cfg.Name)
	}
	if rawTemplate, ok := m["templateName"]; ok {
		_ = json.Unmarshal(rawTemplate, &cfg.TemplateName)
	}
	if rawType, ok := m["type"]; ok {
		configType, err := parseScalarType(rawType)
		if err != nil {
			return cfg, fmt.Errorf("invalid config.type: %w", err)
		}
		if configType != scalarType {
			return cfg, fmt.Errorf("config.type %s does not match type %s", configType.String(), scalarType.String())
		}
		cfg.Type = configType
	}
	return cfg, nil
}

func (in rtdbTagSharedInput) applyTo(shared tree.TagShared) (tree.TagShared, bool, error) {
	changed := false
	if in.Description != nil {
		shared.Description = *in.Description
		changed = true
	}
	if in.Units != nil {
		shared.Units = *in.Units
		changed = true
	}
	if in.Deadband != nil {
		shared.Deadband = *in.Deadband
		changed = true
	}
	if in.EnumValues != nil {
		shared.EnumValues = in.EnumValues
		changed = true
	}
	if in.Pipeline != nil {
		if len(*in.Pipeline) == 0 {
			shared.Pipeline = nil
		} else {
			pipeline, err := tree.UnmarshalPipeline(*in.Pipeline)
			if err != nil {
				return shared, changed, fmt.Errorf("invalid pipeline: %w", err)
			}
			shared.Pipeline = pipeline
		}
		changed = true
	}
	return shared, changed, nil
}

func (req rtdbTagMutationRequest) sharedInput() rtdbTagSharedInput {
	in := req.Shared
	if req.Metadata.Description != nil {
		in.Description = req.Metadata.Description
	}
	if req.Metadata.Units != nil {
		in.Units = req.Metadata.Units
	}
	if req.Metadata.Deadband != nil {
		in.Deadband = req.Metadata.Deadband
	}
	if req.Metadata.EnumValues != nil {
		in.EnumValues = req.Metadata.EnumValues
	}
	if req.Metadata.Pipeline != nil {
		in.Pipeline = req.Metadata.Pipeline
	}
	if req.Description != nil {
		in.Description = req.Description
	}
	if req.Units != nil {
		in.Units = req.Units
	}
	if req.Deadband != nil {
		in.Deadband = req.Deadband
	}
	if req.EnumValues != nil {
		in.EnumValues = req.EnumValues
	}
	if req.Pipeline != nil {
		in.Pipeline = req.Pipeline
	}
	return in
}

func (s *Server) toolGetRTDBItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	item, err := s.deps.Tree.FindNodeOrLeaf(path)
	if err != nil {
		return nil, err
	}
	if item.IsNode() {
		if err := s.require(ctx, "nodes", "read"); err != nil {
			return nil, err
		}
		return nodeResponse(path, item.(*tree.Node), req.Depth), nil
	}
	if err := s.require(ctx, "tags", "read"); err != nil {
		return nil, err
	}
	return tagResponse(path, item.(tree.Leaf)), nil
}

func (s *Server) toolCreateNode(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "nodes", "write"); err != nil {
		return nil, err
	}
	var req rtdbNodeMutationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "create_node", "path": path, "nodeType": string(normalizeNodeType(req.NodeType, req.IsDevice))}, nil
	}
	if s.deps.APIProxy != nil {
		return s.proxyAPICall(ctx, "post_nodes", nil, map[string]any{
			"path":         path,
			"description":  stringPtrValue(req.Description),
			"templateName": stringPtrValue(req.TemplateName),
			"nodeType":     string(normalizeNodeType(req.NodeType, req.IsDevice)),
			"isArray":      req.IsArray != nil && *req.IsArray,
		}, true)
	}
	nodeType := normalizeNodeType(req.NodeType, req.IsDevice)
	switch nodeType {
	case tree.NodeTypeDevice:
		err = s.deps.Tree.CreateDeviceNode(path, stringPtrValue(req.TemplateName))
	case tree.NodeTypeOrganisation:
		err = s.deps.Tree.CreateOrganisationNode(path, stringPtrValue(req.TemplateName))
	default:
		err = s.deps.Tree.CreateNode(path, stringPtrValue(req.TemplateName))
	}
	if err != nil {
		return nil, err
	}
	node, err := s.deps.Tree.FindNode(path)
	if err != nil {
		return nil, err
	}
	if req.Description != nil {
		node.SetDescription(*req.Description)
	}
	if req.TemplateName != nil {
		node.SetTemplateName(*req.TemplateName)
	}
	if req.IsArray != nil {
		node.SetIsArray(*req.IsArray)
	}
	if req.Locked != nil {
		if *req.Locked {
			if err := s.deps.Tree.LockNode(path); err != nil {
				return nil, err
			}
		} else if err := s.deps.Tree.UnlockNode(path); err != nil {
			return nil, err
		}
	}
	s.deps.Tree.NotifyChange(path, node)
	s.publishTreeChange(path, node)
	return map[string]any{"status": "ok", "node": nodeResponse(path, node, 0)}, nil
}

func (s *Server) toolUpdateNode(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "nodes", "write"); err != nil {
		return nil, err
	}
	var req rtdbNodeMutationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "update_node", "path": path, "name": req.Name}, nil
	}
	if s.deps.APIProxy != nil {
		body := map[string]any{}
		if req.Name != "" {
			body["name"] = req.Name
		}
		if req.Description != nil {
			body["description"] = *req.Description
		}
		if req.TemplateName != nil {
			body["templateName"] = *req.TemplateName
		}
		if req.NodeType != "" || req.IsDevice {
			body["nodeType"] = string(normalizeNodeType(req.NodeType, req.IsDevice))
		}
		if req.IsArray != nil {
			body["isArray"] = *req.IsArray
		}
		if req.Locked != nil {
			body["locked"] = *req.Locked
		}
		return s.proxyAPICall(ctx, "put_nodes_by_path", map[string]any{"path": path}, body, true)
	}
	oldPath := ""
	if req.Name != "" {
		parent, oldName, err := parentAndChildName(path)
		if err != nil {
			return nil, err
		}
		if req.Name != oldName {
			oldPath = path
			if err := s.deps.Tree.RenameChild(parent, oldName, req.Name); err != nil {
				return nil, err
			}
			path = replacePathName(path, req.Name)
		}
	}
	node, err := s.deps.Tree.FindNode(path)
	if err != nil {
		return nil, err
	}
	if req.Description != nil {
		node.SetDescription(*req.Description)
	}
	if req.TemplateName != nil {
		node.SetTemplateName(*req.TemplateName)
	}
	if req.NodeType != "" || req.IsDevice {
		switch normalizeNodeType(req.NodeType, req.IsDevice) {
		case tree.NodeTypeDevice:
			if err := s.deps.Tree.CreateDeviceNode(path, node.GetTemplateName()); err != nil {
				return nil, err
			}
		case tree.NodeTypeOrganisation:
			if err := s.deps.Tree.CreateOrganisationNode(path, node.GetTemplateName()); err != nil {
				return nil, err
			}
		default:
			node.SetNodeType(tree.NodeTypeStandard)
		}
		node, err = s.deps.Tree.FindNode(path)
		if err != nil {
			return nil, err
		}
	}
	if req.IsArray != nil {
		node.SetIsArray(*req.IsArray)
	}
	if req.Locked != nil {
		if *req.Locked {
			if err := s.deps.Tree.LockNode(path); err != nil {
				return nil, err
			}
		} else if err := s.deps.Tree.UnlockNode(path); err != nil {
			return nil, err
		}
	}
	s.deps.Tree.NotifyChange(path, node)
	if oldPath != "" {
		s.publishTreeChange(oldPath, nil)
	}
	s.publishTreeChange(path, node)
	return map[string]any{"status": "ok", "node": nodeResponse(path, node, 0)}, nil
}

func (s *Server) toolDeleteNode(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "nodes", "write"); err != nil {
		return nil, err
	}
	var req struct {
		Path   string `json:"path"`
		DryRun *bool  `json:"dryRun"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "delete_node", "path": path}, nil
	}
	if s.deps.APIProxy != nil {
		return s.proxyAPICall(ctx, "delete_nodes_by_path", map[string]any{"path": path}, nil, true)
	}
	propagated := s.deps.Tree.PropagateTemplateNodeDelete(path)
	if err := s.deps.Tree.DeleteNode(path); err != nil {
		return nil, err
	}
	s.publishTreeChange(path, nil)
	return map[string]any{"status": "deleted", "path": path, "type": "node", "propagatedTo": propagated}, nil
}

func (s *Server) toolCreateTag(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "tags", "write"); err != nil {
		return nil, err
	}
	var req rtdbTagMutationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	scalarType, err := parseScalarType(req.Type)
	if err != nil {
		return nil, err
	}
	cfg, err := tagConfigFromRaw(req.Config, scalarType)
	if err != nil {
		return nil, err
	}
	shared, _, err := req.sharedInput().applyTo(tree.TagShared{})
	if err != nil {
		return nil, err
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "create_tag", "path": path, "valueType": scalarType.String(), "config": cfg, "shared": shared}, nil
	}
	if s.deps.APIProxy != nil {
		body := map[string]any{
			"path":   path,
			"type":   scalarType,
			"config": cfg,
			"shared": tagSharedAPIMap(shared),
		}
		if sharedInput := req.sharedInput(); sharedInput.Pipeline != nil {
			body["pipeline"] = *sharedInput.Pipeline
		}
		if len(req.Value) > 0 {
			var value any
			if err := json.Unmarshal(req.Value, &value); err != nil {
				return nil, fmt.Errorf("invalid value: %w", err)
			}
			body["value"] = value
		}
		return s.proxyAPICall(ctx, "post_tags", nil, body, true)
	}
	if err := s.deps.Tree.CreateTag(path, scalarType, cfg, shared); err != nil {
		return nil, err
	}
	propagated := s.deps.Tree.PropagateTemplateTag(path)
	leaf, err := s.deps.Tree.FindLeaf(path)
	if err != nil {
		return nil, err
	}
	if len(req.Value) > 0 {
		var value any
		if err := json.Unmarshal(req.Value, &value); err != nil {
			return nil, fmt.Errorf("invalid value: %w", err)
		}
		if err := s.deps.Tree.SetLeafValue(path, value); err != nil {
			return nil, err
		}
		leaf, _ = s.deps.Tree.FindLeaf(path)
	}
	s.publishTreeChange(path, leaf)
	return map[string]any{"status": "ok", "tag": tagResponse(path, leaf), "propagatedTo": propagated}, nil
}

func (s *Server) toolUpdateTag(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "tags", "write"); err != nil {
		return nil, err
	}
	var req rtdbTagMutationRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "update_tag", "path": path, "name": req.Name}, nil
	}
	if s.deps.APIProxy != nil {
		body := map[string]any{}
		if req.Name != "" {
			body["name"] = req.Name
		}
		sharedInput := req.sharedInput()
		shared, changed, err := sharedInput.applyTo(tree.TagShared{})
		if err != nil {
			return nil, err
		}
		if changed {
			body["shared"] = tagSharedAPIMap(shared)
		}
		if sharedInput.Pipeline != nil {
			body["pipeline"] = *sharedInput.Pipeline
		}
		if len(req.Value) > 0 {
			var value any
			if err := json.Unmarshal(req.Value, &value); err != nil {
				return nil, fmt.Errorf("invalid value: %w", err)
			}
			body["value"] = value
		}
		return s.proxyAPICall(ctx, "put_tags_by_path", map[string]any{"path": path}, body, true)
	}
	oldPath := ""
	if req.Name != "" {
		parent, oldName, err := parentAndChildName(path)
		if err != nil {
			return nil, err
		}
		if req.Name != oldName {
			oldPath = path
			if err := s.deps.Tree.RenameChild(parent, oldName, req.Name); err != nil {
				return nil, err
			}
			path = replacePathName(path, req.Name)
		}
	}
	leaf, err := s.deps.Tree.FindLeaf(path)
	if err != nil {
		return nil, err
	}
	sharedInput := req.sharedInput()
	shared, structuralChange, err := sharedInput.applyTo(leaf.GetShared())
	if err != nil {
		return nil, err
	}
	if structuralChange {
		leaf.SetShared(shared)
		if sharedInput.EnumValues != nil {
			if enumLeaf, ok := leaf.(interface{ SetEnumValues(map[int]string) }); ok {
				enumLeaf.SetEnumValues(sharedInput.EnumValues)
			}
		}
		if sharedInput.Pipeline != nil && len(*sharedInput.Pipeline) > 0 {
			tree.InitPipelineBlocks(leaf, shared.Pipeline)
		}
		s.deps.Tree.NotifyChange(path, leaf)
	}
	if len(req.Value) > 0 {
		var value any
		if err := json.Unmarshal(req.Value, &value); err != nil {
			return nil, fmt.Errorf("invalid value: %w", err)
		}
		if err := s.deps.Tree.SetLeafValue(path, value); err != nil {
			return nil, err
		}
		leaf, _ = s.deps.Tree.FindLeaf(path)
	}
	if oldPath != "" {
		s.publishTreeChange(oldPath, nil)
	}
	s.publishTreeChange(path, leaf)
	return map[string]any{"status": "ok", "tag": tagResponse(path, leaf)}, nil
}

func (s *Server) toolDeleteTag(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.requireWrite(ctx, "tags", "write"); err != nil {
		return nil, err
	}
	var req struct {
		Path   string `json:"path"`
		DryRun *bool  `json:"dryRun"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "delete_tag", "path": path}, nil
	}
	if s.deps.APIProxy != nil {
		return s.proxyAPICall(ctx, "delete_tags_by_path", map[string]any{"path": path}, nil, true)
	}
	propagated := s.deps.Tree.PropagateTemplateTagDelete(path)
	if err := s.deps.Tree.DeleteTag(path); err != nil {
		return nil, err
	}
	s.publishTreeChange(path, nil)
	return map[string]any{"status": "deleted", "path": path, "type": "tag", "propagatedTo": propagated}, nil
}

func (s *Server) toolDeleteRTDBItem(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		Path   string `json:"path"`
		DryRun *bool  `json:"dryRun"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, errors.New("path is required")
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	item, err := s.deps.Tree.FindNodeOrLeaf(path)
	if err != nil {
		return nil, err
	}
	if item.IsNode() {
		if err := s.requireWrite(ctx, "nodes", "write"); err != nil {
			return nil, err
		}
		if isDryRun(req.DryRun) {
			return map[string]any{"dryRun": true, "operation": "delete_node", "path": path}, nil
		}
		if s.deps.APIProxy != nil {
			return s.proxyAPICall(ctx, "delete_nodes_by_path", map[string]any{"path": path}, nil, true)
		}
		propagated := s.deps.Tree.PropagateTemplateNodeDelete(path)
		if err := s.deps.Tree.DeleteNode(path); err != nil {
			return nil, err
		}
		s.publishTreeChange(path, nil)
		return map[string]any{"status": "deleted", "path": path, "type": "node", "propagatedTo": propagated}, nil
	}
	if err := s.requireWrite(ctx, "tags", "write"); err != nil {
		return nil, err
	}
	if isDryRun(req.DryRun) {
		return map[string]any{"dryRun": true, "operation": "delete_tag", "path": path}, nil
	}
	if s.deps.APIProxy != nil {
		return s.proxyAPICall(ctx, "delete_tags_by_path", map[string]any{"path": path}, nil, true)
	}
	propagated := s.deps.Tree.PropagateTemplateTagDelete(path)
	if err := s.deps.Tree.DeleteTag(path); err != nil {
		return nil, err
	}
	s.publishTreeChange(path, nil)
	return map[string]any{"status": "deleted", "path": path, "type": "tag", "propagatedTo": propagated}, nil
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Server) toolGetTag(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.require(ctx, "tags", "read"); err != nil {
		return nil, err
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	leaf, err := s.deps.Tree.FindLeaf(path)
	if err != nil {
		return nil, err
	}
	return tagResponse(path, leaf), nil
}

func (s *Server) toolBrowseTree(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.require(ctx, "nodes", "read"); err != nil {
		return nil, err
	}
	var req struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	path, _, err := s.fullPath(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	node, err := s.deps.Tree.FindNode(path)
	if err != nil {
		return nil, err
	}
	return nodeResponse(path, node, req.Depth), nil
}

func (s *Server) toolFindTags(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := s.require(ctx, "tags", "read"); err != nil {
		return nil, err
	}
	var req struct {
		Query          string   `json:"query"`
		Types          []string `json:"types"`
		HistoricalOnly bool     `json:"historicalOnly"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	root, err := s.deps.Tree.FindNode(org)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, typ := range req.Types {
		allowed[strings.ToLower(typ)] = true
	}
	q := strings.ToLower(strings.TrimSpace(req.Query))
	matches := []map[string]any{}
	walkLeaves(root, org, func(path string, leaf tree.Leaf) {
		typ := leaf.ValueType().String()
		if len(allowed) > 0 && !allowed[strings.ToLower(typ)] {
			return
		}
		if req.HistoricalOnly && !hasBlock(leaf, "historyrecorder") {
			return
		}
		haystack := strings.ToLower(path + " " + leaf.GetName() + " " + leaf.GetDescription() + " " + leaf.GetShared().Units)
		if q != "" && !strings.Contains(haystack, q) {
			return
		}
		matches = append(matches, tagResponse(path, leaf))
	})
	return map[string]any{"matches": matches, "count": len(matches)}, nil
}

func (s *Server) toolQueryHistory(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.deps.DB == nil {
		return nil, errors.New("database is not configured")
	}
	var req struct {
		DevicePath string   `json:"devicePath"`
		Metrics    []string `json:"metrics"`
		Range      string   `json:"range"`
		Start      string   `json:"start"`
		End        string   `json:"end"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	start, end, err := resolveTimeRange(req.Range, req.Start, req.End)
	if err != nil {
		return nil, err
	}
	device := strings.Trim(strings.TrimPrefix(strings.ReplaceAll(req.DevicePath, "/", "."), org+"."), ".")
	series, err := s.deps.DB.QueryMetricsRange(ctx, org, device, req.Metrics, start, end)
	if err != nil {
		return nil, err
	}
	return map[string]any{"org": org, "devicePath": device, "metrics": req.Metrics, "start": start, "end": end, "series": series}, nil
}

func (s *Server) toolGetBlockSchemas(ctx context.Context) (any, error) {
	if err := s.require(ctx, "tags", "read"); err != nil {
		return nil, err
	}
	return tree.GetRegisteredSchemas(), nil
}

func (s *Server) toolGenerateDriverContext(ctx context.Context, raw json.RawMessage) (any, error) {
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	tenant := stringValue(req, "tenant")
	if tenant == "" {
		tenant, _ = s.org(ctx)
	}
	method := strings.ToLower(stringValue(req, "method"))
	if method == "" {
		method = "rest"
	}
	zone := stringValue(req, "zone")
	deviceType := stringValue(req, "deviceType")
	deviceName := stringValue(req, "deviceName")
	endpoint := fmt.Sprintf("/api/v1/ingest/%s/%s/%s", tenant, deviceType, deviceName)
	if zone != "" {
		endpoint = fmt.Sprintf("/api/v1/ingest/%s/zone/%s/%s/%s", tenant, zone, deviceType, deviceName)
	}
	return map[string]any{
		"method": method,
		"rest": map[string]any{
			"endpoint": endpoint,
			"auth":     "X-API-Key header",
		},
		"mqtt": map[string]any{
			"topic": "xact/ingest/{tenant}/{deviceType}/{deviceName}",
			"note":  "Validate the exact topic format against the MQTT worker before treating this as authoritative.",
		},
		"nats": map[string]any{
			"note": "Use request/reply admission into the shared ingest queue for high-throughput drivers.",
		},
		"provisionPayload": buildProvisionPayloadFromAny(req),
		"valueOnlyPayload": buildValueOnlyPayloadFromAny(req),
		"timestamp":        "Omit timestamps to let XACT use server receive time; use ts for Unix milliseconds when the device timestamp is authoritative.",
	}, nil
}

func (s *Server) toolGetDriverExamples(_ context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		Method   string `json:"method"`
		Language string `json:"language"`
	}
	_ = json.Unmarshal(raw, &req)
	candidates := []string{
		filepath.Join(s.cfg.ExamplesRoot, "python-example", "ingest_example.py"),
		filepath.Join(s.cfg.ExamplesRoot, "iss_driver", "nats.go"),
		filepath.Join("..", "demo", "python-example", "ingest_example.py"),
		filepath.Join("..", "demo", "iss_driver", "nats.go"),
	}
	out := []map[string]string{}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, map[string]string{"path": p, "content": string(data)})
	}
	return map[string]any{"method": req.Method, "language": req.Language, "examples": out}, nil
}

func (s *Server) toolValidateProvisioningPlan(ctx context.Context, raw json.RawMessage) (any, error) {
	var req provisionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Tenant == "" {
		req.Tenant, _ = s.org(ctx)
	}
	payload, warnings, errs := buildProvisionPayload(req)
	return map[string]any{"normalized": req, "payload": payload, "warnings": warnings, "errors": errs, "dryRun": true}, nil
}

func (s *Server) toolProvisionDevice(ctx context.Context, raw json.RawMessage) (any, error) {
	var req provisionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Tenant == "" {
		req.Tenant, _ = s.org(ctx)
	}
	payload, warnings, errs := buildProvisionPayload(req)
	if len(errs) > 0 {
		return map[string]any{"payload": payload, "warnings": warnings, "errors": errs, "dryRun": true}, nil
	}
	if req.DryRun == nil || *req.DryRun {
		return map[string]any{"payload": payload, "warnings": warnings, "dryRun": true}, nil
	}
	if err := s.requireWrite(ctx, "tags", "write"); err != nil {
		return nil, err
	}
	if s.deps.Ingest == nil {
		return nil, errors.New("ingest processor is not configured")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	tagData, err := ingest.ParsePayload(data)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Ingest.WriteDeviceData(req.Tenant, req.Zone, req.DeviceType, req.DeviceName, tagData); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "payload": payload, "warnings": warnings, "dryRun": false}, nil
}

func (s *Server) toolProvisionScheduler(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.deps.ScheduleHandlers == nil || s.deps.ScheduleHandlers.DB == nil {
		return nil, errors.New("scheduler is not configured")
	}
	var req schedulerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(req.Operation)
	if op == "" {
		op = "list"
	}
	switch op {
	case "list":
		if err := s.require(ctx, "scheduler", "view", "manage"); err != nil {
			return nil, err
		}
		tasks, err := s.deps.ScheduleHandlers.DB.ListScheduledTasks(ctx, org)
		return map[string]any{"tasks": tasks}, err
	case "get":
		if err := s.require(ctx, "scheduler", "view", "manage"); err != nil {
			return nil, err
		}
		task, err := s.deps.ScheduleHandlers.DB.GetScheduledTask(ctx, org, req.ID)
		return map[string]any{"task": task}, err
	case "history":
		if err := s.require(ctx, "scheduler", "view", "manage"); err != nil {
			return nil, err
		}
		h, err := s.deps.ScheduleHandlers.DB.ListScheduleRunLog(ctx, req.ID, 50)
		return map[string]any{"history": h}, err
	case "run":
		if err := s.requireWrite(ctx, "scheduler", "manage"); err != nil {
			return nil, err
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": "run", "id": req.ID}, nil
		}
		if s.deps.ScheduleHandlers.Engine == nil {
			return nil, errors.New("scheduler engine unavailable")
		}
		return map[string]any{"status": "started"}, s.deps.ScheduleHandlers.Engine.StartNow(ctx, org, req.ID)
	case "delete":
		if err := s.requireWrite(ctx, "scheduler", "manage"); err != nil {
			return nil, err
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": "delete", "id": req.ID}, nil
		}
		if err := s.deps.ScheduleHandlers.DB.DeleteScheduledTask(ctx, org, req.ID); err != nil {
			return nil, err
		}
		if s.deps.ScheduleHandlers.Engine != nil {
			s.deps.ScheduleHandlers.Engine.Remove(req.ID)
		}
		return map[string]any{"status": "deleted", "id": req.ID}, nil
	case "create", "update":
		if err := s.requireWrite(ctx, "scheduler", "manage"); err != nil {
			return nil, err
		}
		task, err := req.task()
		if err != nil {
			return nil, err
		}
		if _, err := cron.ParseStandard(task.Schedule); err != nil {
			return nil, fmt.Errorf("invalid cron schedule: %w", err)
		}
		if scheduler.IsUnsafeTaskType(task.TaskType) && (s.deps.ScheduleHandlers.Engine == nil || !s.deps.ScheduleHandlers.Engine.AllowUnsafeTasks()) {
			return nil, errors.New("task type is disabled by server configuration")
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": op, "task": task}, nil
		}
		if op == "create" {
			if err := s.deps.ScheduleHandlers.DB.CreateScheduledTask(ctx, org, &task); err != nil {
				return nil, err
			}
		} else {
			if err := s.deps.ScheduleHandlers.DB.UpdateScheduledTask(ctx, org, req.ID, &task); err != nil {
				return nil, err
			}
			task.ID = req.ID
		}
		if task.Enabled && s.deps.ScheduleHandlers.Engine != nil {
			_ = s.deps.ScheduleHandlers.Engine.Reload(ctx, org, task.ID)
		}
		return map[string]any{"status": "ok", "task": task}, nil
	default:
		return nil, fmt.Errorf("unknown scheduler operation %q", req.Operation)
	}
}

func (s *Server) toolDefineReport(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.deps.DB == nil {
		return nil, errors.New("database is not configured")
	}
	var req reportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(req.Operation)
	if op == "" {
		op = "list"
	}
	switch op {
	case "list":
		if err := s.require(ctx, "reports", "view", "manage"); err != nil {
			return nil, err
		}
		templates, err := s.deps.DB.ListPDFTemplates(ctx, org)
		return map[string]any{"templates": templates}, err
	case "get":
		if err := s.require(ctx, "reports", "view", "manage"); err != nil {
			return nil, err
		}
		t, err := s.deps.DB.GetPDFTemplate(ctx, org, req.ID)
		return map[string]any{"template": t}, err
	case "delete":
		if err := s.requireWrite(ctx, "reports", "manage"); err != nil {
			return nil, err
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": "delete", "id": req.ID}, nil
		}
		return map[string]any{"status": "deleted", "id": req.ID}, s.deps.DB.DeletePDFTemplate(ctx, org, req.ID)
	case "create", "update":
		if err := s.requireWrite(ctx, "reports", "manage"); err != nil {
			return nil, err
		}
		t := sqldb.PDFTemplate{ID: req.ID, Name: req.Name, Description: req.Description, TemplateJSON: req.TemplateJSON, Variables: req.Variables}
		if t.TemplateJSON == nil {
			t.TemplateJSON = json.RawMessage(`{"config":{},"elements":[]}`)
		}
		if t.Variables == nil {
			t.Variables = json.RawMessage(`[]`)
		}
		warnings := reportVariableWarnings(t.TemplateJSON, t.Variables)
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": op, "template": t, "warnings": warnings}, nil
		}
		if op == "create" {
			if err := s.deps.DB.CreatePDFTemplate(ctx, org, &t); err != nil {
				return nil, err
			}
		} else {
			if err := s.deps.DB.UpdatePDFTemplate(ctx, org, req.ID, &t); err != nil {
				return nil, err
			}
			t.ID = req.ID
		}
		return map[string]any{"status": "ok", "template": t, "warnings": warnings}, nil
	default:
		return nil, fmt.Errorf("unknown report operation %q", req.Operation)
	}
}

func (s *Server) toolDefineTagCalc(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.deps.TagCalcHandlers == nil || s.deps.TagCalcHandlers.DB == nil {
		return nil, errors.New("tag calc engine is not configured")
	}
	var req tagCalcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	org, err := s.org(ctx)
	if err != nil {
		return nil, err
	}
	op := strings.ToLower(req.Operation)
	if op == "" {
		op = "list"
	}
	switch op {
	case "list":
		if err := s.require(ctx, "tagcalcs", "view", "manage"); err != nil {
			return nil, err
		}
		calcs, err := s.deps.TagCalcHandlers.DB.ListTagCalcs(ctx, org)
		return map[string]any{"tagCalcs": calcs, "functions": tagCalcFunctions()}, err
	case "get":
		if err := s.require(ctx, "tagcalcs", "view", "manage"); err != nil {
			return nil, err
		}
		calc, err := s.deps.TagCalcHandlers.DB.GetTagCalc(ctx, org, req.ID)
		return map[string]any{"tagCalc": calc, "functions": tagCalcFunctions()}, err
	case "test":
		if err := s.requireWrite(ctx, "tagcalcs", "manage"); err != nil {
			return nil, err
		}
		if s.deps.TagCalcHandlers.Engine == nil {
			return nil, errors.New("tag calc engine unavailable")
		}
		result, err := s.deps.TagCalcHandlers.Engine.EvaluateAny(org, req.Expression)
		if err != nil {
			return map[string]any{"error": err.Error(), "functions": tagCalcFunctions()}, nil
		}
		return map[string]any{"result": result, "functions": tagCalcFunctions()}, nil
	case "delete":
		if err := s.requireWrite(ctx, "tagcalcs", "manage"); err != nil {
			return nil, err
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": "delete", "id": req.ID}, nil
		}
		if s.deps.TagCalcHandlers.Engine != nil {
			s.deps.TagCalcHandlers.Engine.Unschedule(req.ID)
		}
		return map[string]any{"status": "deleted", "id": req.ID}, s.deps.TagCalcHandlers.DB.DeleteTagCalc(ctx, org, req.ID)
	case "disable":
		if err := s.requireWrite(ctx, "tagcalcs", "manage"); err != nil {
			return nil, err
		}
		existing, err := s.deps.TagCalcHandlers.DB.GetTagCalc(ctx, org, req.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, errors.New("tag calc not found")
		}
		req.Name, req.Description, req.OutputTag, req.Expression, req.IntervalSeconds, req.Enabled = existing.Name, existing.Description, existing.OutputTag, existing.Expression, existing.IntervalSeconds, boolPtr(false)
		fallthrough
	case "create", "update":
		if err := s.requireWrite(ctx, "tagcalcs", "manage"); err != nil {
			return nil, err
		}
		calc := req.calc()
		if calc.IntervalSeconds <= 0 {
			calc.IntervalSeconds = 60
		}
		var testResult any
		if s.deps.TagCalcHandlers.Engine != nil && calc.Expression != "" {
			v, err := s.deps.TagCalcHandlers.Engine.EvaluateAny(org, calc.Expression)
			if err != nil {
				return nil, fmt.Errorf("expression error: %w", err)
			}
			testResult = v
		}
		if req.DryRun == nil || *req.DryRun {
			return map[string]any{"dryRun": true, "operation": op, "tagCalc": calc, "testResult": testResult, "functions": tagCalcFunctions()}, nil
		}
		if op == "create" {
			if err := s.deps.TagCalcHandlers.DB.CreateTagCalc(ctx, org, &calc); err != nil {
				return nil, err
			}
		} else {
			if err := s.deps.TagCalcHandlers.DB.UpdateTagCalc(ctx, org, req.ID, &calc); err != nil {
				return nil, err
			}
			calc.ID = req.ID
		}
		if s.deps.TagCalcHandlers.Engine != nil {
			s.deps.TagCalcHandlers.Engine.Reload(ctx, org, calc.ID)
		}
		return map[string]any{"status": "ok", "tagCalc": calc, "testResult": testResult, "functions": tagCalcFunctions()}, nil
	default:
		return nil, fmt.Errorf("unknown tag calc operation %q", req.Operation)
	}
}

type provisionRequest struct {
	Tenant      string          `json:"tenant"`
	Zone        string          `json:"zone"`
	DeviceType  string          `json:"deviceType"`
	DeviceName  string          `json:"deviceName"`
	Description string          `json:"description"`
	Tags        []provisionTag  `json:"tags"`
	DryRun      *bool           `json:"dryRun"`
	Raw         json.RawMessage `json:"-"`
}

type provisionTag struct {
	Group       string            `json:"group"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Units       string            `json:"units"`
	Value       any               `json:"value"`
	History     bool              `json:"history"`
	Persist     bool              `json:"persist"`
	Publish     bool              `json:"publish"`
	StaleCheck  any               `json:"stalecheck"`
	Deadband    any               `json:"deadband"`
	EnumValues  map[string]string `json:"enumValues"`
	Limits      map[string]any    `json:"limits"`
	Scaling     map[string]any    `json:"scaling"`
}

func buildProvisionPayload(req provisionRequest) (map[string]any, []string, []string) {
	warnings := []string{}
	errs := []string{}
	if req.Tenant == "" {
		errs = append(errs, "tenant is required")
	}
	if req.DeviceType == "" {
		errs = append(errs, "deviceType is required")
	}
	if req.DeviceName == "" {
		errs = append(errs, "deviceName is required")
	}
	payload := map[string]any{}
	if req.Description != "" {
		payload["description"] = req.Description
	}
	for _, tag := range req.Tags {
		if tag.Name == "" {
			errs = append(errs, "tag name is required")
			continue
		}
		obj := map[string]any{"value": defaultValue(tag)}
		if tag.Value != nil {
			obj["value"] = tag.Value
		}
		if tag.Type != "" {
			if strings.EqualFold(tag.Type, "enum") {
				obj["type"] = "enum"
			} else {
				obj["type"] = strings.ToLower(tag.Type)
			}
		}
		if tag.Description != "" {
			obj["description"] = tag.Description
		}
		if tag.Units != "" {
			obj["units"] = tag.Units
		}
		if tag.History {
			obj["history"] = true
			if strings.EqualFold(tag.Type, "boolean") || strings.EqualFold(tag.Type, "string") {
				warnings = append(warnings, fmt.Sprintf("%s history requested for non-numeric tag", tag.Name))
			}
		}
		if tag.Persist {
			obj["persist"] = true
		}
		if tag.Publish {
			obj["publish"] = true
		}
		if tag.StaleCheck != nil {
			obj["stalecheck"] = tag.StaleCheck
		}
		if tag.Deadband != nil {
			obj["deadband"] = tag.Deadband
		}
		if len(tag.EnumValues) > 0 {
			obj["type"] = "enum"
			obj["enumValues"] = tag.EnumValues
		}
		if len(tag.Limits) > 0 {
			obj["limits"] = tag.Limits
		}
		if len(tag.Scaling) > 0 {
			obj["scaling"] = tag.Scaling
		}
		if tag.Group == "" {
			payload[tag.Name] = obj
		} else {
			group, _ := payload[tag.Group].(map[string]any)
			if group == nil {
				group = map[string]any{}
				payload[tag.Group] = group
			}
			group[tag.Name] = obj
		}
	}
	return payload, warnings, errs
}

func defaultValue(tag provisionTag) any {
	switch strings.ToLower(tag.Type) {
	case "boolean", "bool":
		return false
	case "string":
		return ""
	case "integer", "int", "enum":
		return 0
	default:
		return 0.0
	}
}

type schedulerRequest struct {
	Operation   string          `json:"operation"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	TaskType    string          `json:"taskType"`
	TaskConfig  json.RawMessage `json:"taskConfig"`
	Schedule    string          `json:"schedule"`
	Enabled     *bool           `json:"enabled"`
	DryRun      *bool           `json:"dryRun"`
}

func (r schedulerRequest) task() (sqldb.ScheduledTask, error) {
	if r.Name == "" || r.TaskType == "" || r.Schedule == "" {
		return sqldb.ScheduledTask{}, errors.New("name, taskType, and schedule are required")
	}
	cfg := r.TaskConfig
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return sqldb.ScheduledTask{Name: r.Name, Description: r.Description, TaskType: r.TaskType, TaskConfig: cfg, Schedule: r.Schedule, Enabled: enabled}, nil
}

type reportRequest struct {
	Operation    string          `json:"operation"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	TemplateJSON json.RawMessage `json:"templateJson"`
	Variables    json.RawMessage `json:"variables"`
	DryRun       *bool           `json:"dryRun"`
}

type tagCalcRequest struct {
	Operation       string `json:"operation"`
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	OutputTag       string `json:"outputTag"`
	Expression      string `json:"expression"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Enabled         *bool  `json:"enabled"`
	DryRun          *bool  `json:"dryRun"`
}

func (r tagCalcRequest) calc() sqldb.TagCalc {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return sqldb.TagCalc{Name: r.Name, Description: r.Description, OutputTag: r.OutputTag, Expression: r.Expression, IntervalSeconds: r.IntervalSeconds, Enabled: enabled}
}

func boolPtr(v bool) *bool { return &v }

func nodeResponse(path string, node *tree.Node, depth int) map[string]any {
	return map[string]any{
		"path":         path,
		"name":         node.GetName(),
		"description":  node.GetDescription(),
		"templateName": node.GetTemplateName(),
		"type":         string(node.GetNodeType()),
		"locked":       node.IsLocked(),
		"isArray":      node.GetIsArray(),
		"children":     childResponses(node, path, depth),
	}
}

func childResponses(node *tree.Node, parentPath string, depth int) []map[string]any {
	children := node.GetChildren()
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		child := children[name]
		path := parentPath + "." + name
		if child.IsNode() {
			n := child.(*tree.Node)
			item := map[string]any{"name": name, "type": "node", "description": n.GetDescription(), "isArray": n.GetIsArray()}
			if depth != 0 {
				item["children"] = childResponses(n, path, depth-1)
			}
			out = append(out, item)
		} else if leaf, ok := child.(tree.Leaf); ok {
			item := tagResponse(path, leaf)
			item["type"] = "leaf"
			item["name"] = name
			out = append(out, item)
		}
	}
	return out
}

func tagResponse(path string, leaf tree.Leaf) map[string]any {
	shared := leaf.GetShared()
	pipeline, _ := tree.MarshalPipeline(leaf.GetPipeline())
	return map[string]any{
		"path":      path,
		"name":      leaf.GetName(),
		"value":     leaf.GetAnyValue(),
		"valueType": leaf.ValueType().String(),
		"status":    leaf.GetState(),
		"timestamp": leaf.GetUpdatedTime().UnixMilli(),
		"config":    leaf.GetConfig(),
		"shared": map[string]any{
			"description": leaf.GetDescription(),
			"units":       shared.Units,
			"deadband":    shared.Deadband,
			"enumValues":  shared.EnumValues,
			"pipeline":    pipeline,
		},
	}
}

func walkLeaves(node *tree.Node, path string, fn func(string, tree.Leaf)) {
	children := node.GetChildren()
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := children[name]
		childPath := path + "." + name
		if child.IsNode() {
			walkLeaves(child.(*tree.Node), childPath, fn)
		} else if leaf, ok := child.(tree.Leaf); ok {
			fn(childPath, leaf)
		}
	}
}

func hasBlock(leaf tree.Leaf, typ string) bool {
	for _, block := range leaf.GetPipeline() {
		if strings.EqualFold(block.GetType(), typ) {
			return true
		}
	}
	return false
}

func resolveTimeRange(rel, startRaw, endRaw string) (time.Time, time.Time, error) {
	end := time.Now()
	if endRaw != "" {
		t, err := time.Parse(time.RFC3339, endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = t
	}
	if startRaw != "" {
		t, err := time.Parse(time.RFC3339, startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return t, end, nil
	}
	if rel == "" {
		rel = "24h"
	}
	d, err := time.ParseDuration(rel)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return end.Add(-d), end, nil
}

func buildProvisionPayloadFromAny(req map[string]any) map[string]any {
	data, _ := json.Marshal(req)
	var pr provisionRequest
	_ = json.Unmarshal(data, &pr)
	payload, _, _ := buildProvisionPayload(pr)
	return payload
}

func buildValueOnlyPayloadFromAny(req map[string]any) map[string]any {
	payload := buildProvisionPayloadFromAny(req)
	return stripMetadata(payload)
}

func stripMetadata(v any) map[string]any {
	src, _ := v.(map[string]any)
	out := map[string]any{}
	for k, val := range src {
		if obj, ok := val.(map[string]any); ok {
			if scalar, has := obj["value"]; has {
				out[k] = scalar
			} else {
				out[k] = stripMetadata(obj)
			}
		} else {
			out[k] = val
		}
	}
	return out
}

func stringValue(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func tagCalcFunctions() []string {
	return []string{
		"avg(pattern)", "sum(pattern)", "min(pattern)", "max(pattern)", "count(pattern)", "countWhere(pattern, value)",
		"listHighest(pattern, count)", "listLowest(pattern, count)",
		"if(condition, trueValue, falseValue)", "min(a, b)", "max(a, b)",
		"abs(v)", "round(v, decimals)", "floor(v)", "ceil(v)", "sqrt(v)", "pow(base, exp)", "log(v)", "log10(v)", "sin(v)", "cos(v)", "tan(v)",
	}
}

func reportVariableWarnings(templateJSON, varsJSON json.RawMessage) []string {
	vars := map[string]bool{}
	var rawVars []map[string]any
	_ = json.Unmarshal(varsJSON, &rawVars)
	for _, v := range rawVars {
		if name, ok := v["name"].(string); ok {
			vars[name] = true
		}
	}
	used := map[string]bool{}
	text := string(templateJSON)
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			break
		}
		text = text[start+2:]
		end := strings.Index(text, "}}")
		if end < 0 {
			break
		}
		name := strings.TrimSpace(text[:end])
		if name != "" {
			used[name] = true
		}
		text = text[end+2:]
	}
	warnings := []string{}
	for name := range used {
		if !vars[name] {
			warnings = append(warnings, fmt.Sprintf("variable %q is used but not defined", name))
		}
	}
	sort.Strings(warnings)
	return warnings
}

func (s *Server) resources() []map[string]any {
	return []map[string]any{
		{"uri": "xact://docs/data-ingest", "name": "Data ingest documentation", "mimeType": "text/markdown"},
		{"uri": "xact://docs/tags", "name": "Tags documentation", "mimeType": "text/markdown"},
		{"uri": "xact://docs/historical-data", "name": "Historical data documentation", "mimeType": "text/markdown"},
		{"uri": "xact://blocks/schemas", "name": "Processing block schemas", "mimeType": "application/json"},
	}
}

func (s *Server) readResource(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.URI == "xact://blocks/schemas" {
		v, err := s.toolGetBlockSchemas(ctx)
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(v, "", "  ")
		return map[string]any{"contents": []map[string]string{{"uri": req.URI, "mimeType": "application/json", "text": string(data)}}}, nil
	}
	files := map[string]string{
		"xact://docs/data-ingest":     "13-data-ingest.md",
		"xact://docs/tags":            "06-tags.md",
		"xact://docs/historical-data": "08-historical-data.md",
	}
	name, ok := files[req.URI]
	if !ok {
		return nil, fmt.Errorf("unknown resource %q", req.URI)
	}
	candidates := []string{
		filepath.Join(s.cfg.DocsRoot, name),
		filepath.Join("..", "ui", "public", "manual", name),
		filepath.Join("ui", "public", "manual", name),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return map[string]any{"contents": []map[string]string{{"uri": req.URI, "mimeType": "text/markdown", "text": string(data)}}}, nil
		}
	}
	return nil, fmt.Errorf("resource file not found for %s", req.URI)
}

func rpcError(id json.RawMessage, code int, message, data string) map[string]any {
	errObj := map[string]any{"code": code, "message": message}
	if data != "" {
		errObj["data"] = data
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": errObj}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
