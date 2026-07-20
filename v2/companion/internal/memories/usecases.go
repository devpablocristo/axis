package memories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

type UseCases struct{ repo *Repository }

func NewUseCases(repo *Repository) *UseCases { return &UseCases{repo: repo} }
func (u *UseCases) authorize(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error {
	return u.repo.Authorized(ctx, strings.TrimSpace(tenant), virployee, strings.TrimSpace(actor), strings.ToLower(strings.TrimSpace(role)))
}

// Authorize exposes the same per-virployee role gate the human write paths use
// (owner/admin, or the assigned supervisor), so other modules that install
// memories on a human's behalf (e.g. learning's accept) enforce it too.
func (u *UseCases) Authorize(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error {
	return u.authorize(ctx, tenant, virployee, actor, role)
}
func (u *UseCases) Create(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string, in CreateInput) (Memory, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	in.Provenance, in.ActorID = "human", actor
	m, err := u.repo.Create(ctx, tenant, virployee, in)
	return redact(m, false), err
}

// CreateSystem is the internal-only write port. It is deliberately not exposed by Handler.
func (u *UseCases) CreateSystem(ctx context.Context, tenant string, virployee uuid.UUID, actor, source string, in CreateInput) (Memory, error) {
	in.Provenance, in.ActorID, in.SourceReference = "system", actor, source
	return u.repo.Create(ctx, tenant, virployee, in)
}
func (u *UseCases) Get(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string) (Memory, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	m, err := u.repo.Get(ctx, tenant, virployee, id)
	return redact(m, true), err
}
func (u *UseCases) List(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string, in ListInput) (Page, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Page{}, err
	}
	p, err := u.repo.List(ctx, tenant, virployee, in)
	if err != nil {
		return Page{}, err
	}
	for i := range p.Items {
		p.Items[i] = redact(p.Items[i], false)
	}
	return p, nil
}
func (u *UseCases) Update(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string, in UpdateInput) (Memory, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return Memory{}, err
	}
	in.ActorID = actor
	m, err := u.repo.Update(ctx, tenant, virployee, id, in)
	return redact(m, false), err
}
func (u *UseCases) Recall(ctx context.Context, tenant string, virployee uuid.UUID, actor, role, query string, limit int) ([]Reference, error) {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return nil, err
	}
	items, err := u.repo.Recall(ctx, tenant, virployee, query, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]Reference, len(items))
	for i := range items {
		refs[i] = items[i].Reference
	}
	return refs, nil
}
func (u *UseCases) RecallInternal(ctx context.Context, tenant string, virployee uuid.UUID, query string, limit int) ([]Recalled, error) {
	return u.repo.Recall(ctx, tenant, virployee, query, limit)
}
func (u *UseCases) Lifecycle(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role, action string) error {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return err
	}
	return u.repo.Lifecycle(ctx, tenant, virployee, id, action, actor)
}
func (u *UseCases) Purge(ctx context.Context, tenant string, virployee, id uuid.UUID, actor, role string) error {
	if err := u.authorize(ctx, tenant, virployee, actor, role); err != nil {
		return err
	}
	return u.repo.Purge(ctx, tenant, virployee, id, actor)
}
func ContextHash(refs []Reference) string {
	raw, _ := json.Marshal(refs)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
