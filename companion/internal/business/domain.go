package business

import (
	"fmt"
	"strings"
	"time"
)

type Model struct {
	ID             string         `json:"id,omitempty"`
	OrgID          string         `json:"org_id"`
	ProductSurface string         `json:"product_surface"`
	Version        int            `json:"version"`
	Status         string         `json:"status"`
	Organization   Organization   `json:"organization"`
	Areas          []Area         `json:"areas,omitempty"`
	Roles          []Role         `json:"roles,omitempty"`
	Users          []Actor        `json:"users,omitempty"`
	Agents         []Actor        `json:"agents,omitempty"`
	Workflows      []Workflow     `json:"workflows,omitempty"`
	Processes      []Process      `json:"processes,omitempty"`
	Rules          []Rule         `json:"rules,omitempty"`
	Preferences    []Preference   `json:"preferences,omitempty"`
	Priorities     []Priority     `json:"priorities,omitempty"`
	Exceptions     []Exception    `json:"exceptions,omitempty"`
	Vocabulary     []Vocabulary   `json:"vocabulary,omitempty"`
	Schedules      []Schedule     `json:"schedules,omitempty"`
	SLAs           []SLA          `json:"slas,omitempty"`
	Tools          []ToolRef      `json:"tools,omitempty"`
	Relationships  []Relationship `json:"relationships,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
	CreatedBy      string         `json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
}

type Organization struct {
	Name        string `json:"name,omitempty"`
	Industry    string `json:"industry,omitempty"`
	Description string `json:"description,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
}

type Area struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	AreaID      string   `json:"area_id,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type Actor struct {
	ID     string   `json:"id"`
	Name   string   `json:"name,omitempty"`
	RoleID string   `json:"role_id,omitempty"`
	AreaID string   `json:"area_id,omitempty"`
	Tools  []string `json:"tools,omitempty"`
}

type Workflow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	OwnerRoleID string   `json:"owner_role_id,omitempty"`
	Steps       []string `json:"steps,omitempty"`
	Triggers    []string `json:"triggers,omitempty"`
}

type Process struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	WorkflowID  string   `json:"workflow_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Exceptions  []string `json:"exceptions,omitempty"`
}

type Rule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	AppliesTo   string `json:"applies_to,omitempty"`
}

type Preference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Priority struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Weight int    `json:"weight,omitempty"`
}

type Exception struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
}

type Vocabulary struct {
	Term       string `json:"term"`
	Meaning    string `json:"meaning"`
	Equivalent string `json:"equivalent,omitempty"`
}

type Schedule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone,omitempty"`
	Window   string `json:"window,omitempty"`
}

type SLA struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Priority string `json:"priority,omitempty"`
}

type ToolRef struct {
	ID         string `json:"id"`
	Capability string `json:"capability,omitempty"`
}

type Relationship struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

func (m Model) Summary() string {
	var lines []string
	if strings.TrimSpace(m.Organization.Name) != "" {
		lines = append(lines, "Organización: "+m.Organization.Name)
	}
	if m.Organization.Industry != "" {
		lines = append(lines, "Industria: "+m.Organization.Industry)
	}
	appendNamed := func(label string, values any, count int) {
		if count > 0 {
			lines = append(lines, fmt.Sprintf("%s: %d definidos", label, count))
		}
		_ = values
	}
	appendNamed("Áreas", m.Areas, len(m.Areas))
	appendNamed("Roles", m.Roles, len(m.Roles))
	appendNamed("Workflows", m.Workflows, len(m.Workflows))
	appendNamed("Procesos", m.Processes, len(m.Processes))
	appendNamed("Reglas internas", m.Rules, len(m.Rules))
	if len(m.Priorities) > 0 {
		var priorities []string
		for _, p := range m.Priorities {
			priorities = append(priorities, p.Name)
		}
		lines = append(lines, "Prioridades: "+strings.Join(priorities, ", "))
	}
	if len(m.SLAs) > 0 {
		var slas []string
		for _, sla := range m.SLAs {
			slas = append(slas, sla.Name+" -> "+sla.Target)
		}
		lines = append(lines, "SLAs: "+strings.Join(slas, "; "))
	}
	if len(m.Vocabulary) > 0 {
		var terms []string
		for _, term := range m.Vocabulary {
			terms = append(terms, term.Term+"="+term.Meaning)
		}
		lines = append(lines, "Vocabulario: "+strings.Join(terms, "; "))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Modelo empresarial:\n- " + strings.Join(lines, "\n- ")
}
