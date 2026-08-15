package visualscripts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaxNodes      = 250
	MaxEdges      = 500
	MaxGraphBytes = 2 << 20
)

func NormalizeGraph(graph GraphDocument) GraphDocument {
	if graph.SchemaVersion == 0 {
		graph.SchemaVersion = SchemaVersion
	}
	if graph.Settings.MaxConcurrency == 0 {
		graph.Settings.MaxConcurrency = 1
	}
	if graph.Settings.QueueLimit == 0 {
		graph.Settings.QueueLimit = 100
	}
	if graph.Settings.ErrorPolicy == "" {
		graph.Settings.ErrorPolicy = "stop-message"
	}
	if graph.Settings.TraceLevel == "" {
		graph.Settings.TraceLevel = "errors"
	}
	if graph.Nodes == nil {
		graph.Nodes = []GraphNode{}
	}
	if graph.Edges == nil {
		graph.Edges = []GraphEdge{}
	}
	if len(graph.Annotations) == 0 || string(graph.Annotations) == "null" {
		graph.Annotations = json.RawMessage(`[]`)
	}
	for i := range graph.Nodes {
		if graph.Nodes[i].TypeVersion == 0 {
			graph.Nodes[i].TypeVersion = 1
		}
		if len(graph.Nodes[i].Config) == 0 || string(graph.Nodes[i].Config) == "null" {
			graph.Nodes[i].Config = json.RawMessage(`{}`)
		}
		if graph.Nodes[i].Type == "core.tag-changed" || graph.Nodes[i].Type == "core.rising-edge" || graph.Nodes[i].Type == "core.falling-edge" {
			var config map[string]any
			if json.Unmarshal(graph.Nodes[i].Config, &config) == nil {
				if path, ok := config["pathPattern"].(string); ok {
					config["pathPattern"] = NormalizePathPattern(path)
					if encoded, err := json.Marshal(config); err == nil {
						graph.Nodes[i].Config = encoded
					}
				}
			}
		}
	}
	return graph
}

func GraphHash(graph GraphDocument) (string, []byte, error) {
	normalized := NormalizeGraph(graph)
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}

