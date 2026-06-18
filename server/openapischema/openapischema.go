package openapischema

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

type Handler struct {
	Handler     http.HandlerFunc
	RequestBody map[string]any
	Responses   map[string]any
	Tags        []string
}

func WithSchema(handler http.HandlerFunc, request any, response any, tags ...string) Handler {
	h := Handler{
		Handler:   handler,
		Responses: ResponseSchema(http.StatusOK, response),
		Tags:      tags,
	}
	if request != nil {
		h.RequestBody = JSONRequestBody(request)
	}
	return h
}

func WithResponses(handler http.HandlerFunc, responses map[int]any, tags ...string) Handler {
	return Handler{
		Handler:   handler,
		Responses: ResponseSchemas(responses),
		Tags:      tags,
	}
}

func JSONRequestBody(v any) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": SchemaFor(v),
			},
		},
	}
}

func ResponseSchema(status int, v any) map[string]any {
	return ResponseSchemas(map[int]any{status: v})
}

func ResponseSchemas(responses map[int]any) map[string]any {
	out := map[string]any{}
	for status, v := range responses {
		resp := map[string]any{"description": http.StatusText(status)}
		if v != nil {
			resp["content"] = map[string]any{
				"application/json": map[string]any{
					"schema": SchemaFor(v),
				},
			}
		}
		out[fmt.Sprint(status)] = resp
	}
	return out
}

func SchemaFor(v any) map[string]any {
	if schema, ok := v.(map[string]any); ok {
		return schema
	}
	return schemaForType(reflect.TypeOf(v), map[reflect.Type]bool{})
}

func schemaForType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if seen[t] {
			return map[string]any{"type": "object"}
		}
		seen[t] = true
		props := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" && !field.Anonymous {
				continue
			}
			name, omitempty, skip := jsonFieldName(field)
			if skip {
				continue
			}
			props[name] = schemaForType(field.Type, seen)
			if !omitempty && field.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			sort.Strings(required)
			out["required"] = required
		}
		delete(seen, t)
		return out
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaForType(t.Elem(), seen)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaForType(t.Elem(), seen)}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer"}
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{"type": "string"}
	}
}

func jsonFieldName(field reflect.StructField) (string, bool, bool) {
	name := field.Name
	omitempty := false
	if tag := field.Tag.Get("json"); tag != "" {
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			return "", false, true
		}
		if parts[0] != "" {
			name = parts[0]
		}
		for _, part := range parts[1:] {
			if part == "omitempty" {
				omitempty = true
			}
		}
	}
	return name, omitempty, false
}
