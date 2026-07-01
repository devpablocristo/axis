package tasks

import (
	"fmt"
	"strings"
)

func stringFromBinding(binding map[string]any, key, fallback string) string {
	if binding == nil {
		return fallback
	}
	value, ok := binding[key]
	if !ok || value == nil {
		return fallback
	}
	out := strings.TrimSpace(fmt.Sprint(value))
	if out == "" || out == "<nil>" {
		return fallback
	}
	return out
}