func ValidateGraph(registry *Registry, graph GraphDocument) ValidationResult {
	graph = NormalizeGraph(graph)
	// Diagnostics is an array in the API contract. Initializing it here keeps a
	// successful validation from being encoded as `"diagnostics": null`.
	result := ValidationResult{Graph: graph, Diagnostics: []Diagnostic{}}
	hash, encoded, err := GraphHash(graph)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_graph_json", "", "", "graph", err.Error()))
		return result
	}
	result.GraphHash = hash
	if len(encoded) > MaxGraphBytes {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "graph_too_large", "", "", "graph", fmt.Sprintf("Graph exceeds %d bytes", MaxGraphBytes)))
	}
	if graph.SchemaVersion != SchemaVersion {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "unsupported_schema", "", "", "schemaVersion", fmt.Sprintf("Schema version %d is not supported", graph.SchemaVersion)))
	}
	if len(graph.Nodes) > MaxNodes {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "too_many_nodes", "", "", "nodes", fmt.Sprintf("Graph exceeds the %d node limit", MaxNodes)))
	}
	if len(graph.Edges) > MaxEdges {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "too_many_edges", "", "", "edges", fmt.Sprintf("Graph exceeds the %d edge limit", MaxEdges)))
	}
	if graph.Settings.MaxConcurrency < 1 || graph.Settings.MaxConcurrency > 16 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_concurrency", "", "", "settings.maxConcurrency", "maxConcurrency must be between 1 and 16"))
	}
	if graph.Settings.QueueLimit < 1 || graph.Settings.QueueLimit > 10000 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_queue_limit", "", "", "settings.queueLimit", "queueLimit must be between 1 and 10000"))
	}
	if graph.Settings.ErrorPolicy != "stop-message" {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_error_policy", "", "", "settings.errorPolicy", "Only stop-message is supported in this release"))
	}
	if graph.Settings.TraceLevel != "errors" && graph.Settings.TraceLevel != "all" && graph.Settings.TraceLevel != "none" {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_trace_level", "", "", "settings.traceLevel", "traceLevel must be errors, all, or none"))
	}

	nodes := make(map[string]GraphNode, len(graph.Nodes))
	definitions := make(map[string]NodeDefinition, len(graph.Nodes))
	triggerIDs := make([]string, 0)
	for _, node := range graph.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_node_id", "", "", "id", "Node ID is required"))
			continue
		}
		if _, exists := nodes[node.ID]; exists {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "duplicate_node_id", node.ID, "", "id", "Node ID must be unique"))
			continue
		}
		nodes[node.ID] = node
		definition, ok := registry.Definition(node.Type)
		if !ok {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "unknown_node_type", node.ID, "", "type", fmt.Sprintf("Node type %q is not installed", node.Type)))
			continue
		}
		definitions[node.ID] = definition
		if node.TypeVersion != definition.TypeVersion {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "unsupported_node_version", node.ID, "", "typeVersion", fmt.Sprintf("%s version %d is not available", node.Type, node.TypeVersion)))
		}
		if !definition.Available {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "node_unavailable", node.ID, "", "type", definition.UnavailableWhy))
		}
		if definition.Category == "Triggers" {
			triggerIDs = append(triggerIDs, node.ID)
		}
		validateNodeConfig(&result, node, definition)
	}
	if len(triggerIDs) == 0 {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_trigger", "", "", "nodes", "At least one trigger is required"))
	}

	edgeIDs := make(map[string]struct{}, len(graph.Edges))
	adjacency := make(map[string][]string)
	indegree := make(map[string]int, len(nodes))
	for id := range nodes {
		indegree[id] = 0
	}
	for _, edge := range graph.Edges {
		if strings.TrimSpace(edge.ID) == "" {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_edge_id", "", "", "edges.id", "Edge ID is required"))
		} else if _, exists := edgeIDs[edge.ID]; exists {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "duplicate_edge_id", "", edge.ID, "id", "Edge ID must be unique"))
		} else {
			edgeIDs[edge.ID] = struct{}{}
		}
		from, fromOK := nodes[edge.From.NodeID]
		to, toOK := nodes[edge.To.NodeID]
		if !fromOK {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_source_node", "", edge.ID, "from.nodeId", "Source node does not exist"))
		}
		if !toOK {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_target_node", "", edge.ID, "to.nodeId", "Target node does not exist"))
		}
		if !fromOK || !toOK {
			continue
		}
		fromDef, fromDefOK := definitions[from.ID]
		toDef, toDefOK := definitions[to.ID]
		fromPort, outputOK := findPort(fromDef.Outputs, edge.From.Port)
		toPort, inputOK := findPort(toDef.Inputs, edge.To.Port)
		if !fromDefOK || !outputOK {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_output_port", from.ID, edge.ID, "from.port", fmt.Sprintf("Output port %q does not exist", edge.From.Port)))
		}
		if !toDefOK || !inputOK {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_input_port", to.ID, edge.ID, "to.port", fmt.Sprintf("Input port %q does not exist", edge.To.Port)))
		}
		if outputOK && inputOK && fromPort.DataType != toPort.DataType {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "incompatible_ports", to.ID, edge.ID, "to.port", "Ports carry incompatible data types"))
		}
		adjacency[from.ID] = append(adjacency[from.ID], to.ID)
		indegree[to.ID]++
	}

	if hasCycle(indegree, adjacency) {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "cycle_detected", "", "", "edges", "Directed cycles are not supported"))
	}
	reachable := make(map[string]bool)
	queue := append([]string(nil), triggerIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reachable[id] {
			continue
		}
		reachable[id] = true
		queue = append(queue, adjacency[id]...)
	}
	for id := range nodes {
		if !reachable[id] {
			result.Diagnostics = append(result.Diagnostics, diagnostic("warning", "unreachable_node", id, "", "", "Node is not reachable from a trigger"))
		}
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Severity == "error" && result.Diagnostics[j].Severity != "error"
	})
	result.Valid = true
	for _, item := range result.Diagnostics {
		if item.Severity == "error" {
			result.Valid = false
			break
		}
	}
	return result
}

