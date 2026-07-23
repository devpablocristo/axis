package productintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const validationFreshness = 24 * time.Hour

var supportedAPIs = map[string]string{
	"action-types":        "v1",
	"approvals":           "v1",
	"audit":               "v1",
	"authorization":       "v1",
	"evidence":            "v1",
	"governance":          "v1",
	"governance-policies": "v1",
	"operations":          "v1",
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func requireTrusted(orgID, actor string) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(actor) == "" {
		return domainerr.Validation("trusted organization and actor are required")
	}
	return nil
}

func requireManager(role string) error {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin":
		return nil
	default:
		return domainerr.Forbidden("organization owner or admin is required")
	}
}

func (s *Service) CreateVersion(
	ctx context.Context,
	orgID, actor, role string,
	productID uuid.UUID,
	productSurface string,
	input CreateVersionInput,
) (Version, bool, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return Version{}, false, err
	}
	if err := requireManager(role); err != nil {
		return Version{}, false, err
	}
	productSurface = strings.ToLower(strings.TrimSpace(productSurface))
	if productSurface == "" {
		productSurface = strings.ToLower(strings.TrimSpace(input.ProductSurface))
	}
	if !codePattern.MatchString(productSurface) {
		return Version{}, false, domainerr.Validation("trusted product surface is invalid")
	}
	if input.SourceIntegrationID == uuid.Nil || input.SourceVersionID == uuid.Nil || input.Version < 1 || !validContentHash(input.ContractHash) {
		return Version{}, false, domainerr.Validation("source integration binding is invalid")
	}
	section, err := normalizeSection(input.Section)
	if err != nil {
		return Version{}, false, err
	}
	contentHash, err := sectionHash(section)
	if err != nil {
		return Version{}, false, err
	}
	sectionJSON, err := json.Marshal(section)
	if err != nil {
		return Version{}, false, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Version{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	integrationID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO nexus_product_integrations(id,org_id,product_id,product_surface)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(org_id,product_id) DO UPDATE
		SET product_surface=EXCLUDED.product_surface,updated_at=now()
		RETURNING id
	`, integrationID, orgID, productID, productSurface).Scan(&integrationID)
	if err != nil {
		return Version{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM nexus_product_integrations WHERE id=$1 FOR UPDATE`, integrationID).Scan(&integrationID); err != nil {
		return Version{}, false, err
	}

	existing, getErr := getVersionBySource(ctx, tx, orgID, productID, input.SourceVersionID)
	if getErr == nil {
		return existing, false, tx.Commit(ctx)
	}
	if !errors.Is(getErr, pgx.ErrNoRows) {
		return Version{}, false, getErr
	}

	var revision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(revision),0)+1
		FROM nexus_product_integration_versions
		WHERE integration_id=$1
	`, integrationID).Scan(&revision); err != nil {
		return Version{}, false, err
	}
	versionID := uuid.New()
	var out Version
	var raw json.RawMessage
	err = tx.QueryRow(ctx, `
		INSERT INTO nexus_product_integration_versions(
			id,integration_id,revision,source_integration_id,source_version_id,source_revision,
			contract_hash,schema_version,section_json,content_hash,created_by
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id,integration_id,revision,source_integration_id,source_version_id,source_revision,
			contract_hash,schema_version,section_json,content_hash,status,created_by,created_at,activated_by,activated_at
	`, versionID, integrationID, revision, input.SourceIntegrationID, input.SourceVersionID, input.Version,
		strings.ToLower(input.ContractHash), section.SchemaVersion, sectionJSON, contentHash, actor).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SourceIntegrationID, &out.SourceVersionID,
		&out.SourceRevision, &out.ContractHash, &out.SchemaVersion, &raw, &out.ContentHash,
		&out.Status, &out.CreatedBy, &out.CreatedAt, &out.ActivatedBy, &out.ActivatedAt,
	)
	if err != nil {
		return Version{}, false, err
	}
	if err := json.Unmarshal(raw, &out.Section); err != nil {
		return Version{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, false, err
	}
	return out, true, nil
}

func (s *Service) GetIntegration(ctx context.Context, orgID, actor string, productID uuid.UUID) (Integration, *Version, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return Integration{}, nil, err
	}
	var out Integration
	err := s.pool.QueryRow(ctx, `
		SELECT id,org_id,product_id,product_surface,lifecycle,active_version_id,created_at,updated_at
		FROM nexus_product_integrations
		WHERE org_id=$1 AND product_id=$2
	`, orgID, productID).Scan(
		&out.ID, &out.OrgID, &out.ProductID, &out.ProductSurface, &out.Lifecycle,
		&out.ActiveVersionID, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, nil, domainerr.NotFound("product integration not found")
	}
	if err != nil {
		return Integration{}, nil, err
	}
	if out.ActiveVersionID == nil {
		return out, nil, nil
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, *out.ActiveVersionID)
	return out, &version, err
}

func (s *Service) ValidateVersion(
	ctx context.Context,
	orgID, actor, role string,
	versionID uuid.UUID,
) (ValidationReport, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return ValidationReport{}, err
	}
	if err := requireManager(role); err != nil {
		return ValidationReport{}, err
	}
	version, productID, err := getVersionForOrg(ctx, s.pool, orgID, versionID)
	if err != nil {
		return ValidationReport{}, err
	}
	checks := make([]ValidationCheck, 0, 3+len(version.Section.APIContracts)+len(version.Section.ActionTypes))
	valid := true
	add := func(code string, ok bool, message string) {
		status := "pass"
		if !ok {
			status = "fail"
			valid = false
		}
		checks = append(checks, ValidationCheck{Code: code, Status: status, Message: message})
	}
	add("schema_version", version.SchemaVersion == SchemaVersion, "")
	add("contract_hash", validContentHash(version.ContractHash), "")
	for _, contract := range version.Section.APIContracts {
		supported, exists := supportedAPIs[contract.Name]
		add("api."+contract.Name, exists && supported == contract.Version, "requested API contract is not supported")
	}
	for _, ref := range version.Section.ActionTypes {
		var enabled bool
		queryErr := s.pool.QueryRow(ctx, `
			SELECT enabled FROM action_types WHERE org_id=$1 AND action_type_key=$2
		`, orgID, ref.Key).Scan(&enabled)
		add("action_type."+ref.Key, queryErr == nil && enabled, "action type is missing or disabled")
	}
	checksJSON, err := json.Marshal(checks)
	if err != nil {
		return ValidationReport{}, err
	}
	report := ValidationReport{
		ID: uuid.New(), ProductID: productID, VersionID: version.ID, ContentHash: version.ContentHash,
		Valid: valid, Checks: checks, CreatedBy: actor, CreatedAt: s.now(),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO nexus_product_integration_validation_reports(
			id,org_id,product_id,version_id,content_hash,valid,checks_json,created_by,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(version_id,content_hash) DO UPDATE
		SET valid=EXCLUDED.valid,checks_json=EXCLUDED.checks_json,created_by=EXCLUDED.created_by,created_at=EXCLUDED.created_at
		RETURNING id,created_at
	`, report.ID, orgID, productID, report.VersionID, report.ContentHash, report.Valid, checksJSON, actor, report.CreatedAt).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		return ValidationReport{}, err
	}
	if valid {
		_, err = s.pool.Exec(ctx, `
			UPDATE nexus_product_integration_versions
			SET status='validated'
			WHERE id=$1 AND status='draft'
		`, versionID)
	}
	return report, err
}

