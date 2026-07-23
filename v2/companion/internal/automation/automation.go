package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Watcher struct {
	ID              uuid.UUID  `json:"id"`
	OrgID           string     `json:"org_id"`
	ProductID       string     `json:"product_id"`
	Name            string     `json:"name"`
	Lifecycle       string     `json:"lifecycle"`
	Mode            string     `json:"mode"`
	ActiveVersionID *uuid.UUID `json:"active_version_id,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type VersionInput struct {
	TriggerType           string          `json:"trigger_type"`
	TriggerConfig         json.RawMessage `json:"trigger_config"`
	DetectorCapabilityKey string          `json:"detector_capability_key"`
	DetectorManifestHash  string          `json:"detector_manifest_hash"`
	DetectorArguments     json.RawMessage `json:"detector_arguments"`
	ActionCapabilityKey   string          `json:"action_capability_key"`
	ActionManifestHash    string          `json:"action_manifest_hash"`
}

type Version struct {
	ID             uuid.UUID `json:"id"`
	WatcherID      uuid.UUID `json:"watcher_id"`
	Version        int64     `json:"version"`
	DefinitionHash string    `json:"definition_hash"`
	VersionInput
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

type Proposal struct {
	OccurrenceKey string          `json:"occurrence_key"`
	SubjectID     string          `json:"subject_id"`
	CaseID        *uuid.UUID      `json:"case_id,omitempty"`
	ResourceType  string          `json:"resource_type"`
	ResourceID    string          `json:"resource_id"`
	Arguments     json.RawMessage `json:"arguments"`
}

type GatePort interface {
	Detect(context.Context, GateRequest) ([]Proposal, error)
	Invoke(context.Context, GateRequest, Proposal) (status, invocationID string, err error)
}

type GateRequest struct {
	OrgID, ActorID, ProductID, ProductSurface string
	VirployeeID                               uuid.UUID
	DetectorKey, DetectorHash                 string
	DetectorArguments                         json.RawMessage
	ActionKey, ActionHash                     string
	IdempotencyKey                            string
}

type Service struct {
	pool *pgxpool.Pool
	gate GatePort
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool, gate GatePort) *Service {
	return &Service{pool: pool, gate: gate, now: func() time.Time { return time.Now().UTC() }}
}

func require(org, actor, role string, mutation bool) error {
	if strings.TrimSpace(org) == "" || strings.TrimSpace(actor) == "" {
		return domainerr.Unauthorized("trusted organization and actor are required")
	}
	if mutation && role != "owner" && role != "admin" && role != "operator" {
		return domainerr.Forbidden("watcher administration permission is required")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, org, actor, role, productID, name, mode string) (Watcher, error) {
	if err := require(org, actor, role, true); err != nil {
		return Watcher{}, err
	}
	productID, name, mode = strings.TrimSpace(productID), strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(mode))
	if productID == "" || name == "" {
		return Watcher{}, domainerr.Validation("product_id and name are required")
	}
	if mode == "" {
		mode = "propose"
	}
	if mode != "observe" && mode != "propose" && mode != "execute_if_authorized" {
		return Watcher{}, domainerr.Validation("mode is invalid")
	}
	now := s.now()
	out := Watcher{ID: uuid.New(), OrgID: org, ProductID: productID, Name: name, Lifecycle: "draft", Mode: mode, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	_, err := s.pool.Exec(ctx, `INSERT INTO companion_business_watchers(id,org_id,product_id,name,lifecycle,mode,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'draft',$5,$6,$7,$7)`,
		out.ID, org, productID, name, mode, actor, now)
	return out, err
}

func (s *Service) List(ctx context.Context, org, actor string) ([]Watcher, error) {
	if err := require(org, actor, "", false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,org_id,product_id,name,lifecycle,mode,active_version_id,created_by,created_at,updated_at FROM companion_business_watchers WHERE org_id=$1 ORDER BY created_at DESC,id DESC`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watcher
	for rows.Next() {
		var item Watcher
		if err := rows.Scan(&item.ID, &item.OrgID, &item.ProductID, &item.Name, &item.Lifecycle, &item.Mode, &item.ActiveVersionID, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func normalizedJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return nil, domainerr.Validation("configuration must be an object")
	}
	return json.Marshal(object)
}

func hashDefinition(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Service) CreateVersion(ctx context.Context, org, actor, role string, watcherID uuid.UUID, in VersionInput) (Version, error) {
	if err := require(org, actor, role, true); err != nil {
		return Version{}, err
	}
	in.TriggerType = strings.ToLower(strings.TrimSpace(in.TriggerType))
	in.DetectorCapabilityKey = strings.ToLower(strings.TrimSpace(in.DetectorCapabilityKey))
	in.DetectorManifestHash = strings.ToLower(strings.TrimSpace(in.DetectorManifestHash))
	in.ActionCapabilityKey = strings.ToLower(strings.TrimSpace(in.ActionCapabilityKey))
	in.ActionManifestHash = strings.ToLower(strings.TrimSpace(in.ActionManifestHash))
	if in.TriggerType != "schedule" && in.TriggerType != "event" {
		return Version{}, domainerr.Validation("trigger_type must be schedule or event")
	}
	var err error
	if in.TriggerConfig, err = normalizedJSON(in.TriggerConfig); err != nil {
		return Version{}, err
	}
	if in.DetectorArguments, err = normalizedJSON(in.DetectorArguments); err != nil {
		return Version{}, err
	}
	var detectorEffect string
	err = s.pool.QueryRow(ctx, `SELECT side_effect_class FROM capabilities WHERE tenant_id=$1 AND capability_key=$2 AND manifest_hash=$3 AND conformed_hash=$3 AND promotion_state='active' AND archived_at IS NULL AND trashed_at IS NULL`,
		org, in.DetectorCapabilityKey, in.DetectorManifestHash).Scan(&detectorEffect)
	if err != nil || detectorEffect != "read" {
		return Version{}, domainerr.Conflict("detector must be an active conformant read capability with the exact manifest hash")
	}
	if in.ActionCapabilityKey != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM capabilities WHERE tenant_id=$1 AND capability_key=$2 AND manifest_hash=$3 AND conformed_hash=$3 AND promotion_state='active' AND archived_at IS NULL AND trashed_at IS NULL)`,
			org, in.ActionCapabilityKey, in.ActionManifestHash).Scan(&exists); err != nil || !exists {
			return Version{}, domainerr.Conflict("action capability must be active and conformant with the exact manifest hash")
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM companion_business_watchers WHERE org_id=$1 AND id=$2 FOR UPDATE`, org, watcherID).Scan(&exists); err != nil {
		return Version{}, mapNotFound(err, "watcher not found")
	}
	var version int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM companion_business_watcher_versions WHERE org_id=$1 AND watcher_id=$2`, org, watcherID).Scan(&version); err != nil {
		return Version{}, err
	}
	out := Version{ID: uuid.New(), WatcherID: watcherID, Version: version, VersionInput: in, CreatedBy: actor, CreatedAt: s.now()}
	out.DefinitionHash = hashDefinition(in)
	_, err = tx.Exec(ctx, `INSERT INTO companion_business_watcher_versions(id,org_id,watcher_id,version,trigger_type,trigger_config,detector_capability_key,detector_manifest_hash,detector_arguments,action_capability_key,action_manifest_hash,definition_hash,created_by,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, out.ID, org, watcherID, version, in.TriggerType, in.TriggerConfig, in.DetectorCapabilityKey, in.DetectorManifestHash, in.DetectorArguments, in.ActionCapabilityKey, in.ActionManifestHash, out.DefinitionHash, actor, out.CreatedAt)
	if err != nil {
		return Version{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Service) Activate(ctx context.Context, org, actor, role string, versionID uuid.UUID) (Watcher, error) {
	if err := require(org, actor, role, true); err != nil {
		return Watcher{}, err
	}
	var watcherID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT watcher_id FROM companion_business_watcher_versions WHERE org_id=$1 AND id=$2`, org, versionID).Scan(&watcherID); err != nil {
		return Watcher{}, mapNotFound(err, "watcher version not found")
	}
	_, err := s.pool.Exec(ctx, `UPDATE companion_business_watchers SET active_version_id=$3,lifecycle='paused',updated_at=now() WHERE org_id=$1 AND id=$2`, org, watcherID, versionID)
	if err != nil {
		return Watcher{}, err
	}
	return s.get(ctx, org, watcherID)
}

func (s *Service) Pause(ctx context.Context, org, actor, role string, watcherID uuid.UUID) error {
	if err := require(org, actor, role, true); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE companion_business_watchers SET lifecycle='paused',updated_at=now() WHERE org_id=$1 AND id=$2 AND lifecycle<>'archived'`, org, watcherID)
	if err == nil && tag.RowsAffected() == 0 {
		return domainerr.NotFound("watcher not found")
	}
	return err
}

