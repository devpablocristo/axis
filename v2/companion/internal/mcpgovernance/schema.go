package mcpgovernance

import (
	"fmt"
	"math"
	"reflect"
	"strings"
)

// ValidateJSONSchema implements the deterministic subset used by capability
// manifests. Unsupported schema keywords are ignored, but an unsupported type
// or malformed schema fails closed.
func ValidateJSONSchema(schema map[string]any, value any) error {
	return validateSchemaAt(schema, value, "$", 0)
}

func validateSchemaAt(schema map[string]any, value any, path string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("schema nesting exceeds limit")
	}
	typeName, _ := schema["type"].(string)
	if typeName != "" && !matchesJSONType(typeName, value) {
		return fmt.Errorf("%s must be %s", path, typeName)
	}
	if choices, ok := schema["enum"].([]any); ok {
		matched := false
		for _, choice := range choices {
			if reflect.DeepEqual(choice, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is not an allowed value", path)
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		if err := validateObject(schema, typed, path, depth); err != nil {
			return err
		}
	case []any:
		if err := validateArray(schema, typed, path, depth); err != nil {
			return err
		}
	case string:
		if minimum, ok := numberKeyword(schema, "minLength"); ok && len([]rune(typed)) < int(minimum) {
			return fmt.Errorf("%s is shorter than minLength", path)
		}
		if maximum, ok := numberKeyword(schema, "maxLength"); ok && len([]rune(typed)) > int(maximum) {
			return fmt.Errorf("%s is longer than maxLength", path)
		}
	}
	return nil
}

func validateObject(schema map[string]any, value map[string]any, path string, depth int) error {
	required, _ := schema["required"].([]any)
	for _, raw := range required {
		name, ok := raw.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("schema required list is invalid")
		}
		if _, exists := value[name]; !exists {
			return fmt.Errorf("%s.%s is required", path, name)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for name, item := range value {
		rawSchema, exists := properties[name]
		if !exists {
			if allowed, ok := schema["additionalProperties"].(bool); ok && !allowed {
				return fmt.Errorf("%s.%s is not allowed", path, name)
			}
			continue
		}
		child, ok := rawSchema.(map[string]any)
		if !ok {
			return fmt.Errorf("schema for %s.%s is invalid", path, name)
		}
		if err := validateSchemaAt(child, item, path+"."+name, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateArray(schema map[string]any, value []any, path string, depth int) error {
	if minimum, ok := numberKeyword(schema, "minItems"); ok && len(value) < int(minimum) {
		return fmt.Errorf("%s has fewer than minItems", path)
	}
	if maximum, ok := numberKeyword(schema, "maxItems"); ok && len(value) > int(maximum) {
		return fmt.Errorf("%s has more than maxItems", path)
	}
	rawItems, ok := schema["items"]
	if !ok {
		return nil
	}
	items, ok := rawItems.(map[string]any)
	if !ok {
		return fmt.Errorf("schema items is invalid")
	}
	for index, item := range value {
		if err := validateSchemaAt(items, item, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func matchesJSONType(name string, value any) bool {
	switch name {
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
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	case "null":
		return value == nil
	default:
		return false
	}
}

func numberKeyword(schema map[string]any, key string) (float64, bool) {
	value, ok := schema[key].(float64)
	return value, ok
}