func (s *Service) ActivateVersion(
	ctx context.Context,
	orgID, actor, role string,
	versionID uuid.UUID,
) (Readiness, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return Readiness{}, err
	}
	if err := requireManager(role); err != nil {
		return Readiness{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Readiness{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	version, productID, err := getVersionForOrg(ctx, tx, orgID, versionID)
	if err != nil {
		return Readiness{}, err
	}
	var valid bool
	var reportHash string
	var reportAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT valid,content_hash,created_at
		FROM nexus_product_integration_validation_reports
		WHERE version_id=$1
		ORDER BY created_at DESC LIMIT 1
	`, versionID).Scan(&valid, &reportHash, &reportAt)
	if errors.Is(err, pgx.ErrNoRows) || !valid || reportHash != version.ContentHash || s.now().Sub(reportAt) > validationFreshness {
		return Readiness{}, domainerr.Conflict("a fresh successful validation report is required")
	}
	if err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE nexus_product_integration_versions
		SET status='retired'
		WHERE integration_id=$1 AND status='active' AND id<>$2
	`, version.IntegrationID, version.ID); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE nexus_product_integration_versions
		SET status='active',activated_by=$2,activated_at=$3
		WHERE id=$1
	`, version.ID, actor, s.now()); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE nexus_product_integrations
		SET lifecycle='active',active_version_id=$2,updated_at=$3
		WHERE id=$1
	`, version.IntegrationID, version.ID, s.now()); err != nil {
		return Readiness{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Readiness{}, err
	}
	return s.Readiness(ctx, orgID, actor, productID)
}

func (s *Service) Suspend(ctx context.Context, orgID, actor, role string, productID uuid.UUID) (Integration, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return Integration{}, err
	}
	if err := requireManager(role); err != nil {
		return Integration{}, err
	}
	var out Integration
	err := s.pool.QueryRow(ctx, `
		UPDATE nexus_product_integrations
		SET lifecycle='suspended',updated_at=$3
		WHERE org_id=$1 AND product_id=$2 AND lifecycle<>'retired'
		RETURNING id,org_id,product_id,product_surface,lifecycle,active_version_id,created_at,updated_at
	`, orgID, productID, s.now()).Scan(
		&out.ID, &out.OrgID, &out.ProductID, &out.ProductSurface, &out.Lifecycle,
		&out.ActiveVersionID, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, domainerr.NotFound("product integration not found")
	}
	return out, err
}