func (s *Service) Run(ctx context.Context, org, actor, role string, watcherID, virployeeID uuid.UUID, triggerRef string, event json.RawMessage) (uuid.UUID, error) {
	if err := require(org, actor, "", false); err != nil {
		return uuid.Nil, err
	}
	if role != "owner" && role != "admin" && role != "operator" && role != "service" {
		return uuid.Nil, domainerr.Forbidden("watcher execution permission is required")
	}
	if s.gate == nil {
		return uuid.Nil, domainerr.Conflict("governed tool gate is not configured")
	}
	watcher, version, err := s.loadActive(ctx, org, watcherID)
	if err != nil {
		return uuid.Nil, err
	}
	if watcher.Lifecycle != "active" {
		return uuid.Nil, domainerr.Conflict("watcher is paused")
	}
	triggerHash := hashDefinition(map[string]any{"trigger_ref": triggerRef, "event": json.RawMessage(event)})
	runID, now := uuid.New(), s.now()
	if _, err := s.pool.Exec(ctx, `INSERT INTO companion_business_watcher_runs(id,org_id,watcher_id,watcher_version_id,trigger_type,trigger_ref_hash,status,started_at) VALUES($1,$2,$3,$4,$5,$6,'running',$7)`,
		runID, org, watcher.ID, version.ID, version.TriggerType, triggerHash, now); err != nil {
		return uuid.Nil, err
	}
	args := version.DetectorArguments
	if len(event) > 0 {
		var base map[string]any
		_ = json.Unmarshal(args, &base)
		var incoming any
		_ = json.Unmarshal(event, &incoming)
		base["event"] = incoming
		args, _ = json.Marshal(base)
	}
	request := GateRequest{OrgID: org, ActorID: actor, ProductID: watcher.ProductID, VirployeeID: virployeeID, DetectorKey: version.DetectorCapabilityKey, DetectorHash: version.DetectorManifestHash, DetectorArguments: args, ActionKey: version.ActionCapabilityKey, ActionHash: version.ActionManifestHash}
	if productUUID, parseErr := uuid.Parse(watcher.ProductID); parseErr == nil {
		_ = s.pool.QueryRow(ctx, `SELECT product_surface FROM companion_product_integrations WHERE org_id=$1 AND product_id=$2`, org, productUUID).Scan(&request.ProductSurface)
	}
	proposals, err := s.gate.Detect(ctx, request)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE companion_business_watcher_runs SET status='failed',error_code='detector_failed',completed_at=now() WHERE org_id=$1 AND id=$2`, org, runID)
		return runID, err
	}
	proposed, invoked := 0, 0
	for _, proposal := range proposals {
		if strings.TrimSpace(proposal.OccurrenceKey) == "" {
			continue
		}
		status, invocationID := "observed", ""
		if watcher.Mode == "propose" {
			status, proposed = "proposed", proposed+1
		}
		if watcher.Mode == "execute_if_authorized" && version.ActionCapabilityKey != "" {
			request.IdempotencyKey = watcher.ID.String() + ":" + proposal.OccurrenceKey
			status, invocationID, err = s.gate.Invoke(ctx, request, proposal)
			if err != nil {
				status = "blocked"
			} else {
				invoked++
			}
		}
		proposalRaw, _ := json.Marshal(proposal)
		_, insertErr := s.pool.Exec(ctx, `INSERT INTO companion_business_watcher_occurrences(id,org_id,product_id,watcher_id,watcher_version_id,run_id,occurrence_key,subject_id,case_id,resource_type,resource_id,proposal,status,invocation_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(org_id,watcher_id,occurrence_key) DO NOTHING`,
			uuid.New(), org, watcher.ProductID, watcher.ID, version.ID, runID, proposal.OccurrenceKey, proposal.SubjectID, proposal.CaseID, proposal.ResourceType, proposal.ResourceID, proposalRaw, status, invocationID)
		if insertErr != nil {
			return runID, insertErr
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE companion_business_watcher_runs SET status='done',detected_count=$3,proposed_count=$4,invoked_count=$5,completed_at=now() WHERE org_id=$1 AND id=$2`,
		org, runID, len(proposals), proposed, invoked)
	return runID, err
}

