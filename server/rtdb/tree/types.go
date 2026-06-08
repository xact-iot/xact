package tree

// ScalarType represents the type of a scalar value
type ScalarType int

const (
	TypeInteger ScalarType = iota
	TypeFloat
	TypeString
	TypeBoolean
	TypeEnum
)

func (t ScalarType) String() string {
	switch t {
	case TypeInteger:
		return "integer"
	case TypeFloat:
		return "float"
	case TypeString:
		return "string"
	case TypeBoolean:
		return "boolean"
	case TypeEnum:
		return "enum"
	default:
		return "unknown"
	}
}