func (s *Service) Readiness(ctx context.Context, orgID, actor string, productID uuid.UUID) (Readiness, error) {
	integration, version, err := s.GetIntegration(ctx, orgID, actor, productID)
	if err != nil {
		return Readiness{}, err
	}
	out := Readiness{
		Service: "nexus", ProductID: productID, ProductSurface: integration.ProductSurface,
		Lifecycle: integration.Lifecycle, Status: "blocked", CheckedAt: s.now(),
	}
	if integration.Lifecycle == "suspended" || integration.Lifecycle == "retired" {
		out.Status = "inactive"
		return out, nil
	}
	if version == nil {
		out.Checks = append(out.Checks, ValidationCheck{Code: "active_version", Status: "fail", Message: "no active version"})
		return out, nil
	}
	out.ActiveRevision = version.SourceRevision
	out.ActiveContentHash = version.ContractHash
	report, reportErr := s.currentValidation(ctx, orgID, version.ID)
	if reportErr != nil || !report.Valid || report.ContentHash != version.ContentHash {
		out.Checks = append(out.Checks, ValidationCheck{Code: "validation", Status: "fail", Message: "active validation is missing or stale"})
		return out, nil
	}
	out.Checks = append(out.Checks, report.Checks...)
	for _, ref := range version.Section.ActionTypes {
		var enabled bool
		if err := s.pool.QueryRow(ctx, `SELECT enabled FROM action_types WHERE org_id=$1 AND action_type_key=$2`, orgID, ref.Key).Scan(&enabled); err != nil || !enabled {
			out.Checks = append(out.Checks, ValidationCheck{Code: "action_type." + ref.Key, Status: "fail", Message: "action type drift"})
			return out, nil
		}
	}
	out.Status = "ready"
	return out, nil
}

func (s *Service) ValidateRuntimeContext(ctx context.Context, runtime RuntimeContext, actionType string) error {
	if strings.TrimSpace(runtime.OrgID) == "" || runtime.ProductID == uuid.Nil || runtime.IntegrationID == uuid.Nil ||
		runtime.IntegrationRevision < 1 || !validContentHash(runtime.IntegrationHash) {
		return domainerr.Forbidden("trusted product integration context is incomplete")
	}
	runtime.AccessMode = canonicalAccessMode(runtime.AccessMode)
	if runtime.AccessMode == "" {
		runtime.AccessMode = AccessModeDirect
	}
	var sectionJSON json.RawMessage
	var lifecycle, contractHash string
	var sourceIntegrationID uuid.UUID
	var sourceRevision int64
	err := s.pool.QueryRow(ctx, `
		SELECT i.lifecycle,v.source_integration_id,v.source_revision,v.contract_hash,v.section_json
		FROM nexus_product_integrations i
		JOIN nexus_product_integration_versions v ON v.id=i.active_version_id
		WHERE i.org_id=$1 AND i.product_id=$2
	`, runtime.OrgID, runtime.ProductID).Scan(&lifecycle, &sourceIntegrationID, &sourceRevision, &contractHash, &sectionJSON)
	if err != nil || lifecycle != "active" || sourceIntegrationID != runtime.IntegrationID ||
		sourceRevision != runtime.IntegrationRevision || contractHash != strings.ToLower(runtime.IntegrationHash) {
		return domainerr.Forbidden("product integration is inactive or does not match the active binding")
	}
	var section Section
	if err := json.Unmarshal(sectionJSON, &section); err != nil {
		return domainerr.Forbidden("product integration section is invalid")
	}
	if !accessModeAllowed(section.AccessModes, runtime.AccessMode) {
		return domainerr.Forbidden("product integration access mode is not authorized")
	}
	actionType = strings.ToLower(strings.TrimSpace(actionType))
	if actionType != "" && !actionAllowed(section.ActionTypes, actionType) {
		return domainerr.Forbidden("action type is not authorized by the product integration")
	}
	return nil
}