func validateNodeConfig(result *ValidationResult, node GraphNode, definition NodeDefinition) {
	var config map[string]any
	if err := json.Unmarshal(node.Config, &config); err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_node_config", node.ID, "", "config", "Configuration must be a JSON object"))
		return
	}
	for _, parameter := range definition.Parameters {
		value, exists := config[parameter.Name]
		missing := !exists || (value == nil && parameter.Type != "json")
		if parameter.Required && (missing || (parameter.Type == "string" && strings.TrimSpace(fmt.Sprint(value)) == "")) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_required_config", node.ID, "", "config."+parameter.Name, parameter.Label+" is required"))
			continue
		}
		if !exists || value == nil {
			continue
		}
		if parameter.Type == "number" {
			if _, ok := finiteFloat(value); !ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_number", node.ID, "", "config."+parameter.Name, parameter.Label+" must be a finite number"))
			}
		}
		if parameter.Type == "string" {
			if _, ok := value.(string); !ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_string", node.ID, "", "config."+parameter.Name, parameter.Label+" must be text"))
			}
		}
		if parameter.Type == "boolean" {
			if _, ok := value.(bool); !ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_boolean", node.ID, "", "config."+parameter.Name, parameter.Label+" must be true or false"))
			}
		}
		if parameter.Type == "select" && len(parameter.Options) > 0 {
			selected, stringOK := value.(string)
			valid := false
			for _, option := range parameter.Options {
				valid = valid || selected == option
			}
			if !stringOK || !valid {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_option", node.ID, "", "config."+parameter.Name, parameter.Label+" has an unsupported value"))
			}
		}
	}
	if field, exists := config["field"]; exists {
		if path, ok := field.(string); ok && path != "" && !validFieldPath(path) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_field_path", node.ID, "", "config.field", "Field path cannot contain empty segments"))
		}
	}
	for _, key := range []string{"leftField", "rightField", "outputField"} {
		if field, exists := config[key]; exists {
			if path, ok := field.(string); ok && path != "" && !validFieldPath(path) {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_field_path", node.ID, "", "config."+key, "Field path cannot contain empty segments"))
			}
		}
	}
	if node.Type == "core.and" || node.Type == "core.or" {
		validateFieldList(result, node, config["fields"], true)
	}
	if node.Type == "core.average" {
		if fields, exists := config["fields"]; exists && fields != nil {
			validateFieldList(result, node, fields, false)
		}
	}
	if node.Type == "core.divide" {
		if divisor, ok := asFloat(config["divisor"]); ok && divisor == 0 {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "division_by_zero", node.ID, "", "config.divisor", "Divisor cannot be zero"))
		}
	}
	if node.Type == "core.in-range" || node.Type == "core.clamp" {
		min, minOK := asFloat(config["minimum"])
		max, maxOK := asFloat(config["maximum"])
		if minOK && maxOK && min > max {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_range", node.ID, "", "config.maximum", "Maximum must be greater than or equal to minimum"))
		}
	}
	if node.Type == "core.scale" {
		min, minOK := asFloat(config["inputMin"])
		max, maxOK := asFloat(config["inputMax"])
		if minOK && maxOK && min == max {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "zero_input_range", node.ID, "", "config.inputMax", "Input range cannot have zero width"))
		}
	}
	if node.Type == "core.timer" {
		validateDurationConfig(result, node, config, "interval", time.Second, true)
		validateDurationConfig(result, node, config, "initialDelay", 0, false)
		validateDurationConfig(result, node, config, "jitter", 0, false)
	}
	if node.Type == "core.startup" {
		validateDurationConfig(result, node, config, "delay", 0, false)
	}
	if node.Type == "core.tag-changed" || node.Type == "core.rising-edge" || node.Type == "core.falling-edge" {
		pattern := stringConfig(config, "pathPattern")
		if _, err := CompilePathPattern(pattern); err != nil {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_path_pattern", node.ID, "", "config.pathPattern", err.Error()))
		}
	}
	if node.Type == "core.rising-edge" || node.Type == "core.falling-edge" {
		validateDurationConfig(result, node, config, "debounce", 0, false)
	}
	if node.Type == "core.send-control" {
		if timeout, ok := finiteFloat(config["timeout"]); ok && timeout <= 0 {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_timeout", node.ID, "", "config.timeout", "Timeout must be greater than zero"))
		}
	}
	if node.Type == "core.compare-times" {
		switch stringConfig(config, "rightSource") {
		case "field":
			if strings.TrimSpace(stringConfig(config, "rightField")) == "" {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_time_field", node.ID, "", "config.rightField", "Right time field is required"))
			}
		case "configured":
			validateTimestampConfig(result, node, config, "rightTime")
		}
	}
	if node.Type == "core.set-time-context" {
		switch stringConfig(config, "source") {
		case "field":
			if strings.TrimSpace(stringConfig(config, "field")) == "" {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_time_field", node.ID, "", "config.field", "Time field is required"))
			}
		case "configured":
			validateTimestampConfig(result, node, config, "time")
		}
	}
	if node.Type == "core.debug" {
		if fields, exists := config["timeFields"]; exists && fields != nil {
			validateTimeFieldList(result, node, fields)
		}
	}
}

