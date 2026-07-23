package productintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const validationFreshness = 24 * time.Hour

var supportedAPIs = map[string]struct{}{
	"axis.product-edge": {},
	"assist-runs":       {}, "evaluations": {}, "finops": {}, "knowledge": {},
	"mcp": {}, "operations": {}, "watchers": {},
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func requireTrusted(orgID, actor, role string, mutation bool) error {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(actor) == "" {
		return domainerr.Validation("trusted organization and actor are required")
	}
	if mutation && role != "owner" && role != "admin" {
		return domainerr.Forbidden("organization owner or admin is required")
	}
	return nil
}

func (s *Service) CreateVersion(
	ctx context.Context,
	orgID, actor, role string,
	productID uuid.UUID,
	surface string,
	in CreateVersionInput,
) (Version, bool, error) {
	if err := requireTrusted(orgID, actor, role, true); err != nil {
		return Version{}, false, err
	}
	surface = strings.ToLower(strings.TrimSpace(surface))
	if surface == "" {
		surface = strings.ToLower(strings.TrimSpace(in.ProductSurface))
	}
	if !codePattern.MatchString(surface) || in.SourceIntegrationID == uuid.Nil || in.SourceVersionID == uuid.Nil ||
		in.Version < 1 || !hashPattern.MatchString(strings.ToLower(strings.TrimSpace(in.ContractHash))) {
		return Version{}, false, domainerr.Validation("product integration binding is invalid")
	}
	section, err := normalizeSection(in.Section)
	if err != nil {
		return Version{}, false, err
	}
	contentHash, err := sectionHash(section)
	if err != nil {
		return Version{}, false, err
	}
	raw, _ := json.Marshal(section)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Version{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	integrationID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO companion_product_integrations(id,org_id,product_id,product_surface)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(org_id,product_id) DO UPDATE
		SET product_surface=EXCLUDED.product_surface,updated_at=now()
		RETURNING id
	`, integrationID, orgID, productID, surface).Scan(&integrationID)
	if err != nil {
		return Version{}, false, err
	}
	var existingID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT v.id FROM companion_product_integration_versions v
		JOIN companion_product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND i.product_id=$2 AND v.source_version_id=$3
	`, orgID, productID, in.SourceVersionID).Scan(&existingID)
	if err == nil {
		existing, getErr := getVersion(ctx, tx, orgID, productID, existingID)
		return existing, false, commit(ctx, tx, getErr)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Version{}, false, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(revision),0)+1 FROM companion_product_integration_versions WHERE integration_id=$1
	`, integrationID).Scan(&revision); err != nil {
		return Version{}, false, err
	}
	var out Version
	var sectionJSON json.RawMessage
	err = tx.QueryRow(ctx, `
		INSERT INTO companion_product_integration_versions(
			id,integration_id,revision,source_integration_id,source_version_id,source_revision,
			contract_hash,section_json,content_hash,created_by
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id,integration_id,revision,source_integration_id,source_version_id,source_revision,
			contract_hash,section_json,content_hash,status,created_by,created_at
	`, uuid.New(), integrationID, revision, in.SourceIntegrationID, in.SourceVersionID, in.Version,
		strings.ToLower(in.ContractHash), raw, contentHash, actor).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SourceIntegrationID, &out.SourceVersionID,
		&out.SourceRevision, &out.ContractHash, &sectionJSON, &out.ContentHash, &out.Status,
		&out.CreatedBy, &out.CreatedAt,
	)
	if err != nil {
		return Version{}, false, err
	}
	if err := json.Unmarshal(sectionJSON, &out.Section); err != nil {
		return Version{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, false, err
	}
	return out, true, nil
}

func (s *Service) Validate(ctx context.Context, orgID, actor, role string, versionID uuid.UUID) (ValidationReport, error) {
	if err := requireTrusted(orgID, actor, role, true); err != nil {
		return ValidationReport{}, err
	}
	version, productID, err := getVersionForOrg(ctx, s.pool, orgID, versionID)
	if err != nil {
		return ValidationReport{}, err
	}
	checks := []ValidationCheck{{Code: "schema_version", Status: "pass"}}
	valid := true
	add := func(code string, ok bool, message string) {
		status := "pass"
		if !ok {
			valid, status = false, "fail"
		}
		checks = append(checks, ValidationCheck{Code: code, Status: status, Message: message})
	}
	for _, api := range version.Section.APIContracts {
		_, ok := supportedAPIs[api.Name]
		add("api."+api.Name, ok && api.Version == "v1", "API contract is unsupported")
	}
	for _, virployeeID := range version.Section.VirployeeIDs {
		var exists bool
		queryErr := s.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM virployees
			WHERE org_id=$1 AND id=$2 AND archived_at IS NULL AND trashed_at IS NULL)
		`, orgID, virployeeID).Scan(&exists)
		add("virployee."+virployeeID.String(), queryErr == nil && exists, "Virployee is missing or inactive")
	}
	for _, poolID := range version.Section.PoolIDs {
		var exists bool
		queryErr := s.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM companion_routing_pools
			WHERE org_id=$1 AND id=$2 AND archived_at IS NULL)
		`, orgID, poolID).Scan(&exists)
		add("pool."+poolID.String(), queryErr == nil && exists, "routing pool is missing or inactive")
	}
	for _, capability := range version.Section.Capabilities {
		var active bool
		var queryErr error
		checkIdentity := capability.Key
		if capability.ID != "" {
			capabilityID, parseErr := uuid.Parse(capability.ID)
			if parseErr != nil {
				queryErr = parseErr
			} else {
				queryErr = s.pool.QueryRow(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM capabilities
						WHERE org_id=$1 AND id=$2 AND ($3='' OR capability_key=$3)
							AND promotion_state='active'
							AND manifest_hash=$4 AND conformed_hash=$4
							AND COALESCE(manifest->>'version','')=$5
							AND archived_at IS NULL AND trashed_at IS NULL
					)
				`, orgID, capabilityID, capability.Key, capability.ManifestHash, capability.Version).Scan(&active)
			}
			checkIdentity = capability.ID
		} else {
			queryErr = s.pool.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM capabilities
					WHERE org_id=$1 AND capability_key=$2 AND promotion_state='active'
						AND manifest_hash=$3 AND conformed_hash=$3
						AND COALESCE(manifest->>'version','')=$4
						AND archived_at IS NULL AND trashed_at IS NULL
				)
			`, orgID, capability.Key, capability.ManifestHash, capability.Version).Scan(&active)
		}
		add("capability."+checkIdentity, queryErr == nil && active, "capability manifest is missing, inactive, or drifted")
	}
	checksJSON, _ := json.Marshal(checks)
	report := ValidationReport{
		ID: uuid.New(), ProductID: productID, VersionID: version.ID, ContentHash: version.ContentHash,
		Valid: valid, Checks: checks, CreatedAt: s.now(),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO companion_product_integration_validation_reports(
			id,org_id,product_id,version_id,content_hash,valid,checks_json,created_by,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(version_id,content_hash) DO UPDATE
		SET valid=EXCLUDED.valid,checks_json=EXCLUDED.checks_json,created_by=EXCLUDED.created_by,created_at=EXCLUDED.created_at
		RETURNING id,created_at
	`, report.ID, orgID, productID, version.ID, version.ContentHash, valid, checksJSON, actor, report.CreatedAt).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		return ValidationReport{}, err
	}
	if valid {
		_, err = s.pool.Exec(ctx, `UPDATE companion_product_integration_versions SET status='validated' WHERE id=$1 AND status='draft'`, version.ID)
	}
	return report, err
}

