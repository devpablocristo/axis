package inbound

import (
	"bytes"
	"encoding/json"
	"reflect"
	"unicode/utf8"
)

// matchesEventSchema validates the conservative JSON Schema subset used by
// product event contracts. Unsupported constraints do not authorize a value;
// structural keywords used here are evaluated recursively with a depth cap.
func matchesEventSchema(schemaJSON, valueJSON json.RawMessage) bool {
	if len(schemaJSON) == 0 {
		// Legacy in-memory bindings did not carry schemas. Persisted contracts
		// always do; this branch exists only for the compatibility constructor.
		return true
	}
	var schema map[string]any
	if err := decodeJSON(schemaJSON, &schema); err != nil {
		return false
	}
	var value any
	if err := decodeJSON(valueJSON, &value); err != nil {
		return false
	}
	return matchesSchemaValue(schema, value, 0)
}

func decodeJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(out)
}

func matchesSchemaValue(schema map[string]any, value any, depth int) bool {
	if depth > 32 {
		return false
	}
	if rawType, ok := schema["type"].(string); ok && !matchesJSONType(rawType, value) {
		return false
	}
	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if reflect.DeepEqual(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				name, ok := raw.(string)
				if !ok {
					return false
				}
				if _, exists := typed[name]; !exists {
					return false
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, item := range typed {
			property, declared := properties[name]
			if !declared {
				if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
					return false
				}
				continue
			}
			propertySchema, ok := property.(map[string]any)
			if !ok || !matchesSchemaValue(propertySchema, item, depth+1) {
				return false
			}
		}
	case []any:
		if minItems, ok := jsonInteger(schema["minItems"]); ok && len(typed) < minItems {
			return false
		}
		if maxItems, ok := jsonInteger(schema["maxItems"]); ok && len(typed) > maxItems {
			return false
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for _, item := range typed {
				if !matchesSchemaValue(itemSchema, item, depth+1) {
					return false
				}
			}
		}
	case string:
		length := utf8.RuneCountInString(typed)
		if minLength, ok := jsonInteger(schema["minLength"]); ok && length < minLength {
			return false
		}
		if maxLength, ok := jsonInteger(schema["maxLength"]); ok && length > maxLength {
			return false
		}
	}
	return true
}

func matchesJSONType(wanted string, value any) bool {
	switch wanted {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := number.Int64()
		return err == nil
	case "null":
		return value == nil
	default:
		return false
	}
}

func jsonInteger(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	if err != nil || parsed < 0 {
		return 0, false
	}
	return int(parsed), true
}