func (s *Service) RecordObservation(ctx context.Context, in Observation) error {
	if in.ProductID == uuid.Nil || strings.TrimSpace(in.OrgID) == "" || strings.TrimSpace(in.Area) == "" {
		return nil
	}
	if in.ObservedAt.IsZero() {
		in.ObservedAt = s.now()
	}
	in.AccessMode = canonicalAccessMode(in.AccessMode)
	if in.AccessMode == "" {
		in.AccessMode = AccessModeDirect
	}
	bucket := in.ObservedAt.UTC().Truncate(time.Hour)
	latencyMS := int(in.Latency.Milliseconds())
	if latencyMS < 0 {
		latencyMS = 0
	}
	succeeded := in.StatusCode >= 200 && in.StatusCode < 400
	denied := in.StatusCode == 401 || in.StatusCode == 403
	failed := in.StatusCode >= 500
	_, err := s.pool.Exec(ctx, `
		INSERT INTO nexus_product_service_activity(
			org_id,product_id,product_surface,integration_id,integration_revision,integration_hash,
			area,access_mode,bucket_start,request_count,success_count,denied_count,failure_count,
			latency_samples_ms,last_seen_at,last_success_at,last_error_code,last_error_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,$11,$12,ARRAY[$13]::integer[],$14,$15,$16,$17)
		ON CONFLICT(org_id,product_id,area,access_mode,bucket_start) DO UPDATE SET
			request_count=nexus_product_service_activity.request_count+1,
			success_count=nexus_product_service_activity.success_count+EXCLUDED.success_count,
			denied_count=nexus_product_service_activity.denied_count+EXCLUDED.denied_count,
			failure_count=nexus_product_service_activity.failure_count+EXCLUDED.failure_count,
			latency_samples_ms=(
				nexus_product_service_activity.latency_samples_ms || EXCLUDED.latency_samples_ms
			)[GREATEST(1,array_length(nexus_product_service_activity.latency_samples_ms || EXCLUDED.latency_samples_ms,1)-255):],
			last_seen_at=EXCLUDED.last_seen_at,
			last_success_at=COALESCE(EXCLUDED.last_success_at,nexus_product_service_activity.last_success_at),
			last_error_code=COALESCE(EXCLUDED.last_error_code,nexus_product_service_activity.last_error_code),
			last_error_at=COALESCE(EXCLUDED.last_error_at,nexus_product_service_activity.last_error_at),
			integration_id=EXCLUDED.integration_id,
			integration_revision=EXCLUDED.integration_revision,
			integration_hash=EXCLUDED.integration_hash,
			product_surface=EXCLUDED.product_surface
	`, in.OrgID, in.ProductID, in.ProductSurface, nullableUUID(in.IntegrationID), nullableRevision(in.IntegrationRevision),
		nullableHash(in.IntegrationHash), strings.ToLower(in.Area), in.AccessMode, bucket, boolInt(succeeded),
		boolInt(denied), boolInt(failed), latencyMS, in.ObservedAt, nullableTime(succeeded, in.ObservedAt),
		nullableString(in.ErrorCode, denied || failed), nullableTime(denied || failed, in.ObservedAt))
	return err
}