func (s *Service) Activate(ctx context.Context, orgID, actor, role string, versionID uuid.UUID) (Readiness, error) {
	if err := requireTrusted(orgID, actor, role, true); err != nil {
		return Readiness{}, err
	}
	version, productID, err := getVersionForOrg(ctx, s.pool, orgID, versionID)
	if err != nil {
		return Readiness{}, err
	}
	var valid bool
	var reportHash string
	var reportAt time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT valid,content_hash,created_at
		FROM companion_product_integration_validation_reports
		WHERE version_id=$1 ORDER BY created_at DESC LIMIT 1
	`, versionID).Scan(&valid, &reportHash, &reportAt)
	if err != nil || !valid || reportHash != version.ContentHash || s.now().Sub(reportAt) > validationFreshness {
		return Readiness{}, domainerr.Conflict("a fresh successful compatibility report is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Readiness{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		UPDATE companion_product_integration_versions SET status='retired'
		WHERE integration_id=$1 AND status='active' AND id<>$2
	`, version.IntegrationID, version.ID); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE companion_product_integration_versions
		SET status='active',activated_by=$2,activated_at=$3 WHERE id=$1
	`, version.ID, actor, s.now()); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE companion_product_integrations
		SET lifecycle='active',active_version_id=$2,updated_at=$3 WHERE id=$1
	`, version.IntegrationID, version.ID, s.now()); err != nil {
		return Readiness{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Readiness{}, err
	}
	return s.Readiness(ctx, orgID, actor, productID)
}

