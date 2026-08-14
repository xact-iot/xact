package visualscripts

import "sort"

type Registry struct {
	definitions map[string]NodeDefinition
}

func NewRegistry() *Registry {
	r := &Registry{definitions: make(map[string]NodeDefinition)}
	for _, definition := range coreDefinitions() {
		r.definitions[definition.Type] = definition
	}
	return r
}

func (r *Registry) Definition(nodeType string) (NodeDefinition, bool) {
	d, ok := r.definitions[nodeType]
	return d, ok
}

func (r *Registry) Catalog() []NodeDefinition {
	items := make([]NodeDefinition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		// Catalog collection fields are arrays in the API contract. Go's JSON
		// encoder otherwise emits nil slices as null, which makes parameterless
		// nodes such as the Manual trigger awkward for clients to consume.
		if definition.Inputs == nil {
			definition.Inputs = []PortDefinition{}
		}
		if definition.Outputs == nil {
			definition.Outputs = []PortDefinition{}
		}
		if definition.Parameters == nil {
			definition.Parameters = []ParameterDefinition{}
		}
		items = append(items, definition)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category == items[j].Category {
			return items[i].Name < items[j].Name
		}
		return items[i].Category < items[j].Category
	})
	return items
}

func coreDefinitions() []NodeDefinition {
	in := []PortDefinition{{Name: "in", Label: "Input", DataType: "message"}}
	out := []PortDefinition{{Name: "out", Label: "Output", DataType: "message"}}
	field := ParameterDefinition{Name: "field", Label: "Field", Type: "string", Description: "Message field; leave blank to use value"}
	number := func(name, label string, value float64) ParameterDefinition {
		return ParameterDefinition{Name: name, Label: label, Type: "number", Required: true, Default: value}
	}
	return []NodeDefinition{
		{Type: "core.manual", TypeVersion: 1, Name: "Manual", Description: "Starts a controlled editor run", Category: "Triggers", Icon: "▶", Outputs: out, Available: true},
		{Type: "core.compare", TypeVersion: 1, Name: "Compare", Description: "Routes by an explicit comparison", Category: "Conditions", Icon: "≷", Inputs: in, Outputs: []PortDefinition{{Name: "true", Label: "True", DataType: "message"}, {Name: "false", Label: "False", DataType: "message"}}, Parameters: []ParameterDefinition{field, {Name: "operator", Label: "Operator", Type: "select", Required: true, Options: []string{"<", "<=", "==", "!=", ">=", ">"}, Default: "=="}, {Name: "compareTo", Label: "Compare to", Type: "json", Required: true}}, Available: true},
		{Type: "core.in-range", TypeVersion: 1, Name: "In Range", Description: "Routes when a number is within bounds", Category: "Conditions", Icon: "↔", Inputs: in, Outputs: []PortDefinition{{Name: "true", Label: "True", DataType: "message"}, {Name: "false", Label: "False", DataType: "message"}}, Parameters: []ParameterDefinition{field, number("minimum", "Minimum", 0), number("maximum", "Maximum", 100), {Name: "inclusive", Label: "Inclusive", Type: "boolean", Default: true}}, Available: true},
		{Type: "core.not", TypeVersion: 1, Name: "NOT", Description: "Routes by boolean negation", Category: "Conditions", Icon: "!", Inputs: in, Outputs: []PortDefinition{{Name: "true", Label: "True", DataType: "message"}, {Name: "false", Label: "False", DataType: "message"}}, Parameters: []ParameterDefinition{field}, Available: true},
		{Type: "core.and", TypeVersion: 1, Name: "AND", Description: "Routes when all selected fields are true", Category: "Conditions", Icon: "∧", Inputs: in, Outputs: []PortDefinition{{Name: "true", Label: "True", DataType: "message"}, {Name: "false", Label: "False", DataType: "message"}}, Parameters: []ParameterDefinition{{Name: "fields", Label: "Fields", Type: "json", Required: true, Description: "JSON array of message field names"}}, Available: true},
		{Type: "core.or", TypeVersion: 1, Name: "OR", Description: "Routes when any selected field is true", Category: "Conditions", Icon: "∨", Inputs: in, Outputs: []PortDefinition{{Name: "true", Label: "True", DataType: "message"}, {Name: "false", Label: "False", DataType: "message"}}, Parameters: []ParameterDefinition{{Name: "fields", Label: "Fields", Type: "json", Required: true, Description: "JSON array of message field names"}}, Available: true},
		{Type: "core.set-field", TypeVersion: 1, Name: "Set Field", Description: "Sets value or a named message field", Category: "Transforms", Icon: "✎", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{{Name: "field", Label: "Field", Type: "string", Description: "Leave blank to set message value"}, {Name: "value", Label: "Value", Type: "json", Required: true}}, Available: true},
		{Type: "core.select-field", TypeVersion: 1, Name: "Select Field", Description: "Copies a named field into message value", Category: "Transforms", Icon: "⌖", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{{Name: "field", Label: "Field", Type: "string", Required: true}}, Available: true},
		{Type: "core.multiply", TypeVersion: 1, Name: "Multiply", Description: "Multiplies a numeric value", Category: "Transforms", Icon: "×", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{field, number("factor", "Factor", 1)}, Available: true},
		{Type: "core.divide", TypeVersion: 1, Name: "Divide", Description: "Divides a numeric value", Category: "Transforms", Icon: "÷", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{field, number("divisor", "Divisor", 1)}, Available: true},
		{Type: "core.average", TypeVersion: 1, Name: "Average", Description: "Averages a value array or selected numeric fields", Category: "Transforms", Icon: "x̄", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{{Name: "fields", Label: "Fields", Type: "json", Description: "Optional JSON array of message field names"}}, Available: true},
		{Type: "core.clamp", TypeVersion: 1, Name: "Clamp", Description: "Clamps a numeric value", Category: "Transforms", Icon: "⊣", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{field, number("minimum", "Minimum", 0), number("maximum", "Maximum", 100)}, Available: true},
		{Type: "core.scale", TypeVersion: 1, Name: "Scale", Description: "Maps a value between numeric ranges", Category: "Transforms", Icon: "↗", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{field, number("inputMin", "Input minimum", 0), number("inputMax", "Input maximum", 100), number("outputMin", "Output minimum", 0), number("outputMax", "Output maximum", 1)}, Available: true},
		{Type: "core.get-context", TypeVersion: 1, Name: "Get Variable", Description: "Reads a temporary node or script variable", Category: "Variables", Icon: "⇥", Inputs: in, Outputs: out, Parameters: contextParameters(false), Available: true},
		{Type: "core.set-context", TypeVersion: 1, Name: "Set Variable", Description: "Writes a temporary node or script variable", Category: "Variables", Icon: "⇤", Inputs: in, Outputs: out, Parameters: contextParameters(true), Available: true},
		{Type: "core.delete-context", TypeVersion: 1, Name: "Delete Variable", Description: "Deletes a temporary variable", Category: "Variables", Icon: "⌫", Inputs: in, Outputs: out, Parameters: contextParameters(false), Available: true},
		{Type: "core.increment-context", TypeVersion: 1, Name: "Increment Variable", Description: "Adds an amount to a numeric variable", Category: "Variables", Icon: "+1", Inputs: in, Outputs: out, Parameters: append(contextParameters(false), number("amount", "Amount", 1)), Available: true},
		{Type: "core.debug", TypeVersion: 1, Name: "Debug", Description: "Adds the current message to the run trace", Category: "Actions", Icon: "◎", Inputs: in, Outputs: out, Parameters: []ParameterDefinition{{Name: "label", Label: "Label", Type: "string"}}, Available: true, OutputNode: true, SimulationSafe: true},
	}
}

func contextParameters(withValue bool) []ParameterDefinition {
	params := []ParameterDefinition{
		{Name: "scope", Label: "Scope", Type: "select", Required: true, Options: []string{"node", "script"}, Default: "script"},
		{Name: "key", Label: "Key", Type: "string", Required: true},
	}
	if withValue {
		params = append(params, ParameterDefinition{Name: "source", Label: "Source", Type: "select", Options: []string{"value", "configured"}, Default: "value"}, ParameterDefinition{Name: "value", Label: "Configured value", Type: "json"})
	}
	return params
}
