package executiongate

import (
	"fmt"
	"strings"
)

type PrincipalContext struct {
	Type string
	ID   string
}

var validPrincipalTypes = map[string]bool{
	"person": true, "organization": true, "team": true,
	"case": true, "project": true, "service": true,
}

// NormalizePrincipalContext accepts an entirely absent principal for legacy
// actions. Professional authority decides whether absence is allowed; partial
// or malformed context is never accepted.
func NormalizePrincipalContext(in PrincipalContext) (PrincipalContext, error) {
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.ID = strings.TrimSpace(in.ID)
	if in.Type == "" && in.ID == "" {
		return PrincipalContext{}, nil
	}
	if !validPrincipalTypes[in.Type] {
		return PrincipalContext{}, fmt.Errorf("principal_type is invalid")
	}
	if in.ID == "" || len([]rune(in.ID)) > 256 {
		return PrincipalContext{}, fmt.Errorf("principal_id is required and must be at most 256 characters")
	}
	return in, nil
}