func (s *Service) Suspend(ctx context.Context, orgID, actor, role string, productID uuid.UUID) error {
	if err := requireTrusted(orgID, actor, role, true); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE companion_product_integrations SET lifecycle='suspended',updated_at=$3
		WHERE org_id=$1 AND product_id=$2 AND lifecycle<>'retired'
	`, orgID, productID, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.NotFound("product integration not found")
	}
	return nil
}

func (s *Service) Readiness(ctx context.Context, orgID, actor string, productID uuid.UUID) (Readiness, error) {
	if err := requireTrusted(orgID, actor, "", false); err != nil {
		return Readiness{}, err
	}
	var lifecycle, surface string
	var activeID *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT lifecycle,product_surface,active_version_id
		FROM companion_product_integrations WHERE org_id=$1 AND product_id=$2
	`, orgID, productID).Scan(&lifecycle, &surface, &activeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Readiness{}, domainerr.NotFound("product integration not found")
	}
	if err != nil {
		return Readiness{}, err
	}
	out := Readiness{
		Service: "companion", ProductID: productID, ProductSurface: surface,
		Lifecycle: lifecycle, Status: "blocked", CheckedAt: s.now(),
	}
	if lifecycle != "active" {
		out.Status = "inactive"
		return out, nil
	}
	if activeID == nil {
		return out, nil
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, *activeID)
	if err != nil {
		return out, err
	}
	out.ActiveRevision, out.ActiveContentHash = version.SourceRevision, version.ContractHash
	report, reportErr := s.currentReport(ctx, orgID, version.ID)
	if reportErr != nil || !report.Valid || report.ContentHash != version.ContentHash {
		out.Checks = []ValidationCheck{{Code: "validation", Status: "fail", Message: "active validation is missing or stale"}}
		return out, nil
	}
	out.Checks, out.Status = report.Checks, "ready"
	return out, nil
}

