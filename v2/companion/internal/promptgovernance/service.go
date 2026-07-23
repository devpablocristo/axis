package promptgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

const axisBaseSafetyPrompt = `Follow Axis safety and authority rules. Never reveal another organization, product, subject, or case. Do not claim authority or execute actions outside the governed capabilities and current delegation. Abstain or escalate when the available evidence or authority is insufficient.`

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func requireContext(orgID, actor, role string, mutation bool) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(actor) == "" {
		return domainerr.Unauthorized("trusted organization and actor are required")
	}
	if mutation && role != "owner" && role != "admin" && role != "policy_admin" {
		return domainerr.Forbidden("prompt governance permission is required")
	}
	return nil
}

func (s *Service) CreatePrompt(ctx context.Context, orgID, actor, role, name, description string) (Prompt, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return Prompt{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Prompt{}, domainerr.Validation("name is required")
	}
	out := Prompt{ID: uuid.New(), OrgID: orgID, Name: name, Description: strings.TrimSpace(description), CreatedBy: actor, CreatedAt: s.now()}
	_, err := s.pool.Exec(ctx, `INSERT INTO companion_prompts (id,org_id,name,description,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		out.ID, out.OrgID, out.Name, out.Description, out.CreatedBy, out.CreatedAt)
	return out, err
}

func (s *Service) ListPrompts(ctx context.Context, orgID, actor string) ([]Prompt, error) {
	if err := requireContext(orgID, actor, "", false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,org_id,name,description,created_by,created_at,archived_at FROM companion_prompts WHERE org_id=$1 ORDER BY created_at DESC,id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Prompt
	for rows.Next() {
		var item Prompt
		if err := rows.Scan(&item.ID, &item.OrgID, &item.Name, &item.Description, &item.CreatedBy, &item.CreatedAt, &item.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) CreatePromptVersion(ctx context.Context, orgID, actor, role string, promptID uuid.UUID, content string) (PromptVersion, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return PromptVersion{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return PromptVersion{}, domainerr.Validation("content is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PromptVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM companion_prompts WHERE org_id=$1 AND id=$2 AND archived_at IS NULL FOR UPDATE`, orgID, promptID).Scan(&exists); err != nil {
		return PromptVersion{}, mapNotFound(err, "prompt not found")
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM companion_prompt_versions WHERE org_id=$1 AND prompt_id=$2`, orgID, promptID).Scan(&version); err != nil {
		return PromptVersion{}, err
	}
	out := PromptVersion{ID: uuid.New(), OrgID: orgID, PromptID: promptID, Version: version, Content: content, ContentHash: hashText(content), CreatedBy: actor, CreatedAt: s.now()}
	if _, err := tx.Exec(ctx, `INSERT INTO companion_prompt_versions (id,org_id,prompt_id,version,content,content_hash,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		out.ID, orgID, promptID, version, content, out.ContentHash, actor, out.CreatedAt); err != nil {
		return PromptVersion{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Service) Simulate(ctx context.Context, orgID, actor, role string, versionID uuid.UUID) (Simulation, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return Simulation{}, err
	}
	var content, contentHash string
	if err := s.pool.QueryRow(ctx, `SELECT content,content_hash FROM companion_prompt_versions WHERE org_id=$1 AND id=$2`, orgID, versionID).Scan(&content, &contentHash); err != nil {
		return Simulation{}, mapNotFound(err, "prompt version not found")
	}
	findings := []map[string]string{}
	for _, marker := range []string{"{{payload", "{{memory", "{{document", "{{conversation"} {
		if strings.Contains(strings.ToLower(content), marker) {
			findings = append(findings, map[string]string{"code": "unsafe_interpolation", "marker": marker})
		}
	}
	raw, _ := json.Marshal(findings)
	out := Simulation{ID: uuid.New(), PromptVersionID: versionID, ContentHash: contentHash, Passed: len(findings) == 0, Findings: findings, CreatedAt: s.now()}
	out.ResultHash = canonicalHash(map[string]any{"content_hash": contentHash, "findings": findings, "passed": out.Passed})
	_, err := s.pool.Exec(ctx, `INSERT INTO companion_prompt_simulations (id,org_id,prompt_version_id,content_hash,result_hash,passed,findings,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		out.ID, orgID, versionID, contentHash, out.ResultHash, out.Passed, raw, actor, out.CreatedAt)
	return out, err
}

func (s *Service) CreateSuite(ctx context.Context, orgID, actor, role, name, description, artifactType string) (EvaluationSuite, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return EvaluationSuite{}, err
	}
	artifactType = normalizeTarget(artifactType)
	if artifactType != "prompt_version" && artifactType != "capability_manifest" && artifactType != "virployee_snapshot" {
		return EvaluationSuite{}, domainerr.Validation("artifact_type is invalid")
	}
	if strings.TrimSpace(name) == "" {
		return EvaluationSuite{}, domainerr.Validation("name is required")
	}
	out := EvaluationSuite{ID: uuid.New(), OrgID: orgID, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), ArtifactType: artifactType, CreatedBy: actor, CreatedAt: s.now()}
	_, err := s.pool.Exec(ctx, `INSERT INTO companion_evaluation_suites (id,org_id,name,description,artifact_type,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		out.ID, orgID, out.Name, out.Description, artifactType, actor, out.CreatedAt)
	return out, err
}

func (s *Service) ListSuites(ctx context.Context, orgID, actor string) ([]EvaluationSuite, error) {
	if err := requireContext(orgID, actor, "", false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,org_id,name,description,artifact_type,created_by,created_at,archived_at FROM companion_evaluation_suites WHERE org_id=$1 ORDER BY created_at DESC,id DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvaluationSuite
	for rows.Next() {
		var item EvaluationSuite
		if err := rows.Scan(&item.ID, &item.OrgID, &item.Name, &item.Description, &item.ArtifactType, &item.CreatedBy, &item.CreatedAt, &item.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) CreateSuiteVersion(ctx context.Context, orgID, actor, role string, suiteID uuid.UUID, dataset, thresholds json.RawMessage) (SuiteVersion, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return SuiteVersion{}, err
	}
	var cases []map[string]any
	if len(dataset) == 0 || json.Unmarshal(dataset, &cases) != nil {
		return SuiteVersion{}, domainerr.Validation("dataset must be an array")
	}
	var limits map[string]any
	if len(thresholds) == 0 {
		thresholds = json.RawMessage(`{}`)
	}
	if json.Unmarshal(thresholds, &limits) != nil {
		return SuiteVersion{}, domainerr.Validation("thresholds must be an object")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SuiteVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM companion_evaluation_suites WHERE org_id=$1 AND id=$2 AND archived_at IS NULL FOR UPDATE`, orgID, suiteID).Scan(&exists); err != nil {
		return SuiteVersion{}, mapNotFound(err, "evaluation suite not found")
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM companion_evaluation_suite_versions WHERE org_id=$1 AND suite_id=$2`, orgID, suiteID).Scan(&version); err != nil {
		return SuiteVersion{}, err
	}
	out := SuiteVersion{ID: uuid.New(), SuiteID: suiteID, Version: version, Dataset: dataset, Thresholds: thresholds, SuiteHash: canonicalHash(map[string]any{"dataset": cases, "thresholds": limits}), CreatedBy: actor, CreatedAt: s.now()}
	if _, err := tx.Exec(ctx, `INSERT INTO companion_evaluation_suite_versions (id,org_id,suite_id,version,dataset,thresholds,suite_hash,created_by,created_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9 WHERE EXISTS (SELECT 1 FROM companion_evaluation_suites WHERE org_id=$2 AND id=$3 AND archived_at IS NULL)`,
		out.ID, orgID, suiteID, version, dataset, thresholds, out.SuiteHash, actor, out.CreatedAt); err != nil {
		return SuiteVersion{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Service) RunEvaluation(ctx context.Context, orgID, actor, role string, suiteVersionID uuid.UUID, artifactType, artifactRef, artifactHash, productID, snapshotHash string) (EvaluationRun, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return EvaluationRun{}, err
	}
	artifactType, artifactRef, artifactHash = normalizeTarget(artifactType), strings.TrimSpace(artifactRef), strings.ToLower(strings.TrimSpace(artifactHash))
	if artifactRef == "" || len(artifactHash) != 64 || len(snapshotHash) != 64 {
		return EvaluationRun{}, domainerr.Validation("artifact_ref and 64-character artifact_hash/snapshot_hash are required")
	}
	var dataset json.RawMessage
	var suiteArtifact string
	if err := s.pool.QueryRow(ctx, `SELECT sv.dataset,s.artifact_type FROM companion_evaluation_suite_versions sv JOIN companion_evaluation_suites s ON s.org_id=sv.org_id AND s.id=sv.suite_id WHERE sv.org_id=$1 AND sv.id=$2`, orgID, suiteVersionID).Scan(&dataset, &suiteArtifact); err != nil {
		return EvaluationRun{}, mapNotFound(err, "evaluation suite version not found")
	}
	if artifactType != suiteArtifact {
		return EvaluationRun{}, domainerr.Validation("suite artifact_type does not match")
	}
	var cases []map[string]any
	_ = json.Unmarshal(dataset, &cases)
	passedCount := 0
	for _, testCase := range cases {
		if fail, _ := testCase["simulate_failure"].(bool); !fail {
			passedCount++
		}
	}
	passed := len(cases) > 0 && passedCount == len(cases)
	metrics, _ := json.Marshal(map[string]any{"cases": len(cases), "passed": passedCount, "synthetic": true, "external_effects": 0})
	now := s.now()
	out := EvaluationRun{
		ID: uuid.New(), SuiteVersionID: suiteVersionID, ArtifactType: artifactType, ArtifactRef: artifactRef,
		ArtifactHash: artifactHash, ProductID: strings.TrimSpace(productID), SnapshotHash: strings.ToLower(snapshotHash),
		Status: "failed", Passed: passed, Metrics: metrics, CreatedBy: actor, StartedAt: now, CompletedAt: &now,
	}
	if passed {
		out.Status = "passed"
	}
	out.ReportHash = canonicalHash(map[string]any{"suite_version_id": suiteVersionID, "artifact_ref": artifactRef, "artifact_hash": artifactHash, "snapshot_hash": snapshotHash, "metrics": json.RawMessage(metrics), "passed": passed})
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EvaluationRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO companion_evaluation_runs (id,org_id,suite_version_id,artifact_type,artifact_ref,artifact_hash,product_id,snapshot_hash,status,passed,metrics,report_hash,created_by,started_at,completed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		out.ID, orgID, suiteVersionID, artifactType, artifactRef, artifactHash, out.ProductID, out.SnapshotHash, out.Status, out.Passed, metrics, out.ReportHash, actor, now, now); err != nil {
		return EvaluationRun{}, err
	}
	for index, testCase := range cases {
		key, _ := testCase["key"].(string)
		if key == "" {
			key = fmt.Sprintf("case-%d", index+1)
		}
		checkType, _ := testCase["check_type"].(string)
		if checkType == "" {
			checkType = "behavior"
		}
		casePassed := true
		if fail, _ := testCase["simulate_failure"].(bool); fail {
			casePassed = false
		}
		meta, _ := json.Marshal(map[string]any{"synthetic": true, "external_effects": 0})
		if _, err := tx.Exec(ctx, `INSERT INTO companion_evaluation_results (id,org_id,run_id,case_key,check_type,passed,result_hash,error_code,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			uuid.New(), orgID, out.ID, key, checkType, casePassed, canonicalHash(map[string]any{"key": key, "passed": casePassed}), func() string {
				if casePassed {
					return ""
				}
				return "synthetic_assertion_failed"
			}(), meta); err != nil {
			return EvaluationRun{}, err
		}
	}
	return out, tx.Commit(ctx)
}

func (s *Service) GetEvaluationRun(ctx context.Context, orgID, actor string, id uuid.UUID) (EvaluationRun, error) {
	if err := requireContext(orgID, actor, "", false); err != nil {
		return EvaluationRun{}, err
	}
	var out EvaluationRun
	err := s.pool.QueryRow(ctx, `SELECT id,suite_version_id,artifact_type,artifact_ref,artifact_hash,product_id,snapshot_hash,status,passed,metrics,report_hash,created_by,started_at,completed_at FROM companion_evaluation_runs WHERE org_id=$1 AND id=$2`, orgID, id).
		Scan(&out.ID, &out.SuiteVersionID, &out.ArtifactType, &out.ArtifactRef, &out.ArtifactHash, &out.ProductID, &out.SnapshotHash, &out.Status, &out.Passed, &out.Metrics, &out.ReportHash, &out.CreatedBy, &out.StartedAt, &out.CompletedAt)
	return out, mapNotFound(err, "evaluation run not found")
}

func (s *Service) ListEvaluationRuns(ctx context.Context, orgID, actor string) ([]EvaluationRun, error) {
	if err := requireContext(orgID, actor, "", false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,suite_version_id,artifact_type,artifact_ref,artifact_hash,product_id,snapshot_hash,status,passed,metrics,report_hash,created_by,started_at,completed_at FROM companion_evaluation_runs WHERE org_id=$1 ORDER BY started_at DESC,id DESC LIMIT 200`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvaluationRun
	for rows.Next() {
		var item EvaluationRun
		if err := rows.Scan(&item.ID, &item.SuiteVersionID, &item.ArtifactType, &item.ArtifactRef, &item.ArtifactHash, &item.ProductID, &item.SnapshotHash, &item.Status, &item.Passed, &item.Metrics, &item.ReportHash, &item.CreatedBy, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) Promote(ctx context.Context, orgID, actor, role, targetType string, targetID, promptVersionID, evaluationRunID uuid.UUID, productID, authorizationHash, action string) (Binding, error) {
	if err := requireContext(orgID, actor, role, true); err != nil {
		return Binding{}, err
	}
	targetType, action = normalizeTarget(targetType), normalizeTarget(action)
	if targetType != "job_role" && targetType != "profile_template" && targetType != "virployee" {
		return Binding{}, domainerr.Validation("target_type is invalid")
	}
	if action != "promote" && action != "rollback" {
		return Binding{}, domainerr.Validation("action must be promote or rollback")
	}
	if len(strings.TrimSpace(authorizationHash)) != 64 {
		return Binding{}, domainerr.Validation("Nexus authorization_hash is required")
	}
	var contentHash, versionCreator string
	if err := s.pool.QueryRow(ctx, `SELECT content_hash,created_by FROM companion_prompt_versions WHERE org_id=$1 AND id=$2`, orgID, promptVersionID).Scan(&contentHash, &versionCreator); err != nil {
		return Binding{}, mapNotFound(err, "prompt version not found")
	}
	var evalCreator string
	var completedAt time.Time
	var passed bool
	err := s.pool.QueryRow(ctx, `SELECT created_by,completed_at,passed FROM companion_evaluation_runs WHERE org_id=$1 AND id=$2 AND artifact_type='prompt_version' AND artifact_ref=$3 AND artifact_hash=$4 AND product_id=$5`,
		orgID, evaluationRunID, promptVersionID.String(), contentHash, strings.TrimSpace(productID)).Scan(&evalCreator, &completedAt, &passed)
	if err != nil || !passed || s.now().Sub(completedAt) > evaluationFreshness {
		return Binding{}, domainerr.Conflict("a passing evaluation for the exact prompt, product and hash from the last 24 hours is required")
	}
	if actor == versionCreator || actor == evalCreator {
		return Binding{}, domainerr.Forbidden("promotion requires an independent approver")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Binding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previous *uuid.UUID
	var binding Binding
	err = tx.QueryRow(ctx, `SELECT id,prompt_version_id,revision FROM companion_prompt_bindings WHERE org_id=$1 AND target_type=$2 AND target_id=$3 AND product_id=$4 FOR UPDATE`,
		orgID, targetType, targetID, strings.TrimSpace(productID)).Scan(&binding.ID, &previous, &binding.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		binding.ID, binding.Revision = uuid.New(), 0
	} else if err != nil {
		return Binding{}, err
	}
	binding.TargetType, binding.TargetID, binding.ProductID = targetType, targetID, strings.TrimSpace(productID)
	binding.PromptVersionID, binding.Revision, binding.EvaluationRunID = promptVersionID, binding.Revision+1, &evaluationRunID
	binding.AuthorizationHash, binding.PromotedBy, binding.PromotedAt = strings.ToLower(authorizationHash), actor, s.now()
	_, err = tx.Exec(ctx, `INSERT INTO companion_prompt_bindings (id,org_id,target_type,target_id,product_id,prompt_version_id,revision,evaluation_run_id,authorization_hash,promoted_by,promoted_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (org_id,target_type,target_id,product_id) DO UPDATE SET prompt_version_id=EXCLUDED.prompt_version_id,revision=companion_prompt_bindings.revision+1,evaluation_run_id=EXCLUDED.evaluation_run_id,authorization_hash=EXCLUDED.authorization_hash,promoted_by=EXCLUDED.promoted_by,promoted_at=EXCLUDED.promoted_at`,
		binding.ID, orgID, targetType, targetID, binding.ProductID, promptVersionID, binding.Revision, evaluationRunID, binding.AuthorizationHash, actor, binding.PromotedAt)
	if err != nil {
		return Binding{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO companion_prompt_binding_events (id,org_id,binding_id,action,previous_version_id,new_version_id,product_id,evaluation_run_id,authorization_hash,actor_id,binding_revision,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		uuid.New(), orgID, binding.ID, action, previous, promptVersionID, binding.ProductID, evaluationRunID, binding.AuthorizationHash, actor, binding.Revision, binding.PromotedAt)
	if err != nil {
		return Binding{}, err
	}
	return binding, tx.Commit(ctx)
}

func (s *Service) Resolve(ctx context.Context, orgID, actor string, virployeeID uuid.UUID, productID string, includeContent bool) (Resolution, error) {
	if err := requireContext(orgID, actor, "", false); err != nil {
		return Resolution{}, err
	}
	var jobRoleID, profileID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT job_role_id,profile_template_id FROM virployees WHERE tenant_id=$1 AND id=$2 AND lifecycle='active'`, orgID, virployeeID).Scan(&jobRoleID, &profileID); err != nil {
		return Resolution{}, mapNotFound(err, "virployee not found")
	}
	targets := []struct {
		level string
		id    uuid.UUID
	}{{"job_role", jobRoleID}, {"profile_template", profileID}, {"virployee", virployeeID}}
	out := Resolution{OrgID: orgID, ProductID: strings.TrimSpace(productID), VirployeeID: virployeeID}
	base := ResolvedPrompt{Level: "axis_base", Version: 1, ContentHash: hashText(axisBaseSafetyPrompt)}
	if includeContent {
		base.Content = axisBaseSafetyPrompt
	}
	out.ResolvedVersions = append(out.ResolvedVersions, base)
	for _, target := range targets {
		var item ResolvedPrompt
		var evaluationID *uuid.UUID
		err := s.pool.QueryRow(ctx, `SELECT b.product_id,pv.id,pv.version,pv.content_hash,pv.content,b.evaluation_run_id
			FROM companion_prompt_bindings b JOIN companion_prompt_versions pv ON pv.org_id=b.org_id AND pv.id=b.prompt_version_id
			WHERE b.org_id=$1 AND b.target_type=$2 AND b.target_id=$3 AND b.product_id IN ('',$4)
			ORDER BY CASE WHEN b.product_id=$4 AND $4<>'' THEN 0 ELSE 1 END LIMIT 1`,
			orgID, target.level, target.id, out.ProductID).Scan(&item.ProductID, &item.VersionID, &item.Version, &item.ContentHash, &item.Content, &evaluationID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Resolution{}, err
		}
		item.Level, item.TargetID = target.level, target.id
		if !includeContent {
			item.Content = ""
		}
		if evaluationID == nil {
			out.EvaluationUnknown = true
		}
		out.ResolvedVersions = append(out.ResolvedVersions, item)
	}
	var parts []string
	for _, item := range out.ResolvedVersions {
		parts = append(parts, item.Level+":"+item.VersionID.String()+":"+item.ContentHash)
		if includeContent && item.Content != "" {
			out.EffectiveContent += item.Content + "\n\n"
		}
	}
	out.EffectiveContent = strings.TrimSpace(out.EffectiveContent)
	out.PromptBundleHash = hashText(strings.Join(parts, "\n"))
	return out, nil
}

func mapNotFound(err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound(message)
	}
	return err
}
