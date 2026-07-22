package memories

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
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

type memoryScopeQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const memoryColumns = `id, virployee_id, scope_type, subject_id, case_id, title, memory_type, content, sensitivity, provenance, actor_id,
COALESCE(source_reference,'') AS source_reference, content_hash, version, lifecycle_state, trust_score, review_state,
review_reason, poisoning_flags, pii_flags, expires_at, decay_at, last_recalled_at, recall_count,
reviewed_by, reviewed_at, embedding_model, embedding_version, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanMemory(s scanner) (Memory, error) {
	var m Memory
	err := s.Scan(
		&m.ID, &m.VirployeeID, &m.ScopeType, &m.SubjectID, &m.CaseID, &m.Title, &m.Type, &m.Content, &m.Sensitivity, &m.Provenance,
		&m.ActorID, &m.SourceReference, &m.ContentHash, &m.Version, &m.State, &m.TrustScore,
		&m.ReviewState, &m.ReviewReason, &m.PoisoningFlags, &m.PIIFlags, &m.ExpiresAt, &m.DecayAt,
		&m.LastRecalledAt, &m.RecallCount, &m.ReviewedBy, &m.ReviewedAt, &m.EmbeddingModel,
		&m.EmbeddingVersion, &m.CreatedAt, &m.UpdatedAt,
	)
	return m, mapError(err)
}

func (r *Repository) Authorized(ctx context.Context, organization string, virployee uuid.UUID, actor, role string) error {
	if role == "owner" || role == "admin" {
		var ok bool
		err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees WHERE org_id=$1 AND id=$2)`, organization, virployee).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			return domainerr.NotFound("virployee not found")
		}
		return nil
	}
	var supervisor string
	err := r.db.QueryRow(ctx, `SELECT supervisor_user_id FROM virployees WHERE org_id=$1 AND id=$2`, organization, virployee).Scan(&supervisor)
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