func (s *Service) ValidateRuntimeContext(ctx context.Context, runtime RuntimeContext, capabilityID, capabilityKey string) error {
	if runtime.OrgID == "" || runtime.ProductID == uuid.Nil || runtime.IntegrationID == uuid.Nil ||
		runtime.IntegrationRevision < 1 || !hashPattern.MatchString(runtime.IntegrationHash) {
		return domainerr.Forbidden("trusted product integration context is incomplete")
	}
	var lifecycle, contractHash string
	var sourceIntegrationID uuid.UUID
	var sourceRevision int64
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT i.lifecycle,v.source_integration_id,v.source_revision,v.contract_hash,v.section_json
		FROM companion_product_integrations i
		JOIN companion_product_integration_versions v ON v.id=i.active_version_id
		WHERE i.org_id=$1 AND i.product_id=$2
	`, runtime.OrgID, runtime.ProductID).Scan(&lifecycle, &sourceIntegrationID, &sourceRevision, &contractHash, &raw)
	if err != nil || lifecycle != "active" || sourceIntegrationID != runtime.IntegrationID ||
		sourceRevision != runtime.IntegrationRevision || contractHash != runtime.IntegrationHash {
		return domainerr.Forbidden("product integration is inactive or does not match the active binding")
	}
	capabilityID = strings.TrimSpace(capabilityID)
	capabilityKey = strings.ToLower(strings.TrimSpace(capabilityKey))
	if capabilityID == "" && capabilityKey == "" {
		return nil
	}
	var section Section
	if json.Unmarshal(raw, &section) != nil {
		return domainerr.Forbidden("product integration section is invalid")
	}
	for _, ref := range section.Capabilities {
		if capabilityID != "" && ref.ID == capabilityID &&
			(capabilityKey == "" || ref.Key == "" || ref.Key == capabilityKey) {
			return nil
		}
		if capabilityID == "" && capabilityKey != "" && ref.Key == capabilityKey {
			return nil
		}
	}
	return domainerr.Forbidden("capability is not authorized by the active product integration")
}

func (s *Service) RecordObservation(
	ctx context.Context,
	runtime RuntimeContext,
	area string,
	statusCode int,
	latency time.Duration,
) error {
	if runtime.OrgID == "" || runtime.ProductID == uuid.Nil || area == "" {
		return nil
	}
	now := s.now()
	success, denied, failed := statusCode >= 200 && statusCode < 400, statusCode == 401 || statusCode == 403, statusCode >= 500
	errorCode := any(nil)
	errorAt := any(nil)
	if denied || failed {
		errorCode, errorAt = "http_"+strings.TrimSpace(strings.ToLower(httpStatus(statusCode))), now
	}
	successAt := any(nil)
	if success {
		successAt = now
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO companion_product_service_activity(
			org_id,product_id,product_surface,integration_id,integration_revision,integration_hash,
			area,access_mode,bucket_start,request_count,success_count,denied_count,failure_count,
			latency_samples_ms,last_seen_at,last_success_at,last_error_code,last_error_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,'direct',$8,1,$9,$10,$11,ARRAY[$12]::integer[],$13,$14,$15,$16)
		ON CONFLICT(org_id,product_id,area,access_mode,bucket_start) DO UPDATE SET
			request_count=companion_product_service_activity.request_count+1,
			success_count=companion_product_service_activity.success_count+EXCLUDED.success_count,
			denied_count=companion_product_service_activity.denied_count+EXCLUDED.denied_count,
			failure_count=companion_product_service_activity.failure_count+EXCLUDED.failure_count,
			latency_samples_ms=(
				companion_product_service_activity.latency_samples_ms || EXCLUDED.latency_samples_ms
			)[GREATEST(1,array_length(companion_product_service_activity.latency_samples_ms || EXCLUDED.latency_samples_ms,1)-255):],
			last_seen_at=EXCLUDED.last_seen_at,
			last_success_at=COALESCE(EXCLUDED.last_success_at,companion_product_service_activity.last_success_at),
			last_error_code=COALESCE(EXCLUDED.last_error_code,companion_product_service_activity.last_error_code),
			last_error_at=COALESCE(EXCLUDED.last_error_at,companion_product_service_activity.last_error_at)
	`, runtime.OrgID, runtime.ProductID, runtime.ProductSurface, nullableUUID(runtime.IntegrationID),
		nullableInt(runtime.IntegrationRevision), nullableHash(runtime.IntegrationHash), area, now.Truncate(time.Hour),
		boolInt(success), boolInt(denied), boolInt(failed), max(0, int(latency.Milliseconds())), now,
		successAt, errorCode, errorAt)
	return err
}

