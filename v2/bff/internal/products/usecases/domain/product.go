package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	StatusActive  = "active"
	StateActive   = "active"
	StateArchived = "archived"
	StateTrashed  = "trashed"
)

type Product struct {
	ID             uuid.UUID
	ProductSurface string
	Name           string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time

	ArchivedAt *time.Time
	TrashedAt  *time.Time
	PurgeAfter *time.Time
}

type ListInput struct {
	Lifecycle string
}

type CreateInput struct {
	Name           string
	ProductSurface string
	PrincipalID    string
}

type UpdateInput struct {
	ProductID   string
	Name        string
	PrincipalID string
}

type LifecycleInput struct {
	ProductID   string
	PrincipalID string
}

type NormalizedListInput struct {
	Lifecycle string
}

type NormalizedCreateInput struct {
	Name           string
	ProductSurface string
	PrincipalID    string
}

type NormalizedUpdateInput struct {
	ProductID   uuid.UUID
	Name        string
	PrincipalID string
}

type NormalizedLifecycleInput struct {
	ProductID   uuid.UUID
	PrincipalID string
}

func NormalizeListInput(in ListInput) NormalizedListInput {
	return NormalizedListInput{Lifecycle: NormalizeState(in.Lifecycle)}
}

func NormalizeCreateInput(in CreateInput) (NormalizedCreateInput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NormalizedCreateInput{}, domainerr.Validation("name is required")
	}
	productSurface := NormalizeProductSurface(in.ProductSurface)
	if productSurface == "" {
		productSurface = NormalizeProductSurface(name)
	}
	if productSurface == "" {
		return NormalizedCreateInput{}, domainerr.Validation("product_surface is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedCreateInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedCreateInput{
		Name:           name,
		ProductSurface: productSurface,
		PrincipalID:    principalID,
	}, nil
}

func NormalizeUpdateInput(in UpdateInput) (NormalizedUpdateInput, error) {
	productID, err := ParseProductID(in.ProductID)
	if err != nil {
		return NormalizedUpdateInput{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("name is required")
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedUpdateInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedUpdateInput{
		ProductID:   productID,
		Name:        name,
		PrincipalID: principalID,
	}, nil
}

func NormalizeLifecycleInput(in LifecycleInput) (NormalizedLifecycleInput, error) {
	productID, err := ParseProductID(in.ProductID)
	if err != nil {
		return NormalizedLifecycleInput{}, err
	}
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return NormalizedLifecycleInput{}, domainerr.Validation("principal_id is required")
	}
	return NormalizedLifecycleInput{
		ProductID:   productID,
		PrincipalID: principalID,
	}, nil
}

func ParseProductID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domainerr.Validation("product_id must be a valid UUID")
	}
	return id, nil
}

func NormalizeState(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case StateArchived:
		return StateArchived
	case "trash", StateTrashed:
		return StateTrashed
	default:
		return StateActive
	}
}

func NormalizeProductSurface(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	out := make([]rune, 0, len(raw))
	lastDash := false
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = append(out, r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func (p Product) IsUsable() bool {
	return p.Status == StatusActive && p.ArchivedAt == nil && p.TrashedAt == nil
}

func (p Product) State() string {
	if p.TrashedAt != nil {
		return StateTrashed
	}
	if p.ArchivedAt != nil {
		return StateArchived
	}
	return StateActive
}
