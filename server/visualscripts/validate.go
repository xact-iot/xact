package visualscripts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	result := ValidationResult{Graph: graph}
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
		if parameter.Required && (!exists || value == nil || (parameter.Type == "string" && strings.TrimSpace(fmt.Sprint(value)) == "")) {
			result.Diagnostics = append(result.Diagnostics, diagnostic("error", "missing_required_config", node.ID, "", "config."+parameter.Name, parameter.Label+" is required"))
			continue
		}
		if !exists || value == nil {
			continue
		}
		if parameter.Type == "number" {
			if _, ok := asFloat(value); !ok {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_number", node.ID, "", "config."+parameter.Name, parameter.Label+" must be a number"))
			}
		}
		if parameter.Type == "select" && len(parameter.Options) > 0 {
			selected := fmt.Sprint(value)
			valid := false
			for _, option := range parameter.Options {
				valid = valid || selected == option
			}
			if !valid {
				result.Diagnostics = append(result.Diagnostics, diagnostic("error", "invalid_option", node.ID, "", "config."+parameter.Name, parameter.Label+" has an unsupported value"))
			}
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