func (s *Service) ListServed(ctx context.Context, orgID, actor, productFilter string, window time.Duration) ([]ServedProduct, error) {
	if err := requireTrusted(orgID, actor, "", false); err != nil {
		return nil, err
	}
	if window <= 0 || window > 31*24*time.Hour {
		window = 24 * time.Hour
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.id,i.product_id,i.product_surface,i.lifecycle,
			COALESCE(v.source_revision,0),COALESCE(v.contract_hash,''),COALESCE(v.section_json,'{}'::jsonb)
		FROM companion_product_integrations i
		LEFT JOIN companion_product_integration_versions v ON v.id=i.active_version_id
		WHERE i.org_id=$1 AND ($2='' OR i.product_id::text=$2 OR i.product_surface=$2)
		ORDER BY i.product_surface
	`, orgID, productFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ServedProduct, 0)
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
		observed, err := s.activity(ctx, orgID, productID, s.now().Add(-window))
		if err != nil {
			return nil, err
		}
		keys := map[string]struct{}{}
		for _, area := range configured {
			keys[area] = struct{}{}
		}
		for area := range observed {
			keys[area] = struct{}{}
		}
		names := make([]string, 0, len(keys))
		for area := range keys {
			names = append(names, area)
		}
		sort.Strings(names)
		for _, area := range names {
			item := ServedProduct{
				Service: "companion", OrgID: orgID, ProductID: productID, ProductSurface: surface,
				IntegrationID: integrationID, IntegrationRevision: revision, IntegrationHash: contractHash,
				Area: area, Lifecycle: lifecycle, Configured: slices.Contains(configured, area), Status: "idle",
			}
			if lifecycle != "active" {
				item.Status = "inactive"
			}
			if aggregate, ok := observed[area]; ok {
				item.Observed = true
				item.Requests, item.Succeeded, item.Denied, item.Failed = aggregate.requests, aggregate.succeeded, aggregate.denied, aggregate.failed
				item.LastSeenAt, item.LastSuccessAt, item.LastErrorCode = aggregate.lastSeen, aggregate.lastSuccess, aggregate.lastError
				if aggregate.requests > 0 {
					rate := float64(aggregate.succeeded) / float64(aggregate.requests)
					item.SuccessRate = &rate
				}
				if len(aggregate.latencies) > 0 {
					sort.Ints(aggregate.latencies)
					index := int(math.Ceil(float64(len(aggregate.latencies))*0.95)) - 1
					p95 := float64(aggregate.latencies[max(0, index)])
					item.P95LatencyMS = &p95
				}
				if lifecycle == "active" {
					item.Status = "serving"
					if aggregate.failed*20 >= max64(1, aggregate.requests) {
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

type aggregate struct {
	requests, succeeded, denied, failed int64
	latencies                           []int
	lastSeen, lastSuccess               *time.Time
	lastError                           string
}

func (s *Service) activity(ctx context.Context, orgID string, productID uuid.UUID, since time.Time) (map[string]aggregate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT area,request_count,success_count,denied_count,failure_count,latency_samples_ms,
			last_seen_at,last_success_at,COALESCE(last_error_code,'')
		FROM companion_product_service_activity
		WHERE org_id=$1 AND product_id=$2 AND bucket_start >= $3
	`, orgID, productID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]aggregate{}
	for rows.Next() {
		var area, lastError string
		var requests, succeeded, denied, failed int64
		var latencies []int
		var lastSeen time.Time
		var lastSuccess *time.Time
		if err := rows.Scan(&area, &requests, &succeeded, &denied, &failed, &latencies, &lastSeen, &lastSuccess, &lastError); err != nil {
			return nil, err
		}
		current := out[area]
		current.requests += requests
		current.succeeded += succeeded
		current.denied += denied
		current.failed += failed
		current.latencies = append(current.latencies, latencies...)
		if current.lastSeen == nil || lastSeen.After(*current.lastSeen) {
			value := lastSeen
			current.lastSeen, current.lastError = &value, lastError
		}
		if lastSuccess != nil && (current.lastSuccess == nil || lastSuccess.After(*current.lastSuccess)) {
			value := *lastSuccess
			current.lastSuccess = &value
		}
		out[area] = current
	}
	return out, rows.Err()
}

func configuredAreas(section Section) []string {
	seen := map[string]struct{}{}
	for _, api := range section.APIContracts {
		area := api.Name
		switch {
		case strings.Contains(area, "assist"):
			area = "assist"
		case strings.Contains(area, "mcp"):
			area = "mcp"
		case strings.Contains(area, "knowledge"):
			area = "knowledge"
		case strings.Contains(area, "watch"):
			area = "watchers"
		case strings.Contains(area, "evalu"):
			area = "evaluations"
		case strings.Contains(area, "finops"):
			area = "finops"
		case strings.Contains(area, "operation"):
			area = "jobs"
		}
		seen[area] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for area := range seen {
		out = append(out, area)
	}
	sort.Strings(out)
	return out
}

func (s *Service) currentReport(ctx context.Context, orgID string, versionID uuid.UUID) (ValidationReport, error) {
	var out ValidationReport
	var checksJSON json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT id,product_id,version_id,content_hash,valid,checks_json,created_at
		FROM companion_product_integration_validation_reports
		WHERE org_id=$1 AND version_id=$2 ORDER BY created_at DESC LIMIT 1
	`, orgID, versionID).Scan(&out.ID, &out.ProductID, &out.VersionID, &out.ContentHash, &out.Valid, &checksJSON, &out.CreatedAt)
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
			v.contract_hash,v.section_json,v.content_hash,v.status,v.created_by,v.created_at,i.product_id
		FROM companion_product_integration_versions v
		JOIN companion_product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND v.id=$2
	`, orgID, versionID).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SourceIntegrationID, &out.SourceVersionID,
		&out.SourceRevision, &out.ContractHash, &raw, &out.ContentHash, &out.Status,
		&out.CreatedBy, &out.CreatedAt, &productID,
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

func commit(ctx context.Context, tx pgx.Tx, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func nullableInt(value int64) any {
	if value < 1 {
		return nil
	}
	return value
}

func nullableHash(value string) any {
	if !hashPattern.MatchString(value) {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func httpStatus(value int) string {
	if value < 100 || value > 599 {
		return "unknown"
	}
	return strconv.Itoa(value)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