func validateTimestampConfig(result *ValidationResult, node GraphNode, config map[string]any, key string) {
	value, exists := config[key]
	if !exists || strings.TrimSpace(fmt.Sprint(value)) == "" {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_timestamp", node.ID, "", "config."+key, "Configured time is required"))
		return
	}
	if _, err := timestampMillis(value); err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_timestamp", node.ID, "", "config."+key, err.Error()))
	}
}

func validateTimeFieldList(result *ValidationResult, node GraphNode, value any) {
	fields, ok := value.([]any)
	if !ok {
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_time_field_list", node.ID, "", "config.timeFields", "Time fields must be a JSON array"))
		return
	}
	for _, value := range fields {
		field, ok := value.(string)
		if !ok || (field != "$value" && !validFieldPath(field)) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_time_field", node.ID, "", "config.timeFields", "Each time field must be $value or a dotted field path"))
			return
		}
	}
}

func validateDurationConfig(result *ValidationResult, node GraphNode, config map[string]any, key string, minimum time.Duration, required bool) {
	value, exists := config[key]
	if !exists || strings.TrimSpace(fmt.Sprint(value)) == "" {
		if required {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_duration", node.ID, "", "config."+key, key+" is required"))
		}
		return
	}
	duration, err := time.ParseDuration(fmt.Sprint(value))
	if err != nil || duration < minimum {
		message := key + " must be a valid non-negative duration such as 30s or 5m"
		if minimum > 0 {
			message = fmt.Sprintf("%s must be at least %s", key, minimum)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_duration", node.ID, "", "config."+key, message))
	}
}

func validateFieldList(result *ValidationResult, node GraphNode, value any, requireItems bool) {
	fields, ok := value.([]any)
	if !ok || (requireItems && len(fields) == 0) {
		message := "Fields must be a JSON array of field paths"
		if requireItems {
			message = "Fields must contain at least one field path"
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_field_list", node.ID, "", "config.fields", message))
		return
	}
	for _, value := range fields {
		field, ok := value.(string)
		if !ok || !validFieldPath(field) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_field_path", node.ID, "", "config.fields", "Every field must be a non-empty dotted path"))
			return
		}
	}
}

func validFieldPath(path string) bool {
	if strings.TrimSpace(path) != path || path == "" {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func findPort(ports []PortDefinition, name string) (PortDefinition, bool) {
	for _, port := range ports {
		if port.Name == name {
			return port, true
		}
	}
	return PortDefinition{}, false
}

func hasCycle(indegree map[string]int, adjacency map[string][]string) bool {
	copyDegree := make(map[string]int, len(indegree))
	queue := make([]string, 0)
	for id, degree := range indegree {
		copyDegree[id] = degree
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adjacency[id] {
			copyDegree[next]--
			if copyDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return visited != len(indegree)
}

func diagnostic(severity, code, nodeID, edgeID, field, message string) Diagnostic {
	return Diagnostic{Severity: severity, Code: code, NodeID: nodeID, EdgeID: edgeID, Field: field, Message: message}
}
