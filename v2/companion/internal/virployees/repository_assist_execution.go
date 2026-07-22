package virployees

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/memories"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtraces"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

// AssistSourceAuthorizationHash snapshots the monotonic catalog revisions that
// authorized every source supplied to Runtime. Source bytes alone are not
// enough: archive/activate and binding remove/re-add cycles must revoke an old
// answer even when the indexed chunk is byte-identical.
func (r *Repository) AssistSourceAuthorizationHash(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, run AssistRun, citations []knowledgebases.Citation) (string, error) {
	records := make([]string, 0, len(citations))
	for _, citation := range citations {
		locator := citation.Locator
		if len(locator) == 0 || !json.Valid(locator) {
			locator = json.RawMessage(`null`)
		}
		// Clinical capability responses intentionally expose only the canonical
		// document reference. Recover the internal knowledge-base identity from
		// the organization-scoped catalog before recomputing authorization.
		if citation.KnowledgeBaseID == nil {
			if documentID, parseErr := uuid.Parse(strings.TrimSpace(citation.DocumentID)); parseErr == nil && documentID != uuid.Nil {
				var knowledgeBaseID uuid.UUID
				lookupErr := r.pool.QueryRow(ctx, `SELECT knowledge_base_id
					FROM companion_knowledge_documents
					WHERE org_id=$1 AND id=$2 AND lifecycle_state='active'
					  AND artifact_virployee_id=$3 AND artifact_product_surface=$4
					  AND artifact_subject_id=$5 AND artifact_repository_generation=$6
					  AND source_version=$7 AND source_sha256=$8`, orgID, documentID, virployeeID,
					run.ProductSurface, strings.TrimSpace(run.SubjectID), run.RepositoryGeneration,
					strings.TrimSpace(citation.SourceVersion), strings.TrimSpace(citation.SHA256)).Scan(&knowledgeBaseID)
				if lookupErr == nil && knowledgeBaseID != uuid.Nil {
					citation.KnowledgeBaseID = &knowledgeBaseID
				}
			}
		}
		if citation.KnowledgeBaseID == nil {
			if strings.TrimSpace(citation.SourceVersion) != strings.TrimSpace(run.RepositoryGeneration) {
				return "", domainerr.Conflict("Assist artifact source version changed")
			}
			var artifactID uuid.UUID
			err := r.pool.QueryRow(ctx, `SELECT a.id
				FROM companion_artifacts a
				JOIN companion_artifact_chunks c
				  ON c.org_id=a.org_id AND c.virployee_id=a.virployee_id
				 AND c.product_surface=a.product_surface AND c.subject_id=a.subject_id
				 AND c.repository_generation=a.repository_generation AND c.document_id=a.document_id
				WHERE a.org_id=$1 AND a.virployee_id=$2 AND a.product_surface=$3
				  AND a.subject_id=$4 AND a.repository_generation=$5 AND a.document_id=$6
				  AND a.status='indexed' AND a.sha256=$7 AND c.source_sha256=$7
				  AND c.locator=$8::jsonb
				ORDER BY c.id LIMIT 1`, orgID, run.VirployeeID, run.ProductSurface,
				strings.TrimSpace(run.SubjectID), caseScopedRepositoryGeneration(run.RepositoryGeneration, run.CaseID),
				strings.TrimSpace(citation.DocumentID), strings.TrimSpace(citation.SHA256), locator).Scan(&artifactID)
			if err != nil {
				return "", domainerr.Conflict("Assist artifact authorization changed")
			}
			records = append(records, strings.Join([]string{
				"artifact", artifactID.String(), strings.TrimSpace(citation.DocumentID),
				strings.TrimSpace(citation.SourceVersion), strings.TrimSpace(citation.SHA256), string(locator),
			}, "\x1f"))
			continue
		}

		documentID, err := uuid.Parse(strings.TrimSpace(citation.DocumentID))
		if err != nil || documentID == uuid.Nil || *citation.KnowledgeBaseID == uuid.Nil {
			return "", domainerr.Conflict("Assist knowledge citation is invalid")
		}
		rows, err := r.pool.Query(ctx, `SELECT DISTINCT kb.version,d.version,b.id,b.version
			FROM companion_knowledge_documents d
			JOIN companion_knowledge_bases kb
			  ON kb.org_id=d.org_id AND kb.id=d.knowledge_base_id
			JOIN companion_knowledge_bindings b
			  ON b.org_id=kb.org_id AND b.knowledge_base_id=kb.id
			JOIN companion_artifacts a
			  ON a.org_id=d.org_id AND a.virployee_id=d.artifact_virployee_id
			 AND a.product_surface=d.artifact_product_surface AND a.subject_id=d.artifact_subject_id
			 AND a.repository_generation=d.artifact_repository_generation AND a.document_id=d.artifact_document_id
			JOIN companion_artifact_chunks c
			  ON c.org_id=a.org_id AND c.virployee_id=a.virployee_id
			 AND c.product_surface=a.product_surface AND c.subject_id=a.subject_id
			 AND c.repository_generation=a.repository_generation AND c.document_id=a.document_id
			WHERE d.org_id=$1 AND d.id=$2 AND d.knowledge_base_id=$3
			  AND kb.lifecycle_state='active' AND d.lifecycle_state='active' AND a.status='indexed'
			  AND d.source_version=$4 AND d.source_sha256=$5
			  AND a.sha256=$5 AND c.source_sha256=$5 AND c.source_version=$4 AND c.locator=$6::jsonb
			  AND (
				(kb.classification='professional' AND d.artifact_subject_id='professional' AND (
				  (b.scope_type='professional' AND b.job_role_id=$7) OR
				  (b.scope_type='virployee' AND b.virployee_id=$8)
				)) OR
				(kb.classification='private' AND d.artifact_subject_id=b.subject_id AND (
				  (b.scope_type='subject' AND b.virployee_id=$8 AND $9<>'' AND b.subject_id=$9) OR
				  (b.scope_type='case' AND b.virployee_id=$8 AND $9<>'' AND b.subject_id=$9 AND b.case_id=$10)
				))
			  )
			ORDER BY b.id`, orgID, documentID, *citation.KnowledgeBaseID,
			strings.TrimSpace(citation.SourceVersion), strings.TrimSpace(citation.SHA256), locator,
			jobRoleID, virployeeID, strings.TrimSpace(run.SubjectID), nullableAssistUUID(run.CaseID))
		if err != nil {
			return "", err
		}
		grants := make([]string, 0)
		for rows.Next() {
			var baseVersion, documentVersion, bindingVersion int64
			var bindingID uuid.UUID
			if err := rows.Scan(&baseVersion, &documentVersion, &bindingID, &bindingVersion); err != nil {
				rows.Close()
				return "", err
			}
			grants = append(grants, strings.Join([]string{
				(*citation.KnowledgeBaseID).String(), strconv.FormatInt(baseVersion, 10),
				documentID.String(), strconv.FormatInt(documentVersion, 10),
				bindingID.String(), strconv.FormatInt(bindingVersion, 10),
			}, "\x1f"))
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return "", rowsErr
		}
		if len(grants) == 0 {
			return "", domainerr.Conflict("Assist source authorization changed")
		}
		sort.Strings(grants)
		records = append(records, strings.Join([]string{
			"knowledge", strings.TrimSpace(citation.DocumentID), strings.TrimSpace(citation.SourceVersion),
			strings.TrimSpace(citation.SHA256), string(locator), strings.Join(grants, "\x1e"),
		}, "\x1f"))
	}
	sort.Strings(records)
	return runtraces.HashString("assist-source-authorization.v1\x00" + strings.Join(records, "\x00")), nil
}