func (r *Repository) HasActiveConflict(ctx context.Context, organization string, virployee, exclude uuid.UUID, scope Scope, title, memoryType, contentHash string) (bool, error) {
	scope, err := NormalizeScope(scope)
	if err != nil {
		return false, err
	}
	var conflict bool
	err = r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM companion_virployee_memories
			WHERE org_id=$1 AND virployee_id=$2 AND id<>$3
			  AND lifecycle_state='active' AND review_state<>'rejected'
			  AND memory_type=$4 AND lower(btrim(title))=lower(btrim($5))
			  AND content_hash<>$6 AND scope_type=$7 AND subject_id=$8
			  AND case_id IS NOT DISTINCT FROM $9
		)
	`, strings.TrimSpace(organization), virployee, exclude, memoryType, title, contentHash,
		scope.Type, scope.SubjectID, scope.CaseID).Scan(&conflict)
	return conflict, err
}

func (r *Repository) Create(ctx context.Context, organization string, virployee uuid.UUID, in CuratedInput) (Memory, error) {
	hash := ContentHash(in.Content)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateMemoryScopeAccess(ctx, tx, organization, virployee, in.Scope); err != nil {
		return Memory{}, err
	}
	m, err := scanMemory(tx.QueryRow(ctx, `
		INSERT INTO companion_virployee_memories(
			org_id,virployee_id,scope_type,subject_id,case_id,title,content,memory_type,sensitivity,provenance,actor_id,
			source_reference,content_hash,trust_score,review_state,review_reason,
			poisoning_flags,pii_flags,expires_at,decay_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING `+memoryColumns,
		organization, virployee, in.Scope.Type, in.Scope.SubjectID, in.Scope.CaseID,
		in.Title, in.Content, in.Type, in.Sensitivity, in.Provenance, in.ActorID,
		in.SourceReference, hash, in.TrustScore, in.ReviewState, in.ReviewReason,
		in.PoisoningFlags, in.PIIFlags, in.ExpiresAt, in.DecayAt))
	if err != nil {
		return Memory{}, err
	}
	if safeForPrompt(m) {
		if err := enqueueMemoryIndex(ctx, tx, organization, m); err != nil {
			return Memory{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_virployee_memory_audit(org_id,virployee_id,memory_id,action,actor_id,resulting_hash,resulting_version,scope_type,subject_id,case_id) VALUES($1,$2,$3,'create',$4,$5,$6,$7,$8,$9)`, organization, virployee, m.ID, in.ActorID, m.ContentHash, m.Version, m.ScopeType, m.SubjectID, m.CaseID)
	if err != nil {
		return Memory{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return m, nil
}

func (r *Repository) Get(ctx context.Context, organization string, virployee, id uuid.UUID) (Memory, error) {
	return scanMemory(r.db.QueryRow(ctx, `SELECT `+memoryColumns+` FROM companion_virployee_memories WHERE org_id=$1 AND virployee_id=$2 AND id=$3`, organization, virployee, id))
}
func (r *Repository) List(ctx context.Context, organization string, virployee uuid.UUID, in ListInput) (Page, error) {
	scope, err := NormalizeScope(in.Scope)
	if err != nil {
		return Page{}, err
	}
	if err := validateMemoryScopeAccess(ctx, r.db, organization, virployee, scope); err != nil {
		return Page{}, err
	}
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
	q := `SELECT ` + memoryColumns + ` FROM companion_virployee_memories WHERE org_id=$1 AND virployee_id=$2 AND lifecycle_state=$3 AND scope_type=$4 AND subject_id=$5 AND case_id IS NOT DISTINCT FROM $6 AND ($7='' OR to_tsvector('simple',title||' '||content) @@ websearch_to_tsquery('simple',$7)) AND (NOT $8 OR (updated_at,id)<($9,$10)) ORDER BY updated_at DESC,id DESC LIMIT $11`
	rows, err := r.db.Query(ctx, q, organization, virployee, state, scope.Type, scope.SubjectID, scope.CaseID, strings.TrimSpace(in.Query), hasCursor, cursorTime, cursorID, in.Limit+1)
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

func (r *Repository) Update(ctx context.Context, organization string, virployee, id uuid.UUID, in CuratedInput, expectedVersion int) (Memory, error) {
	if expectedVersion <= 0 {
		return Memory{}, domainerr.Validation("expected_version is required")
	}
	hash := ContentHash(in.Content)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM companion_virployee_memories WHERE org_id=$1 AND virployee_id=$2 AND id=$3 FOR UPDATE`, organization, virployee, id))
	if err != nil {
		return Memory{}, err
	}
	if old.Version != expectedVersion {
		return Memory{}, domainerr.Conflict("memory version conflict")
	}
	m, err := scanMemory(tx.QueryRow(ctx, `
		UPDATE companion_virployee_memories
		SET title=$4,content=$5,memory_type=$6,sensitivity=$7,content_hash=$8,
			trust_score=$9,review_state=$10,review_reason=$11,poisoning_flags=$12,pii_flags=$13,
			expires_at=$14,decay_at=$15,reviewed_by='',reviewed_at=NULL,
			embedding=NULL,embedding_model='',embedding_version='',embedding_content_hash='',
			version=version+1,updated_at=now()
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3
		RETURNING `+memoryColumns,
		organization, virployee, id, in.Title, in.Content, in.Type, in.Sensitivity, hash,
		in.TrustScore, in.ReviewState, in.ReviewReason, in.PoisoningFlags, in.PIIFlags,
		in.ExpiresAt, in.DecayAt))
	if err != nil {
		return Memory{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_virployee_memory_audit(org_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,previous_version,resulting_version,scope_type,subject_id,case_id) VALUES($1,$2,$3,'update',$4,$5,$6,$7,$8,$9,$10,$11)`, organization, virployee, id, in.ActorID, old.ContentHash, m.ContentHash, old.Version, m.Version, m.ScopeType, m.SubjectID, m.CaseID)
	if err != nil {
		return Memory{}, err
	}
	if safeForPrompt(m) {
		if err := enqueueMemoryIndex(ctx, tx, organization, m); err != nil {
			return Memory{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return m, nil
}

func (r *Repository) Review(ctx context.Context, organization string, virployee, id uuid.UUID, actor, decision string) (Memory, error) {
	if !oneOf(decision, "approve", "reject") {
		return Memory{}, domainerr.Validation("decision must be approve or reject")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM companion_virployee_memories WHERE org_id=$1 AND virployee_id=$2 AND id=$3 FOR UPDATE`, organization, virployee, id))
	if err != nil {
		return Memory{}, err
	}
	if !oneOf(old.ReviewState, ReviewPending, ReviewQuarantined) {
		return Memory{}, domainerr.Conflict("memory is not awaiting review")
	}
	next := ReviewApproved
	if decision == "reject" {
		next = ReviewRejected
	}
	m, err := scanMemory(tx.QueryRow(ctx, `
		UPDATE companion_virployee_memories
		SET review_state=$4, trust_score=CASE WHEN $4='approved' THEN GREATEST(trust_score,0.70) ELSE trust_score END,
			reviewed_by=$5,reviewed_at=now(),updated_at=now()
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3
		RETURNING `+memoryColumns, organization, virployee, id, next, actor))
	if err != nil {
		return Memory{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"previous_review_state": old.ReviewState, "review_state": next})
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_virployee_memory_audit(
			org_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,
			previous_version,resulting_version,metadata,scope_type,subject_id,case_id
		) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$7,$8,$9,$10,$11)
	`, organization, virployee, id, "review_"+decision, actor, m.ContentHash, m.Version, metadata, m.ScopeType, m.SubjectID, m.CaseID)
	if err != nil {
		return Memory{}, err
	}
	if safeForPrompt(m) {
		if err := enqueueMemoryIndex(ctx, tx, organization, m); err != nil {
			return Memory{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return m, nil
}

func (r *Repository) IndexCandidate(ctx context.Context, organization string, id uuid.UUID, version int) (Memory, error) {
	return scanMemory(r.db.QueryRow(ctx, `
		SELECT `+memoryColumns+` FROM companion_virployee_memories
		WHERE org_id=$1 AND id=$2 AND version=$3 AND lifecycle_state='active'
		  AND review_state='approved' AND trust_score >= $4
		  AND sensitivity='normal' AND cardinality(poisoning_flags)=0
		  AND review_reason<>'conflicting_memory_requires_review'
		  AND (scope_type<>'virployee' OR memory_type='procedure')
		  AND (expires_at IS NULL OR expires_at > now())
	`, organization, id, version, RecallTrustFloor))
}

func (r *Repository) StoreEmbedding(ctx context.Context, organization string, memory Memory, values []float32, model string) error {
	if len(values) != EmbeddingDimensions || strings.TrimSpace(model) == "" {
		return domainerr.Validation("memory embedding shape or model is invalid")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE companion_virployee_memories
		SET embedding=$5::vector,embedding_model=$6,embedding_version=$7,
			embedding_content_hash=content_hash,updated_at=now()
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3 AND version=$4
		  AND lifecycle_state='active' AND review_state='approved' AND trust_score >= $8
		  AND sensitivity='normal' AND cardinality(poisoning_flags)=0
		  AND review_reason<>'conflicting_memory_requires_review'
	`, organization, memory.VirployeeID, memory.ID, memory.Version, memoryVectorLiteral(values), strings.TrimSpace(model), EmbeddingVersion, RecallTrustFloor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.Conflict("memory changed before indexing completed")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO companion_virployee_memory_audit(
			org_id,virployee_id,memory_id,action,actor_id,resulting_hash,resulting_version,metadata,scope_type,subject_id,case_id
		) VALUES($1,$2,$3,'index','system:memory-indexer',$4,$5,$6,$7,$8,$9)
	`, organization, memory.VirployeeID, memory.ID, memory.ContentHash, memory.Version,
		json.RawMessage(`{"embedding_version":"memory-embed.v1"}`), memory.ScopeType, memory.SubjectID, memory.CaseID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) DecayDue(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 250
	}
	tag, err := r.db.Exec(ctx, `
		WITH picked AS (
			SELECT id FROM companion_virployee_memories
			WHERE lifecycle_state='active' AND decay_at IS NOT NULL AND decay_at <= now()
			ORDER BY decay_at,id LIMIT $1 FOR UPDATE SKIP LOCKED
		), changed AS (
			UPDATE companion_virployee_memories AS memory
			SET trust_score=GREATEST(0,trust_score*0.85),
				lifecycle_state=CASE WHEN expires_at IS NOT NULL AND expires_at<=now() THEN 'archived' ELSE lifecycle_state END,
				review_reason=CASE WHEN expires_at IS NOT NULL AND expires_at<=now() THEN 'retention_expired' ELSE review_reason END,
				decay_at=CASE WHEN expires_at IS NOT NULL AND expires_at<=now() THEN NULL ELSE now()+interval '30 days' END,
				updated_at=now()
			FROM picked WHERE memory.id=picked.id
			RETURNING memory.org_id,memory.virployee_id,memory.id,memory.content_hash,memory.version,
			          memory.scope_type,memory.subject_id,memory.case_id
		)
		INSERT INTO companion_virployee_memory_audit(
			org_id,virployee_id,memory_id,action,actor_id,resulting_hash,resulting_version,metadata,
			scope_type,subject_id,case_id
		)
		SELECT org_id,virployee_id,id,'decay','system:memory-decay',content_hash,version,
		       '{"factor":0.85}'::jsonb,scope_type,subject_id,case_id FROM changed
	`, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) Lifecycle(ctx context.Context, organization string, virployee, id uuid.UUID, action, actor string) error {
	from, to := map[string]string{"archive": "active", "unarchive": "archived", "trash": "active", "restore": "trash"}[action], map[string]string{"archive": "archived", "unarchive": "active", "trash": "trash", "restore": "active"}[action]
	if from == "" {
		return domainerr.Validation("invalid lifecycle action")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	m, err := scanMemory(tx.QueryRow(ctx, `UPDATE companion_virployee_memories SET lifecycle_state=$4,archived_at=CASE WHEN $4='archived' THEN now() ELSE NULL END,trashed_at=CASE WHEN $4='trash' THEN now() ELSE trashed_at END,purge_after=CASE WHEN $4='trash' THEN now()+interval '30 days' ELSE NULL END,updated_at=now() WHERE org_id=$1 AND virployee_id=$2 AND id=$3 AND (lifecycle_state=$5 OR ($6 AND lifecycle_state='archived')) RETURNING `+memoryColumns, organization, virployee, id, to, from, action == "trash"))
	if err != nil {
		return mapError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_virployee_memory_audit(org_id,virployee_id,memory_id,action,actor_id,previous_hash,resulting_hash,previous_version,resulting_version,scope_type,subject_id,case_id) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$7,$8,$9,$10)`, organization, virployee, id, action, actor, m.ContentHash, m.Version, m.ScopeType, m.SubjectID, m.CaseID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) Purge(ctx context.Context, organization string, virployee, id uuid.UUID, actor string) error {
	tag, err := r.db.Exec(ctx, `WITH deleted AS (DELETE FROM companion_virployee_memories WHERE org_id=$1 AND virployee_id=$2 AND id=$3 AND lifecycle_state='trash' RETURNING content_hash,version,scope_type,subject_id,case_id) INSERT INTO companion_virployee_memory_audit(org_id,virployee_id,memory_id,action,actor_id,previous_hash,previous_version,scope_type,subject_id,case_id) SELECT $1,$2,$3,'purge',$4,content_hash,version,scope_type,subject_id,case_id FROM deleted`, organization, virployee, id, actor)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domainerr.Conflict("memory may only be purged after 30 days in trash")
	}
	return nil
}

func (r *Repository) Recall(ctx context.Context, organization string, virployee uuid.UUID, query string, limit int, vector []float32, model string) ([]Recalled, error) {
	return r.RecallScoped(ctx, organization, virployee, Scope{}, query, limit, vector, model)
}

func (r *Repository) RecallScoped(ctx context.Context, organization string, virployee uuid.UUID, scope Scope, query string, limit int, vector []float32, model string) ([]Recalled, error) {
	if strings.TrimSpace(query) == "" {
		return nil, domainerr.Validation("query is required")
	}
	scope, err := NormalizeScope(scope)
	if err != nil {
		return nil, err
	}
	if err := validateMemoryScopeAccess(ctx, r.db, organization, virployee, scope); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	var rows pgx.Rows
	if len(vector) == EmbeddingDimensions && strings.TrimSpace(model) != "" {
		rows, err = r.db.Query(ctx, `
			WITH scoped AS MATERIALIZED (
				SELECT `+memoryColumns+`,
				       CASE WHEN embedding IS NOT NULL AND embedding_model=$4
				                  AND embedding_version=$5 AND embedding_content_hash=content_hash
				            THEN embedding <=> $6::vector ELSE 1 END AS vector_distance,
				       ts_rank_cd(to_tsvector('simple',title||' '||content),
				                  websearch_to_tsquery('simple',regexp_replace(trim($3),'\s+',' OR ','g'))) AS text_rank
				FROM companion_virployee_memories
				WHERE org_id=$1 AND virployee_id=$2 AND lifecycle_state='active'
				  AND review_state='approved' AND trust_score >= $7
				  AND sensitivity='normal' AND cardinality(poisoning_flags)=0
				  AND review_reason<>'conflicting_memory_requires_review'
				  AND (scope_type<>'virployee' OR memory_type='procedure')
				  AND (expires_at IS NULL OR expires_at > now())
				  AND (scope_type='virployee'
				       OR ($8 IN ('subject','case') AND scope_type='subject' AND subject_id=$9)
				       OR ($8='case' AND scope_type='case' AND subject_id=$9 AND case_id=$10))
			)
			SELECT `+memoryColumns+`,
			       (GREATEST(0,1-vector_distance)*0.75 + LEAST(1,text_rank)*0.25) AS score
			FROM scoped ORDER BY score DESC,updated_at DESC,id DESC LIMIT $11
		`, organization, virployee, query, strings.TrimSpace(model), EmbeddingVersion,
			memoryVectorLiteral(vector), RecallTrustFloor, scope.Type, scope.SubjectID, scope.CaseID, limit)
	} else {
		rows, err = r.db.Query(ctx, `
			SELECT `+memoryColumns+`,
			       ts_rank_cd(to_tsvector('simple',title||' '||content),
			                  websearch_to_tsquery('simple',regexp_replace(trim($3),'\s+',' OR ','g'))) score
			FROM companion_virployee_memories
			WHERE org_id=$1 AND virployee_id=$2 AND lifecycle_state='active'
			  AND review_state='approved' AND trust_score >= $5
			  AND sensitivity='normal' AND cardinality(poisoning_flags)=0
			  AND review_reason<>'conflicting_memory_requires_review'
			  AND (scope_type<>'virployee' OR memory_type='procedure')
			  AND (expires_at IS NULL OR expires_at > now())
			  AND (scope_type='virployee'
			       OR ($6 IN ('subject','case') AND scope_type='subject' AND subject_id=$7)
			       OR ($6='case' AND scope_type='case' AND subject_id=$7 AND case_id=$8))
			  AND to_tsvector('simple',title||' '||content) @@
			      websearch_to_tsquery('simple',regexp_replace(trim($3),'\s+',' OR ','g'))
			ORDER BY score DESC,updated_at DESC,id DESC LIMIT $4
		`, organization, virployee, query, limit, RecallTrustFloor, scope.Type, scope.SubjectID, scope.CaseID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recalled{}
	for rows.Next() {
		m, score, scanErr := scanRecalled(rows)
		err = scanErr
		if err != nil {
			return nil, err
		}
		out = append(out, Recalled{Memory: m, Reference: Reference{ID: m.ID, Title: m.Title, Type: m.Type, Version: m.Version, Hash: m.ContentHash, Sensitivity: m.Sensitivity, Score: score, ScopeType: m.ScopeType, SubjectID: m.SubjectID, CaseID: m.CaseID}})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		ids := make([]uuid.UUID, 0, len(out))
		for _, item := range out {
			ids = append(ids, item.Memory.ID)
		}
		_, _ = r.db.Exec(ctx, `
			UPDATE companion_virployee_memories SET last_recalled_at=now(),recall_count=recall_count+1
			WHERE org_id=$1 AND virployee_id=$2 AND id=ANY($3)
		`, organization, virployee, ids)
	}
	return out, nil
}

// validateMemoryScopeAccess prevents a caller from turning a syntactically
// valid subject or case identifier into a cross-patient lookup. A case belongs
// only to its current owner; entrypoint_virployee_id is historical metadata and
// deliberately grants no access after reassignment.
func validateMemoryScopeAccess(ctx context.Context, queryer memoryScopeQueryer, organization string, virployee uuid.UUID, scope Scope) error {
	if scope.Type == ScopeVirployee {
		return nil
	}
	var allowed bool
	var err error
	switch scope.Type {
	case ScopeSubject:
		err = queryer.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM companion_work_subjects AS subject
				WHERE subject.org_id=$1 AND subject.id=$2 AND subject.archived_at IS NULL
				  AND (
					EXISTS (
						SELECT 1 FROM companion_continuity_assignments AS assignment
						WHERE assignment.org_id=$1 AND assignment.subject_id=subject.id
						  AND assignment.virployee_id=$3 AND assignment.status='active'
					)
					OR EXISTS (
						SELECT 1 FROM companion_virployee_relationships AS relationship
						WHERE relationship.org_id=$1 AND relationship.subject_id=subject.id
						  AND relationship.virployee_id=$3
						  AND relationship.relationship_type IN ('serves','works_for')
					)
				  )
			)
		`, strings.TrimSpace(organization), scope.SubjectID, virployee).Scan(&allowed)
	case ScopeCase:
		err = queryer.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM companion_assist_cases AS assist_case
				JOIN companion_work_subjects AS subject
				  ON subject.org_id=assist_case.org_id
				 AND subject.id::text=assist_case.subject_id
				WHERE assist_case.org_id=$1 AND assist_case.id=$2
				  AND assist_case.subject_id=$3 AND assist_case.owner_virployee_id=$4
				  AND assist_case.status IN ('open','needs_human')
				  AND subject.archived_at IS NULL
			)
		`, strings.TrimSpace(organization), scope.CaseID, scope.SubjectID, virployee).Scan(&allowed)
	default:
		return domainerr.Validation("memory scope type must be virployee, subject, or case")
	}
	if err != nil {
		return err
	}
	if !allowed {
		return domainerr.Forbidden("memory scope is not assigned to this Virployee")
	}
	return nil
}

func scanRecalled(s scanner) (Memory, float64, error) {
	var m Memory
	var score float64
	err := s.Scan(
		&m.ID, &m.VirployeeID, &m.ScopeType, &m.SubjectID, &m.CaseID, &m.Title, &m.Type, &m.Content, &m.Sensitivity, &m.Provenance,
		&m.ActorID, &m.SourceReference, &m.ContentHash, &m.Version, &m.State, &m.TrustScore,
		&m.ReviewState, &m.ReviewReason, &m.PoisoningFlags, &m.PIIFlags, &m.ExpiresAt, &m.DecayAt,
		&m.LastRecalledAt, &m.RecallCount, &m.ReviewedBy, &m.ReviewedAt, &m.EmbeddingModel,
		&m.EmbeddingVersion, &m.CreatedAt, &m.UpdatedAt, &score,
	)
	return m, score, mapError(err)
}

func enqueueMemoryIndex(ctx context.Context, tx pgx.Tx, organization string, memory Memory) error {
	payload, err := json.Marshal(IndexJobPayload{MemoryID: memory.ID.String(), Version: memory.Version})
	if err != nil {
		return err
	}
	jobID := uuid.New()
	dedupe := memory.ID.String() + ":" + strconv.Itoa(memory.Version)
	tag, err := tx.Exec(ctx, `
		INSERT INTO companion_jobs(
			id,org_id,product_surface,kind,shard_key,dedupe_key,payload_json,
			status,max_attempts,run_after,timeout_seconds
		) VALUES($1,$2,'companion','memory.index',$3,$4,$5,'queued',5,now(),120)
		ON CONFLICT (org_id,product_surface,kind,dedupe_key) DO NOTHING
	`, jobID, organization, memory.VirployeeID.String(), dedupe, payload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		_, err = tx.Exec(ctx, `
			INSERT INTO companion_job_events(job_id,event,metadata_json)
			VALUES($1,'queued','{"source":"memory_write"}'::jsonb)
		`, jobID)
	}
	return err
}

func memoryVectorLiteral(values []float32) string {
	var builder strings.Builder
	builder.Grow(len(values) * 10)
	builder.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			value = 0
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
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