func (s *Service) ListServedProducts(ctx context.Context, orgID, actor, productFilter string, window time.Duration) ([]ServedProductStatus, error) {
	if err := requireTrusted(orgID, actor); err != nil {
		return nil, err
	}
	if window <= 0 || window > 31*24*time.Hour {
		window = 24 * time.Hour
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.id,i.product_id,i.product_surface,i.lifecycle,
			COALESCE(v.source_revision,0),COALESCE(v.contract_hash,''),COALESCE(v.section_json,'{}'::jsonb)
		FROM nexus_product_integrations i
		LEFT JOIN nexus_product_integration_versions v ON v.id=i.active_version_id
		WHERE i.org_id=$1 AND ($2='' OR i.product_id::text=$2 OR i.product_surface=$2)
		ORDER BY i.product_surface,i.product_id
	`, orgID, strings.TrimSpace(productFilter))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ServedProductStatus, 0)
	for rows.Next() {
		var integrationID, productID uuid.UUID
		var surface, lifecycle, contractHash string
		var revision int64
		var raw json.RawMessage
		if err := rows.Scan(&integrationID, &productID, &surface, &lifecycle, &revision, &contractHash, &raw); err != nil {
			return nil, err
		}
		var section Section
		_ = json.Unmarshal(raw, &section)
		configured := configuredAreas(section)
		observed, err := s.activityForProduct(ctx, orgID, productID, s.now().Add(-window))
		if err != nil {
			return nil, err
		}
		keys := make(map[string]struct{}, len(configured)+len(observed))
		for _, item := range configured {
			keys[item.area+"|"+item.mode] = struct{}{}
		}
		for key := range observed {
			keys[key] = struct{}{}
		}
		sortedKeys := make([]string, 0, len(keys))
		for key := range keys {
			sortedKeys = append(sortedKeys, key)
		}
		sort.Strings(sortedKeys)
		for _, key := range sortedKeys {
			parts := strings.SplitN(key, "|", 2)
			item := ServedProductStatus{
				Service: "nexus", OrgID: orgID, ProductID: productID, ProductSurface: surface,
				IntegrationID: integrationID, IntegrationRevision: revision, IntegrationHash: contractHash,
				Area: parts[0], AccessMode: parts[1], Lifecycle: lifecycle,
				Configured: containsConfigured(configured, parts[0], parts[1]), Status: "idle",
			}
			if lifecycle != "active" {
				item.Status = "inactive"
			}
			if activity, ok := observed[key]; ok {
				item.Observed = true
				item.Requests, item.Succeeded, item.Denied, item.Failed = activity.requests, activity.succeeded, activity.denied, activity.failed
				item.LastSeenAt, item.LastSuccessAt, item.LastErrorCode = activity.lastSeen, activity.lastSuccess, activity.lastError
				if activity.requests > 0 {
					rate := float64(activity.succeeded) / float64(activity.requests)
					item.SuccessRate = &rate
				}
				if len(activity.latencies) > 0 {
					sort.Ints(activity.latencies)
					index := int(math.Ceil(float64(len(activity.latencies))*0.95)) - 1
					p95 := float64(activity.latencies[max(0, index)])
					item.P95LatencyMS = &p95
				}
				if lifecycle == "active" {
					item.Status = "serving"
					if activity.failed > 0 && float64(activity.failed)/float64(max64(1, activity.requests)) >= 0.05 {
						item.Status = "degraded"
					}
				}
			}
			if lifecycle == "active" && !item.Configured {
				item.Status = "blocked"
			}
			out = append(out, item)
		}
	}
	return out, rows.Err()
}

type configuredArea struct{ area, mode string }

func configuredAreas(section Section) []configuredArea {
	out := make([]configuredArea, 0, len(section.APIContracts)*len(section.AccessModes))
	for _, contract := range section.APIContracts {
		area := contract.Name
		switch {
		case strings.Contains(area, "author"):
			area = "authorization"
		case strings.Contains(area, "polic"), strings.Contains(area, "govern"):
			area = "policy_evaluation"
		case strings.Contains(area, "approval"):
			area = "approval"
		case strings.Contains(area, "audit"), strings.Contains(area, "evidence"):
			area = "audit_evidence"
		case strings.Contains(area, "operation"):
			area = "incidents_operations"
		}
		for _, mode := range section.AccessModes {
			out = append(out, configuredArea{area: area, mode: canonicalAccessMode(mode)})
		}
	}
	return out
}

type activityAggregate struct {
	requests, succeeded, denied, failed int64
	latencies                           []int
	lastSeen, lastSuccess               *time.Time
	lastError                           string
}

func (s *Service) activityForProduct(ctx context.Context, orgID string, productID uuid.UUID, since time.Time) (map[string]activityAggregate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT area,access_mode,request_count,success_count,denied_count,failure_count,
			latency_samples_ms,last_seen_at,last_success_at,last_error_code
		FROM nexus_product_service_activity
		WHERE org_id=$1 AND product_id=$2 AND bucket_start >= $3
	`, orgID, productID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]activityAggregate)
	for rows.Next() {
		var area, mode, lastError string
		var requests, succeeded, denied, failed int64
		var latencies []int
		var lastSeen time.Time
		var lastSuccess *time.Time
		if err := rows.Scan(&area, &mode, &requests, &succeeded, &denied, &failed, &latencies, &lastSeen, &lastSuccess, &lastError); err != nil {
			return nil, err
		}
		mode = canonicalAccessMode(mode)
		key := area + "|" + mode
		current := out[key]
		current.requests += requests
		current.succeeded += succeeded
		current.denied += denied
		current.failed += failed
		current.latencies = append(current.latencies, latencies...)
		if current.lastSeen == nil || lastSeen.After(*current.lastSeen) {
			value := lastSeen
			current.lastSeen = &value
			current.lastError = lastError
		}
		if lastSuccess != nil && (current.lastSuccess == nil || lastSuccess.After(*current.lastSuccess)) {
			value := *lastSuccess
			current.lastSuccess = &value
		}
		out[key] = current
	}
	return out, rows.Err()
}

