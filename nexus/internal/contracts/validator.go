package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ValidateSchema validates a deterministic JSON-schema subset used by Nexus
// governance contracts. It intentionally supports the primitives Nexus needs
// for wire-contract enforcement without embedding a broad, nondeterministic
// validation engine in governance paths.
func ValidateSchema(payload map[string]any, schema map[string]any) []string {
	if len(schema) == 0 {
		return nil
	}
	var errs []string
	if typ := stringValue(schema["type"]); typ != "" && typ != "object" {
		return []string{"schema root type must be object"}
	}
	if required, ok := stringList(schema["required"]); ok {
		for _, key := range required {
			if strings.TrimSpace(key) == "" {
				continue
			}
			value, exists := payload[key]
			if !exists || isEmptyValue(value) {
				errs = append(errs, fmt.Sprintf("%s is required", key))
			}
		}
	} else if _, exists := schema["required"]; exists {
		errs = append(errs, "schema required must be an array")
	}

	properties, _ := schema["properties"].(map[string]any)
	additional := true
	if raw, ok := schema["additionalProperties"].(bool); ok {
		additional = raw
	}
	if !additional {
		for key := range payload {
			if _, ok := properties[key]; !ok {
				errs = append(errs, fmt.Sprintf("%s is not allowed", key))
			}
		}
	}
	for key, rawSpec := range properties {
		value, exists := payload[key]
		if !exists || value == nil {
			continue
		}
		spec, ok := rawSpec.(map[string]any)
		if !ok {
			continue
		}
		errs = append(errs, validateProperty(key, value, spec)...)
	}
	return errs
}

func validateProperty(key string, value any, spec map[string]any) []string {
	var errs []string
	if constValue, ok := spec["const"]; ok && fmt.Sprint(value) != fmt.Sprint(constValue) {
		errs = append(errs, fmt.Sprintf("%s must be %s", key, fmt.Sprint(constValue)))
	}
	switch stringValue(spec["type"]) {
	case "", "any":
	case "string":
		v, ok := value.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s must be a string", key))
			break
		}
		if minLength, ok := intValue(spec["minLength"]); ok && len(strings.TrimSpace(v)) < minLength {
			errs = append(errs, fmt.Sprintf("%s must have length >= %d", key, minLength))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			errs = append(errs, fmt.Sprintf("%s must be a boolean", key))
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			errs = append(errs, fmt.Sprintf("%s must be an object", key))
		}
	case "array":
		if _, ok := value.([]any); !ok {
			errs = append(errs, fmt.Sprintf("%s must be an array", key))
		}
	case "number":
		if _, ok := numberValue(value); !ok {
			errs = append(errs, fmt.Sprintf("%s must be a number", key))
		}
	case "integer":
		n, ok := numberValue(value)
		if !ok || math.Trunc(n) != n {
			errs = append(errs, fmt.Sprintf("%s must be an integer", key))
		}
	default:
		errs = append(errs, fmt.Sprintf("%s has unsupported schema type %q", key, stringValue(spec["type"])))
	}
	return errs
}

func HashPayload(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func stringValue(value any) string {
	v, _ := value.(string)
	return strings.TrimSpace(v)
}

func stringList(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	default:
		return nil, false
	}
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if math.Trunc(typed) == typed {
			return int(typed), true
		}
	}
	return 0, false
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}
