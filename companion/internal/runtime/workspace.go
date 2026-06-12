package runtime

import (
	"encoding/json"
	"strings"
)

// effectiveWorkspace resuelve el workspace operativo del run: el campo
// top-level del request gana; si no viene, cae al workspace embebido en el
// handoff (compatibilidad UI legacy).
func effectiveWorkspace(in RunInput) map[string]any {
	if workspace := topLevelWorkspace(in.Workspace); len(workspace) > 0 {
		return workspace
	}
	return handoffWorkspace(in.Handoff)
}

// topLevelWorkspace parsea el workspace top-level del request (el raw ES el
// objeto workspace, no un contenedor).
func topLevelWorkspace(raw json.RawMessage) map[string]any {
	var workspace map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &workspace) != nil {
		return nil
	}
	return sanitizeWorkspace(workspace)
}

// handoffWorkspace extrae el objeto workspace embebido en un handoff crudo.
func handoffWorkspace(raw json.RawMessage) map[string]any {
	var root map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil
	}
	workspace, ok := root["workspace"].(map[string]any)
	if !ok {
		return nil
	}
	return sanitizeWorkspace(workspace)
}

// sanitizeWorkspace descarta entradas sin valor útil (nil, strings vacíos,
// ids numéricos <= 0). Devuelve nil si no queda nada.
func sanitizeWorkspace(workspace map[string]any) map[string]any {
	if len(workspace) == 0 {
		return nil
	}
	out := make(map[string]any, len(workspace))
	for key, value := range workspace {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(v) == "" {
				continue
			}
		case float64:
			if v <= 0 {
				continue
			}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