func (s *Service) currentValidation(ctx context.Context, orgID string, versionID uuid.UUID) (ValidationReport, error) {
	var out ValidationReport
	var checksJSON json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT id,product_id,version_id,content_hash,valid,checks_json,created_by,created_at
		FROM nexus_product_integration_validation_reports
		WHERE org_id=$1 AND version_id=$2
		ORDER BY created_at DESC LIMIT 1
	`, orgID, versionID).Scan(&out.ID, &out.ProductID, &out.VersionID, &out.ContentHash, &out.Valid, &checksJSON, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return ValidationReport{}, err
	}
	err = json.Unmarshal(checksJSON, &out.Checks)
	return out, err
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getVersionForOrg(ctx context.Context, q queryRower, orgID string, versionID uuid.UUID) (Version, uuid.UUID, error) {
	var out Version
	var productID uuid.UUID
	var raw json.RawMessage
	err := q.QueryRow(ctx, `
		SELECT v.id,v.integration_id,v.revision,v.source_integration_id,v.source_version_id,v.source_revision,
			v.contract_hash,v.schema_version,v.section_json,v.content_hash,v.status,v.created_by,v.created_at,
			COALESCE(v.activated_by,''),v.activated_at,i.product_id
		FROM nexus_product_integration_versions v
		JOIN nexus_product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND v.id=$2
	`, orgID, versionID).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SourceIntegrationID, &out.SourceVersionID,
		&out.SourceRevision, &out.ContractHash, &out.SchemaVersion, &raw, &out.ContentHash, &out.Status,
		&out.CreatedBy, &out.CreatedAt, &out.ActivatedBy, &out.ActivatedAt, &productID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, uuid.Nil, domainerr.NotFound("product integration version not found")
	}
	if err != nil {
		return Version{}, uuid.Nil, err
	}
	err = json.Unmarshal(raw, &out.Section)
	return out, productID, err
}

func getVersion(ctx context.Context, q queryRower, orgID string, productID, versionID uuid.UUID) (Version, error) {
	version, actualProductID, err := getVersionForOrg(ctx, q, orgID, versionID)
	if err != nil {
		return Version{}, err
	}
	if actualProductID != productID {
		return Version{}, domainerr.NotFound("product integration version not found")
	}
	return version, nil
}

func getVersionBySource(ctx context.Context, q queryRower, orgID string, productID, sourceVersionID uuid.UUID) (Version, error) {
	var versionID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT v.id
		FROM nexus_product_integration_versions v
		JOIN nexus_product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND i.product_id=$2 AND v.source_version_id=$3
	`, orgID, productID, sourceVersionID).Scan(&versionID)
	if err != nil {
		return Version{}, err
	}
	return getVersion(ctx, q, orgID, productID, versionID)
}

func actionAllowed(refs []ActionTypeRef, value string) bool {
	for _, ref := range refs {
		if ref.Key == value {
			return true
		}
	}
	return false
}

func containsConfigured(values []configuredArea, area, mode string) bool {
	for _, value := range values {
		if value.area == area && value.mode == mode {
			return true
		}
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func nullableRevision(value int64) any {
	if value < 1 {
		return nil
	}
	return value
}

func nullableHash(value string) any {
	if !validContentHash(value) {
		return nil
	}
	return strings.ToLower(value)
}

func nullableString(value string, use bool) any {
	value = strings.TrimSpace(value)
	if !use || value == "" {
		return nil
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func nullableTime(use bool, value time.Time) any {
	if !use {
		return nil
	}
	return value
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
