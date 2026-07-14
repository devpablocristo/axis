package memories

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/devpablocristo/platform/http/go/pagination"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const memoryColumns = `id, virployee_id, title, memory_type, content, sensitivity, provenance, actor_id, COALESCE(source_reference,''), content_hash, version, lifecycle_state, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanMemory(s scanner) (Memory, error) {
	var m Memory
	err := s.Scan(&m.ID, &m.VirployeeID, &m.Title, &m.Type, &m.Content, &m.Sensitivity, &m.Provenance, &m.ActorID, &m.SourceReference, &m.ContentHash, &m.Version, &m.State, &m.CreatedAt, &m.UpdatedAt)
	return m, mapError(err)
}

func (r *Repository) Authorized(ctx context.Context, tenant string, virployee uuid.UUID, actor, role string) error {
	if role == "owner" || role == "admin" {
		var ok bool
		err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees WHERE tenant_id=$1 AND id=$2)`, tenant, virployee).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			return domainerr.NotFound("virployee not found")
		}
		return nil
	}
	var supervisor string
	err := r.db.QueryRow(ctx, `SELECT supervisor_user_id FROM virployees WHERE tenant_id=$1 AND id=$2`, tenant, virployee).Scan(&supervisor)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound("virployee not found")
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(actor) == "" || actor != supervisor {
		return domainerr.Forbidden("memory access requires the assigned supervisor or an owner/admin")
	}
	return nil
}

func (r *Repository) Create(ctx context.Context, tenant string, virployee uuid.UUID, in CreateInput) (Memory, error) {
	in, err := normalizeCreate(in)
	if err != nil {
		return Memory{}, err
	}
	hash := ContentHash(in.Content)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	m, err := scanMemory(tx.QueryRow(ctx, `INSERT INTO companion_memories(tenant_id,virployee_id,title,content,memory_type,sensitivity,provenance,actor_id,source_reference,content_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10) RETURNING `+memoryColumns, tenant, virployee, in.Title, in.Content, in.Type, in.Sensitivity, in.Provenance, in.ActorID, in.SourceReference, hash))
	if err != nil {
		return Memory{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_memory_audit(tenant_id,virployee_id,memory_id,action,actor_id,resulting_hash,resulting_version) VALUES($1,$2,$3,'create',$4,$5,$6)`, tenant, virployee, m.ID, in.ActorID, m.ContentHash, m.Version)
	if err != nil {
		return Memory{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return m, nil
}

func (r *Repository) Get(ctx context.Context, tenant string, virployee, id uuid.UUID) (Memory, error) {
	return scanMemory(r.db.QueryRow(ctx, `SELECT `+memoryColumns+` FROM companion_memories WHERE tenant_id=$1 AND virployee_id=$2 AND id=$3`, tenant, virployee, id))
}
func (r *Repository) List(ctx context.Context, tenant string, virployee uuid.UUID, in ListInput) (Page, error) {
	state := in.State
	if state == "" {
		state = "active"
	}
	if !oneOf(state, "active", "archived", "trash") {
		return Page{}, domainerr.Validation("state must be active, archived, or trash")
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	cursorTime, cursorID, hasCursor, err := decodeMemoryCursor(in.Cursor)
	if err != nil {
		return Page{}, err
	}
	q := `SELECT ` + memoryColumns + ` FROM companion_memories WHERE tenant_id=$1 AND virployee_id=$2 AND lifecycle_state=$3 AND ($4='' OR to_tsvector('simple',title||' '||content) @@ websearch_to_tsquery('simple',$4)) AND (NOT $5 OR (updated_at,id)<($6,$7)) ORDER BY updated_at DESC,id DESC LIMIT $8`
	rows, err := r.db.Query(ctx, q, tenant, virployee, state, strings.TrimSpace(in.Query), hasCursor, cursorTime, cursorID, in.Limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	out := Page{Items: []Memory{}}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			return Page{}, e
		}
		out.Items = append(out.Items, m)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	if len(out.Items) > in.Limit {
		last := out.Items[in.Limit-1]
		out.Items = out.Items[:in.Limit]
		out.NextCursor, err = encodeMemoryCursor(last)
		if err != nil {
			return Page{}, err
		}
	}
	return out, nil
}

func decodeMemoryCursor(raw string) (time.Time, uuid.UUID, bool, error) {
	cursor, ok, err := pagination.DecodeTimeIDCursor(strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, uuid.Nil, false, domainerr.Validation("invalid cursor")
	}
	if !ok {
		return time.Time{}, uuid.Nil, false, nil
	}
	id, err := uuid.Parse(cursor.ID)
	if err != nil {
		return time.Time{}, uuid.Nil, false, domainerr.Validation("invalid cursor")
	}
	return cursor.CreatedAt.UTC(), id, true, nil
}

func encodeMemoryCursor(memory Memory) (string, error) {
	return pagination.EncodeTimeIDCursor(pagination.TimeIDCursor{
		CreatedAt: memory.UpdatedAt.UTC(),
		ID:        memory.ID.String(),
	})
}

func (r *Repository) Update(ctx context.Context, tenant string, virployee, id uuid.UUID, in UpdateInput) (Memory, error) {
	n, err := normalizeCreate(CreateInput{Title: in.Title, Type: in.Type, Content: in.Content, Sensitivity: in.Sensitivity, Provenance: "human", ActorID: in.ActorID})
	if err != nil {
		return Memory{}, err
	}
	if in.ExpectedVersion <= 0 {
		return Memory{}, domainerr.Validation("expected_version is required")
	}
	hash := ContentHash(n.Content)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM companion_memories WHERE tenant_id=$1 AND virployee_id=$2 AND id=$3 FOR UPDATE`, tenant, virployee, id))
	if err != nil {
		return Memory{}, err
	}
	if old.Version != in.ExpectedVersion {
		return Memory{}, domainerr.Conflict("memory version conflict")
	}
	m, err := scanMemory(tx.QueryRow(ctx, `UPDATE companion_memories SET title=$4,content=$5,memory_type=$6,sensitivity=$7,content_hash=$8,version=version+1,updated_at=now() WHERE tenant_id=$1 AND virployee_id=$2 AND id=$3 RETURNING `+memoryColumns, tenant, virployee, id, n.Title, n.Content, n.Type, n.Sensitivity, hash))
	if err != nil {
		return Memory{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_memory_audit(tenant_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,previous_version,resulting_version) VALUES($1,$2,$3,'update',$4,$5,$6,$7,$8)`, tenant, virployee, id, in.ActorID, old.ContentHash, m.ContentHash, old.Version, m.Version)
	if err != nil {
		return Memory{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return m, nil
}

func (r *Repository) Lifecycle(ctx context.Context, tenant string, virployee, id uuid.UUID, action, actor string) error {
	from, to := map[string]string{"archive": "active", "unarchive": "archived", "trash": "active", "restore": "trash"}[action], map[string]string{"archive": "archived", "unarchive": "active", "trash": "trash", "restore": "active"}[action]
	if from == "" {
		return domainerr.Validation("invalid lifecycle action")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	m, err := scanMemory(tx.QueryRow(ctx, `UPDATE companion_memories SET lifecycle_state=$4,archived_at=CASE WHEN $4='archived' THEN now() ELSE NULL END,trashed_at=CASE WHEN $4='trash' THEN now() ELSE trashed_at END,purge_after=CASE WHEN $4='trash' THEN now()+interval '30 days' ELSE NULL END,updated_at=now() WHERE tenant_id=$1 AND virployee_id=$2 AND id=$3 AND (lifecycle_state=$5 OR ($6 AND lifecycle_state='archived')) RETURNING `+memoryColumns, tenant, virployee, id, to, from, action == "trash"))
	if err != nil {
		return mapError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_memory_audit(tenant_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,previous_version,resulting_version) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$7)`, tenant, virployee, id, action, actor, m.ContentHash, m.Version)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) Purge(ctx context.Context, tenant string, virployee, id uuid.UUID, actor string) error {
	tag, err := r.db.Exec(ctx, `WITH deleted AS (DELETE FROM companion_memories WHERE tenant_id=$1 AND virployee_id=$2 AND id=$3 AND lifecycle_state='trash' AND purge_after<=now() RETURNING content_hash,version) INSERT INTO companion_memory_audit(tenant_id,virployee_id,memory_id,action,actor_id,previous_hash,previous_version) SELECT $1,$2,$3,'purge',$4,content_hash,version FROM deleted`, tenant, virployee, id, actor)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.Conflict("memory may only be purged after 30 days in trash")
	}
	return nil
}

func (r *Repository) Recall(ctx context.Context, tenant string, virployee uuid.UUID, query string, limit int) ([]Recalled, error) {
	if strings.TrimSpace(query) == "" {
		return nil, domainerr.Validation("query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	rows, err := r.db.Query(ctx, `SELECT `+memoryColumns+`,ts_rank_cd(to_tsvector('simple',title||' '||content),websearch_to_tsquery('simple',regexp_replace(trim($3),'\s+',' OR ','g'))) score FROM companion_memories WHERE tenant_id=$1 AND virployee_id=$2 AND lifecycle_state='active' AND to_tsvector('simple',title||' '||content)@@websearch_to_tsquery('simple',regexp_replace(trim($3),'\s+',' OR ','g')) ORDER BY score DESC,updated_at DESC,id DESC LIMIT $4`, tenant, virployee, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recalled{}
	for rows.Next() {
		var m Memory
		var score float64
		err = rows.Scan(&m.ID, &m.VirployeeID, &m.Title, &m.Type, &m.Content, &m.Sensitivity, &m.Provenance, &m.ActorID, &m.SourceReference, &m.ContentHash, &m.Version, &m.State, &m.CreatedAt, &m.UpdatedAt, &score)
		if err != nil {
			return nil, err
		}
		out = append(out, Recalled{Memory: m, Reference: Reference{ID: m.ID, Title: m.Title, Type: m.Type, Version: m.Version, Hash: m.ContentHash, Sensitivity: m.Sensitivity, Score: score}})
	}
	return out, rows.Err()
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound("memory not found")
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return domainerr.Conflict("an active memory with the same content already exists")
	}
	return err
}
