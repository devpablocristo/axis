package knowledgebases

import (
	"context"
	"errors"
	"strings"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const baseColumns = `id,name,description,classification,lifecycle_state,version,created_at,updated_at,archived_at`
const documentColumns = `id,knowledge_base_id,title,artifact_virployee_id,artifact_product_surface,
artifact_subject_id,artifact_repository_generation,artifact_document_id,source_version,source_sha256,
lifecycle_state,version,created_at,updated_at,archived_at`
const resolvedDocumentColumns = `d.id,d.knowledge_base_id,d.title,d.artifact_virployee_id,d.artifact_product_surface,
d.artifact_subject_id,d.artifact_repository_generation,d.artifact_document_id,d.source_version,d.source_sha256,
d.lifecycle_state,d.version,d.created_at,d.updated_at,d.archived_at`
const bindingColumns = `id,knowledge_base_id,scope_type,job_role_id,virployee_id,subject_id,case_id,version,created_at,updated_at`

type scanner interface{ Scan(...any) error }
type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanBase(row scanner) (KnowledgeBase, error) {
	var out KnowledgeBase
	if err := row.Scan(&out.ID, &out.Name, &out.Description, &out.Classification, &out.State, &out.Version, &out.CreatedAt, &out.UpdatedAt, &out.ArchivedAt); err != nil {
		return KnowledgeBase{}, mapError(err, "knowledge base")
	}
	return out, nil
}

func scanDocument(row scanner) (Document, error) {
	var out Document
	if err := row.Scan(&out.ID, &out.KnowledgeBaseID, &out.Title, &out.ArtifactScope.VirployeeID,
		&out.ArtifactScope.ProductSurface, &out.ArtifactScope.SubjectID, &out.ArtifactScope.RepositoryGeneration,
		&out.ArtifactScope.DocumentID, &out.SourceVersion, &out.SourceSHA256, &out.State, &out.Version,
		&out.CreatedAt, &out.UpdatedAt, &out.ArchivedAt); err != nil {
		return Document{}, mapError(err, "knowledge document")
	}
	return out, nil
}

func scanBinding(row scanner) (Binding, error) {
	var out Binding
	if err := row.Scan(&out.ID, &out.KnowledgeBaseID, &out.ScopeType, &out.JobRoleID, &out.VirployeeID,
		&out.SubjectID, &out.CaseID, &out.Version, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return Binding{}, mapError(err, "knowledge binding")
	}
	return out, nil
}

func (r *Repository) Create(ctx context.Context, tenant string, in CreateInput) (KnowledgeBase, error) {
	return scanBase(r.db.QueryRow(ctx, `INSERT INTO companion_knowledge_bases(tenant_id,name,description,classification)
		VALUES($1,$2,$3,$4) RETURNING `+baseColumns, tenant, in.Name, in.Description, in.Classification))
}

func (r *Repository) Get(ctx context.Context, tenant string, id uuid.UUID) (KnowledgeBase, error) {
	return scanBase(r.db.QueryRow(ctx, `SELECT `+baseColumns+` FROM companion_knowledge_bases WHERE tenant_id=$1 AND id=$2`, tenant, id))
}

func (r *Repository) List(ctx context.Context, tenant, state string) ([]KnowledgeBase, error) {
	state = strings.TrimSpace(strings.ToLower(state))
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "archived" {
		return nil, domainerr.Validation("state must be active or archived")
	}
	rows, err := r.db.Query(ctx, `SELECT `+baseColumns+` FROM companion_knowledge_bases
		WHERE tenant_id=$1 AND lifecycle_state=$2 ORDER BY updated_at DESC,id`, tenant, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]KnowledgeBase, 0)
	for rows.Next() {
		item, err := scanBase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Update(ctx context.Context, tenant string, id uuid.UUID, in UpdateInput) (KnowledgeBase, error) {
	item, err := scanBase(r.db.QueryRow(ctx, `UPDATE companion_knowledge_bases SET name=$3,description=$4,
		version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND lifecycle_state='active' AND version=$5
		RETURNING `+baseColumns, tenant, id, in.Name, in.Description, in.ExpectedVersion))
	if domainerr.IsNotFound(err) {
		return KnowledgeBase{}, domainerr.Conflict("knowledge base version conflict or resource is not active")
	}
	return item, err
}

func (r *Repository) Lifecycle(ctx context.Context, tenant string, id uuid.UUID, action string, expectedVersion int64) (KnowledgeBase, error) {
	from, to := "active", "archived"
	if action == "activate" {
		from, to = "archived", "active"
	} else if action != "archive" {
		return KnowledgeBase{}, domainerr.Validation("action must be archive or activate")
	}
	item, err := scanBase(r.db.QueryRow(ctx, `UPDATE companion_knowledge_bases
		SET lifecycle_state=$4,version=version+1,updated_at=now(),archived_at=CASE WHEN $4='archived' THEN now() ELSE NULL END
		WHERE tenant_id=$1 AND id=$2 AND version=$3 AND lifecycle_state=$5 RETURNING `+baseColumns,
		tenant, id, expectedVersion, to, from))
	if domainerr.IsNotFound(err) {
		return KnowledgeBase{}, domainerr.Conflict("knowledge base version or lifecycle conflict")
	}
	return item, err
}

func (r *Repository) RegisterDocument(ctx context.Context, tenant string, baseID uuid.UUID, in RegisterDocumentInput) (Document, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state, classification string
	if err := tx.QueryRow(ctx, `SELECT lifecycle_state,classification FROM companion_knowledge_bases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, baseID).Scan(&state, &classification); err != nil {
		return Document{}, mapError(err, "knowledge base")
	}
	if state != "active" {
		return Document{}, domainerr.Conflict("knowledge base is not active")
	}
	if err := validateDocumentScope(ctx, tx, tenant, baseID, classification, in.ArtifactScope); err != nil {
		return Document{}, err
	}
	id := uuid.New()
	doc, err := scanDocument(tx.QueryRow(ctx, `
		INSERT INTO companion_knowledge_documents(
			id,tenant_id,knowledge_base_id,title,artifact_virployee_id,artifact_product_surface,
			artifact_subject_id,artifact_repository_generation,artifact_document_id,source_version,source_sha256
		)
		SELECT $1,$2,$3,COALESCE(NULLIF(btrim($4),''),a.name),a.virployee_id,a.product_surface,
			a.subject_id,a.repository_generation,a.document_id,c.source_version,a.sha256
		FROM companion_artifacts a
		JOIN LATERAL (
			SELECT source_version FROM companion_artifact_chunks c
			WHERE c.tenant_id=a.tenant_id AND c.virployee_id=a.virployee_id
			  AND c.product_surface=a.product_surface AND c.subject_id=a.subject_id
			  AND c.repository_generation=a.repository_generation AND c.document_id=a.document_id
			  AND c.source_sha256=a.sha256
			ORDER BY c.updated_at DESC LIMIT 1
		) c ON true
		WHERE a.tenant_id=$2 AND a.virployee_id=$5 AND a.product_surface=$6
		  AND a.subject_id=$7 AND a.repository_generation=$8 AND a.document_id=$9
		  AND a.status='indexed'
		RETURNING `+documentColumns,
		id, tenant, baseID, strings.TrimSpace(in.Title), in.ArtifactScope.VirployeeID,
		in.ArtifactScope.ProductSurface, in.ArtifactScope.SubjectID, in.ArtifactScope.RepositoryGeneration,
		in.ArtifactScope.DocumentID))
	if domainerr.IsNotFound(err) {
		return Document{}, domainerr.Validation("artifact document must exist, be indexed, and have verified chunks in this tenant")
	}
	if err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE companion_knowledge_bases SET version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, baseID); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ValidateIngestionScope rejects cross-tenant and classification-incompatible
// targets before any remote bytes are fetched or local bytes are indexed.
func (r *Repository) ValidateIngestionScope(ctx context.Context, tenant string, baseID uuid.UUID, scope ArtifactScope) error {
	if scope.ProductSurface != KnowledgeProductSurface {
		return domainerr.Validation("knowledge ingestion product surface is server-owned")
	}
	var state, classification string
	if err := r.db.QueryRow(ctx, `SELECT lifecycle_state,classification FROM companion_knowledge_bases
		WHERE tenant_id=$1 AND id=$2`, tenant, baseID).Scan(&state, &classification); err != nil {
		return mapError(err, "knowledge base")
	}
	if state != "active" {
		return domainerr.Conflict("knowledge base is not active")
	}
	return validateDocumentScope(ctx, r.db, tenant, baseID, classification, scope)
}

func validateDocumentScope(ctx context.Context, query rowQuerier, tenant string, baseID uuid.UUID, classification string, scope ArtifactScope) error {
	var virployeeExists bool
	if err := query.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenant, scope.VirployeeID).Scan(&virployeeExists); err != nil {
		return err
	}
	if !virployeeExists {
		return domainerr.Validation("artifact scope references a missing or cross-tenant Virployee")
	}
	switch classification {
	case ClassificationProfessional:
		if scope.SubjectID != ProfessionalArtifactSubject {
			return domainerr.Validation("professional knowledge documents must use the non-personal professional artifact subject")
		}
	case ClassificationPrivate:
		subjectID, err := uuid.Parse(strings.TrimSpace(scope.SubjectID))
		if err != nil || subjectID == uuid.Nil {
			return domainerr.Validation("private knowledge documents require a valid work subject UUID")
		}
		var accessible bool
		if err := query.QueryRow(ctx, subjectAccessibleToVirployeeSQL, tenant, scope.VirployeeID, subjectID).Scan(&accessible); err != nil {
			return err
		}
		if !accessible {
			return domainerr.Validation("private knowledge document references a missing, cross-tenant, or inaccessible work subject")
		}
		var incompatible bool
		var bindingCount int
		if err := query.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(
				scope_type NOT IN ('subject','case') OR subject_id<>$3 OR virployee_id<>$4
			),false)
			FROM companion_knowledge_bindings
			WHERE tenant_id=$1 AND knowledge_base_id=$2`, tenant, baseID, subjectID.String(), scope.VirployeeID).Scan(&bindingCount, &incompatible); err != nil {
			return err
		}
		if incompatible || bindingCount > 1 {
			return domainerr.Conflict("private knowledge document does not match the bound subject")
		}
	default:
		return domainerr.Conflict("knowledge base classification is invalid")
	}
	return nil
}

func (r *Repository) ListDocuments(ctx context.Context, tenant string, baseID uuid.UUID, state string) ([]Document, error) {
	state = strings.TrimSpace(strings.ToLower(state))
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "archived" {
		return nil, domainerr.Validation("state must be active or archived")
	}
	rows, err := r.db.Query(ctx, `SELECT `+documentColumns+` FROM companion_knowledge_documents
		WHERE tenant_id=$1 AND knowledge_base_id=$2 AND lifecycle_state=$3 ORDER BY updated_at DESC,id`, tenant, baseID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Document, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ArchiveDocument(ctx context.Context, tenant string, baseID, documentID uuid.UUID, expectedVersion int64) (Document, error) {
	doc, err := scanDocument(r.db.QueryRow(ctx, `UPDATE companion_knowledge_documents
		SET lifecycle_state='archived',version=version+1,updated_at=now(),archived_at=now()
		WHERE tenant_id=$1 AND knowledge_base_id=$2 AND id=$3 AND version=$4 AND lifecycle_state='active'
		RETURNING `+documentColumns, tenant, baseID, documentID, expectedVersion))
	if domainerr.IsNotFound(err) {
		return Document{}, domainerr.Conflict("knowledge document version or lifecycle conflict")
	}
	return doc, err
}

func (r *Repository) ListBindings(ctx context.Context, tenant string, baseID uuid.UUID) ([]Binding, error) {
	rows, err := r.db.Query(ctx, `SELECT `+bindingColumns+` FROM companion_knowledge_bindings
		WHERE tenant_id=$1 AND knowledge_base_id=$2 ORDER BY scope_type,subject_id,id`, tenant, baseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Binding, 0)
	for rows.Next() {
		item, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceBindings(ctx context.Context, tenant string, baseID uuid.UUID, expectedVersion int64, bindings []Binding) ([]Binding, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state, classification string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT lifecycle_state,classification,version FROM companion_knowledge_bases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, baseID).Scan(&state, &classification, &version); err != nil {
		return nil, mapError(err, "knowledge base")
	}
	if state != "active" || version != expectedVersion {
		return nil, domainerr.Conflict("knowledge base version conflict or resource is not active")
	}
	if err := validateBindingsForClassification(classification, bindings); err != nil {
		return nil, err
	}
	privateSubject := ""
	for _, binding := range bindings {
		if err := validateBindingClassification(ctx, tx, tenant, baseID, classification, binding); err != nil {
			return nil, err
		}
		if classification == ClassificationPrivate {
			if privateSubject == "" {
				privateSubject = binding.SubjectID
			} else if privateSubject != binding.SubjectID {
				return nil, domainerr.Validation("a private knowledge base may bind only one subject")
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM companion_knowledge_bindings WHERE tenant_id=$1 AND knowledge_base_id=$2`, tenant, baseID); err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(bindings))
	for _, binding := range bindings {
		if err := validateBindingReference(ctx, tx, tenant, binding); err != nil {
			return nil, err
		}
		binding.KnowledgeBaseID = baseID
		created, err := scanBinding(tx.QueryRow(ctx, `INSERT INTO companion_knowledge_bindings(
			id,tenant_id,knowledge_base_id,scope_type,job_role_id,virployee_id,subject_id,case_id
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+bindingColumns,
			binding.ID, tenant, baseID, binding.ScopeType, binding.JobRoleID, binding.VirployeeID, binding.SubjectID, binding.CaseID))
		if err != nil {
			return nil, err
		}
		out = append(out, created)
	}
	if _, err := tx.Exec(ctx, `UPDATE companion_knowledge_bases SET version=version+1,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, baseID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func validateBindingClassification(ctx context.Context, tx pgx.Tx, tenant string, baseID uuid.UUID, classification string, binding Binding) error {
	if err := validateBindingForClassification(classification, binding); err != nil {
		return err
	}
	switch classification {
	case ClassificationProfessional:
		var incompatible bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM companion_knowledge_documents
			WHERE tenant_id=$1 AND knowledge_base_id=$2 AND lifecycle_state='active'
			  AND artifact_subject_id<>$3
		)`, tenant, baseID, ProfessionalArtifactSubject).Scan(&incompatible); err != nil {
			return err
		}
		if incompatible {
			return domainerr.Conflict("professional knowledge base contains a personal artifact subject")
		}
	case ClassificationPrivate:
		var incompatible bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM companion_knowledge_documents
			WHERE tenant_id=$1 AND knowledge_base_id=$2 AND lifecycle_state='active'
			  AND artifact_subject_id<>$3
		)`, tenant, baseID, binding.SubjectID).Scan(&incompatible); err != nil {
			return err
		}
		if incompatible {
			return domainerr.Conflict("private knowledge base contains a document from another subject")
		}
	}
	return nil
}

const subjectAccessibleToVirployeeSQL = `SELECT EXISTS(
	SELECT 1
	FROM companion_work_subjects s
	JOIN virployees v ON v.tenant_id=s.tenant_id AND v.id=$2
	WHERE s.tenant_id=$1 AND s.id=$3 AND s.archived_at IS NULL
	  AND v.archived_at IS NULL AND v.trashed_at IS NULL
	  AND (
		EXISTS(SELECT 1 FROM companion_continuity_assignments a
			WHERE a.tenant_id=s.tenant_id AND a.subject_id=s.id AND a.virployee_id=v.id AND a.status='active')
		OR EXISTS(SELECT 1 FROM companion_virployee_relationships r
			WHERE r.tenant_id=s.tenant_id AND r.subject_id=s.id AND r.virployee_id=v.id AND r.relationship_type='serves')
	  )
)`

const caseAccessibleToVirployeeSQL = `SELECT EXISTS(
	SELECT 1
	FROM companion_assist_cases c
	JOIN companion_work_subjects s ON s.tenant_id=c.tenant_id AND s.id::text=c.subject_id
	JOIN virployees v ON v.tenant_id=c.tenant_id AND v.id=$4
	WHERE c.tenant_id=$1 AND c.id=$2 AND c.subject_id=$3
	  AND s.archived_at IS NULL AND v.archived_at IS NULL AND v.trashed_at IS NULL
	  AND c.owner_virployee_id=v.id
)`

func validateBindingReference(ctx context.Context, query rowQuerier, tenant string, binding Binding) error {
	var valid bool
	switch binding.ScopeType {
	case ScopeProfessional:
		if err := query.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM job_roles WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenant, binding.JobRoleID).Scan(&valid); err != nil {
			return err
		}
	case ScopeVirployee:
		if err := query.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenant, binding.VirployeeID).Scan(&valid); err != nil {
			return err
		}
	case ScopeSubject:
		subjectID, err := uuid.Parse(strings.TrimSpace(binding.SubjectID))
		if err != nil || subjectID == uuid.Nil {
			return domainerr.Validation("knowledge subject binding requires a valid work subject UUID")
		}
		if err := query.QueryRow(ctx, subjectAccessibleToVirployeeSQL, tenant, binding.VirployeeID, subjectID).Scan(&valid); err != nil {
			return err
		}
	case ScopeCase:
		subjectID, err := uuid.Parse(strings.TrimSpace(binding.SubjectID))
		if err != nil || subjectID == uuid.Nil {
			return domainerr.Validation("knowledge case binding requires a valid work subject UUID")
		}
		if err := query.QueryRow(ctx, caseAccessibleToVirployeeSQL,
			tenant, binding.CaseID, binding.SubjectID, binding.VirployeeID).Scan(&valid); err != nil {
			return err
		}
	}
	if !valid {
		return domainerr.Validation("knowledge binding references a missing or cross-tenant resource")
	}
	return nil
}

func (r *Repository) ListForVirployee(ctx context.Context, tenant string, virployeeID uuid.UUID) ([]VirployeeKnowledgeBase, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenant, virployeeID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, domainerr.NotFound("virployee not found")
	}
	rows, err := r.db.Query(ctx, `
		SELECT kb.id,kb.name,kb.description,kb.classification,kb.lifecycle_state,kb.version,
		       kb.created_at,kb.updated_at,kb.archived_at,
		       b.id,b.knowledge_base_id,b.scope_type,b.job_role_id,b.virployee_id,b.subject_id,b.case_id,
		       b.version,b.created_at,b.updated_at
		FROM companion_knowledge_bases kb
		JOIN companion_knowledge_bindings b ON b.tenant_id=kb.tenant_id AND b.knowledge_base_id=kb.id
		JOIN virployees v ON v.tenant_id=kb.tenant_id AND v.id=$2
		WHERE kb.tenant_id=$1 AND kb.lifecycle_state='active'
		  AND ((b.scope_type='professional' AND b.job_role_id=v.job_role_id) OR b.virployee_id=$2)
		ORDER BY kb.name,kb.id,b.scope_type,b.id
	`, tenant, virployeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VirployeeKnowledgeBase, 0)
	indexes := map[uuid.UUID]int{}
	for rows.Next() {
		var base KnowledgeBase
		var binding Binding
		if err := rows.Scan(
			&base.ID, &base.Name, &base.Description, &base.Classification, &base.State, &base.Version,
			&base.CreatedAt, &base.UpdatedAt, &base.ArchivedAt,
			&binding.ID, &binding.KnowledgeBaseID, &binding.ScopeType, &binding.JobRoleID, &binding.VirployeeID,
			&binding.SubjectID, &binding.CaseID, &binding.Version, &binding.CreatedAt, &binding.UpdatedAt,
		); err != nil {
			return nil, err
		}
		index, ok := indexes[base.ID]
		if !ok {
			index = len(out)
			indexes[base.ID] = index
			out = append(out, VirployeeKnowledgeBase{KnowledgeBase: base, Bindings: []Binding{}})
		}
		out[index].Bindings = append(out[index].Bindings, binding)
	}
	return out, rows.Err()
}

func (r *Repository) SetForVirployee(ctx context.Context, tenant string, virployeeID, baseID uuid.UUID, expectedVersion int64, enabled bool) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var virployeeExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM virployees
		WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)`, tenant, virployeeID).Scan(&virployeeExists); err != nil {
		return err
	}
	if !virployeeExists {
		return domainerr.NotFound("virployee not found")
	}
	var state, classification string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT lifecycle_state,classification,version FROM companion_knowledge_bases
		WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, baseID).Scan(&state, &classification, &version); err != nil {
		return mapError(err, "knowledge base")
	}
	if state != "active" || version != expectedVersion {
		return domainerr.Conflict("knowledge base version conflict or resource is not active")
	}
	if classification != ClassificationProfessional {
		return domainerr.Validation("direct Virployee bindings require a professional knowledge base")
	}
	var tag pgconn.CommandTag
	if enabled {
		tag, err = tx.Exec(ctx, `INSERT INTO companion_knowledge_bindings(
			id,tenant_id,knowledge_base_id,scope_type,virployee_id)
			SELECT $1,$2,$3,'virployee',$4
			WHERE NOT EXISTS(SELECT 1 FROM companion_knowledge_bindings
				WHERE tenant_id=$2 AND knowledge_base_id=$3 AND scope_type='virployee' AND virployee_id=$4)`,
			uuid.New(), tenant, baseID, virployeeID)
	} else {
		tag, err = tx.Exec(ctx, `DELETE FROM companion_knowledge_bindings
			WHERE tenant_id=$1 AND knowledge_base_id=$2 AND scope_type='virployee' AND virployee_id=$3`,
			tenant, baseID, virployeeID)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `UPDATE companion_knowledge_bases SET version=version+1,updated_at=now()
			WHERE tenant_id=$1 AND id=$2`, tenant, baseID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ResolvedDocuments returns only active documents from active bases whose
// binding exactly matches the current professional/virployee/subject/case
// context.  The predicate runs before artifact retrieval.
func (r *Repository) ResolvedDocuments(ctx context.Context, scope RetrievalScope) ([]Document, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT `+resolvedDocumentColumns+`
		FROM companion_knowledge_documents d
		JOIN companion_knowledge_bases kb ON kb.tenant_id=d.tenant_id AND kb.id=d.knowledge_base_id
		JOIN companion_knowledge_bindings b ON b.tenant_id=kb.tenant_id AND b.knowledge_base_id=kb.id
		JOIN virployees v ON v.tenant_id=kb.tenant_id AND v.id=$2
		WHERE kb.tenant_id=$1 AND kb.lifecycle_state='active' AND d.lifecycle_state='active'
		  AND (
			(kb.classification='professional' AND d.artifact_subject_id='professional' AND (
			  (b.scope_type='professional' AND b.job_role_id=v.job_role_id) OR
			  (b.scope_type='virployee' AND b.virployee_id=$2)
			)) OR
			(kb.classification='private' AND (
			  SELECT count(*) FROM companion_knowledge_bindings private_scope
			  WHERE private_scope.tenant_id=kb.tenant_id AND private_scope.knowledge_base_id=kb.id
			)=1 AND d.artifact_subject_id=b.subject_id AND (
			  (b.scope_type='subject' AND b.virployee_id=$2 AND $3<>'' AND b.subject_id=$3) OR
			  (b.scope_type='case' AND b.virployee_id=$2 AND $3<>'' AND b.subject_id=$3 AND b.case_id=$4)
			))
		  )
		ORDER BY d.updated_at DESC,d.id
	`, scope.TenantID, scope.VirployeeID, strings.TrimSpace(scope.SubjectID), nullableUUID(scope.CaseID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Document, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func mapError(err error, resource string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound(resource + " not found")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domainerr.Conflict(resource + " already exists")
	}
	return err
}