// ValidateAssistExecutionContext checks the mutable rows referenced by a
// completed Assist. Every predicate is organization-scoped and exact; a sibling
// subject, archived source, replaced chunk, or changed memory cannot satisfy it.
func (r *Repository) ValidateAssistExecutionContext(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, run AssistRun) error {
	if run.CaseID != uuid.Nil {
		var valid bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM companion_assist_cases
			WHERE org_id=$1 AND id=$2 AND subject_id=$3 AND status='open'
			  AND owner_virployee_id=$4
		)`, orgID, run.CaseID, strings.TrimSpace(run.SubjectID), virployeeID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return domainerr.Conflict("Assist case changed or no longer belongs to this Virployee")
		}
	}

	for _, citation := range run.SourceContext {
		if err := r.validateAssistCitation(ctx, orgID, virployeeID, jobRoleID, run, citation); err != nil {
			return err
		}
	}
	if strings.EqualFold(strings.TrimSpace(run.GroundingMode), "sources_only") && len(run.SourceContext) == 0 && run.CapabilityKey == "" {
		return domainerr.Conflict("source-only Assist has no durable citations")
	}
	for _, reference := range run.MemoryReferences {
		if err := r.validateAssistMemoryReference(ctx, orgID, virployeeID, reference); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) validateAssistCitation(ctx context.Context, orgID string, virployeeID, jobRoleID uuid.UUID, run AssistRun, citation knowledgebases.Citation) error {
	locator := citation.Locator
	if len(locator) == 0 || !json.Valid(locator) {
		locator = json.RawMessage(`null`)
	}
	var valid bool
	if citation.KnowledgeBaseID == nil {
		if strings.TrimSpace(citation.SourceVersion) != strings.TrimSpace(run.RepositoryGeneration) {
			return domainerr.Conflict("Assist artifact source version changed")
		}
		scopedGeneration := caseScopedRepositoryGeneration(run.RepositoryGeneration, run.CaseID)
		err := r.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1
			FROM companion_artifacts a
			JOIN companion_artifact_chunks c
			  ON c.org_id=a.org_id AND c.virployee_id=a.virployee_id
			 AND c.product_surface=a.product_surface AND c.subject_id=a.subject_id
			 AND c.repository_generation=a.repository_generation AND c.document_id=a.document_id
			WHERE a.org_id=$1 AND a.virployee_id=$2 AND a.product_surface=$3
			  AND a.subject_id=$4 AND a.repository_generation=$5 AND a.document_id=$6
			  AND a.status='indexed' AND a.sha256=$7 AND c.source_sha256=$7
			  AND c.locator=$8::jsonb
		)`, orgID, run.VirployeeID, run.ProductSurface, strings.TrimSpace(run.SubjectID),
			scopedGeneration, strings.TrimSpace(citation.DocumentID), strings.TrimSpace(citation.SHA256), locator).Scan(&valid)
		if err != nil {
			return err
		}
	} else {
		documentID, err := uuid.Parse(strings.TrimSpace(citation.DocumentID))
		if err != nil || documentID == uuid.Nil || *citation.KnowledgeBaseID == uuid.Nil {
			return domainerr.Conflict("Assist knowledge citation is invalid")
		}
		err = r.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1
			FROM companion_knowledge_documents d
			JOIN companion_knowledge_bases kb
			  ON kb.org_id=d.org_id AND kb.id=d.knowledge_base_id
			JOIN companion_knowledge_bindings b
			  ON b.org_id=kb.org_id AND b.knowledge_base_id=kb.id
			JOIN companion_artifacts a
			  ON a.org_id=d.org_id AND a.virployee_id=d.artifact_virployee_id
			 AND a.product_surface=d.artifact_product_surface AND a.subject_id=d.artifact_subject_id
			 AND a.repository_generation=d.artifact_repository_generation AND a.document_id=d.artifact_document_id
			JOIN companion_artifact_chunks c
			  ON c.org_id=a.org_id AND c.virployee_id=a.virployee_id
			 AND c.product_surface=a.product_surface AND c.subject_id=a.subject_id
			 AND c.repository_generation=a.repository_generation AND c.document_id=a.document_id
			WHERE d.org_id=$1 AND d.id=$2 AND d.knowledge_base_id=$3
			  AND kb.lifecycle_state='active' AND d.lifecycle_state='active' AND a.status='indexed'
			  AND d.source_version=$4 AND d.source_sha256=$5
			  AND a.sha256=$5 AND c.source_sha256=$5 AND c.source_version=$4 AND c.locator=$6::jsonb
			  AND (
				(kb.classification='professional' AND d.artifact_subject_id='professional' AND (
				  (b.scope_type='professional' AND b.job_role_id=$7) OR
				  (b.scope_type='virployee' AND b.virployee_id=$8)
				)) OR
				(kb.classification='private' AND d.artifact_subject_id=b.subject_id AND (
				  (b.scope_type='subject' AND b.virployee_id=$8 AND $9<>'' AND b.subject_id=$9) OR
				  (b.scope_type='case' AND b.virployee_id=$8 AND $9<>'' AND b.subject_id=$9 AND b.case_id=$10)
				))
			  )
		)`, orgID, documentID, *citation.KnowledgeBaseID, strings.TrimSpace(citation.SourceVersion),
			strings.TrimSpace(citation.SHA256), locator, jobRoleID, virployeeID,
			strings.TrimSpace(run.SubjectID), nullableAssistUUID(run.CaseID)).Scan(&valid)
		if err != nil {
			return err
		}
	}
	if !valid {
		return domainerr.Conflict("Assist citation is no longer active in the approved subject context")
	}
	return nil
}

func (r *Repository) validateAssistMemoryReference(ctx context.Context, orgID string, virployeeID uuid.UUID, reference memories.Reference) error {
	var valid bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM companion_memories
		WHERE org_id=$1 AND virployee_id=$2 AND id=$3
		  AND version=$4 AND content_hash=$5 AND title=$6 AND memory_type=$7 AND sensitivity=$8
		  AND scope_type=$9 AND subject_id=$10 AND case_id IS NOT DISTINCT FROM $11::uuid
		  AND lifecycle_state='active' AND review_state='approved' AND trust_score >= $12
		  AND sensitivity='normal' AND cardinality(poisoning_flags)=0
		  AND review_reason<>'conflicting_memory_requires_review'
		  AND (scope_type<>'virployee' OR memory_type='procedure')
		  AND (expires_at IS NULL OR expires_at>now())
	)`, orgID, virployeeID, reference.ID, reference.Version, reference.Hash,
		reference.Title, reference.Type, reference.Sensitivity, reference.ScopeType,
		reference.SubjectID, nullableMemoryCase(reference.CaseID), memories.RecallTrustFloor).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return domainerr.Conflict("Assist memory context changed after completion")
	}
	return nil
}

func nullableMemoryCase(id *uuid.UUID) any {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}