func (s *Service) SetActive(ctx context.Context, org, actor, role string, watcherID uuid.UUID) error {
	if err := require(org, actor, role, true); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE companion_business_watchers SET lifecycle='active',updated_at=now() WHERE org_id=$1 AND id=$2 AND active_version_id IS NOT NULL AND lifecycle<>'archived'`, org, watcherID)
	if err == nil && tag.RowsAffected() == 0 {
		return domainerr.Conflict("watcher must have an active version")
	}
	return err
}

func (s *Service) get(ctx context.Context, org string, id uuid.UUID) (Watcher, error) {
	var out Watcher
	err := s.pool.QueryRow(ctx, `SELECT id,org_id,product_id,name,lifecycle,mode,active_version_id,created_by,created_at,updated_at FROM companion_business_watchers WHERE org_id=$1 AND id=$2`, org, id).
		Scan(&out.ID, &out.OrgID, &out.ProductID, &out.Name, &out.Lifecycle, &out.Mode, &out.ActiveVersionID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt)
	return out, mapNotFound(err, "watcher not found")
}

func (s *Service) loadActive(ctx context.Context, org string, id uuid.UUID) (Watcher, Version, error) {
	watcher, err := s.get(ctx, org, id)
	if err != nil {
		return Watcher{}, Version{}, err
	}
	if watcher.ActiveVersionID == nil {
		return Watcher{}, Version{}, domainerr.Conflict("watcher has no active version")
	}
	var version Version
	err = s.pool.QueryRow(ctx, `SELECT id,watcher_id,version,trigger_type,trigger_config,detector_capability_key,detector_manifest_hash,detector_arguments,action_capability_key,action_manifest_hash,definition_hash,created_by,created_at FROM companion_business_watcher_versions WHERE org_id=$1 AND id=$2`,
		org, *watcher.ActiveVersionID).Scan(&version.ID, &version.WatcherID, &version.Version, &version.TriggerType, &version.TriggerConfig, &version.DetectorCapabilityKey, &version.DetectorManifestHash, &version.DetectorArguments, &version.ActionCapabilityKey, &version.ActionManifestHash, &version.DefinitionHash, &version.CreatedBy, &version.CreatedAt)
	return watcher, version, err
}

func mapNotFound(err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainerr.NotFound(message)
	}
	return err
}
