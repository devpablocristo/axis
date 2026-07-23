package productintegrations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const validationFreshness = 24 * time.Hour

type ServiceURLs struct {
	Companion string
	Nexus     string
}

type Service struct {
	pool         *pgxpool.Pool
	participants ParticipantRegistry
	credentials  CredentialRepository
	now          func() time.Time
}

func NewService(pool *pgxpool.Pool, client ServiceClient, urls ServiceURLs) *Service {
	return NewServiceWithParticipants(
		pool,
		NewParticipantRegistry(
			NewHTTPParticipantWithProjection("companion", urls.Companion, client, ProductInvocationProjection),
			NewHTTPParticipantWithProjection("nexus", urls.Nexus, client, GovernanceProjection),
		).WithInvocationProjection(ProductInvocationProjection),
	)
}

func NewServiceWithParticipants(pool *pgxpool.Pool, participants ParticipantRegistry) *Service {
	return NewServiceWithRepository(pool, participants, NewPostgresCredentialRepository(pool))
}

func NewServiceWithRepository(
	pool *pgxpool.Pool,
	participants ParticipantRegistry,
	credentials CredentialRepository,
) *Service {
	return &Service{
		pool: pool, participants: participants, credentials: credentials,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) CreateVersion(
	ctx context.Context,
	orgID, productID uuid.UUID,
	actor string,
	input CreateVersionInput,
) (Version, bool, error) {
	productSurface, role, err := s.requireProductManager(ctx, orgID, productID, actor)
	if err != nil {
		return Version{}, false, err
	}
	_ = role
	contract, err := normalizeContract(input.Contract)
	if err != nil {
		return Version{}, false, err
	}
	hash, err := contractHash(contract)
	if err != nil {
		return Version{}, false, err
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		return Version{}, false, err
	}
	requiredServices, err := s.participants.Required(contract)
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
		INSERT INTO product_integrations(id,org_id,product_id)
		VALUES($1,$2,$3)
		ON CONFLICT(org_id,product_id) DO UPDATE SET updated_at=now()
		RETURNING id
	`, integrationID, orgID, productID).Scan(&integrationID)
	if err != nil {
		return Version{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM product_integrations WHERE id=$1 FOR UPDATE`, integrationID).Scan(&integrationID); err != nil {
		return Version{}, false, err
	}
	existing, getErr := getVersionByHash(ctx, tx, orgID, productID, hash)
	if getErr == nil {
		return existing, false, tx.Commit(ctx)
	}
	if !errors.Is(getErr, pgx.ErrNoRows) {
		return Version{}, false, getErr
	}
	var revision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(revision),0)+1 FROM product_integration_versions WHERE integration_id=$1
	`, integrationID).Scan(&revision); err != nil {
		return Version{}, false, err
	}
	var out Version
	var contractJSON json.RawMessage
	var required []string
	err = tx.QueryRow(ctx, `
		INSERT INTO product_integration_versions(
			id,integration_id,revision,schema_version,contract_json,contract_hash,required_services,created_by
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id,integration_id,revision,schema_version,contract_json,contract_hash,required_services,
			status,created_by,created_at,COALESCE(activated_by,''),activated_at
	`, uuid.New(), integrationID, revision, contract.SchemaVersion, raw, hash, requiredServices, actor).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SchemaVersion, &contractJSON, &out.ContractHash,
		&required, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.ActivatedBy, &out.ActivatedAt,
	)
	if err != nil {
		return Version{}, false, err
	}
	out.RequiredServices = required
	if err := json.Unmarshal(contractJSON, &out.Contract); err != nil {
		return Version{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, false, err
	}
	_ = productSurface
	return out, true, nil
}

func (s *Service) Get(ctx context.Context, orgID, productID uuid.UUID, actor string) (Integration, []Version, error) {
	if _, _, err := s.requireProductMember(ctx, orgID, productID, actor); err != nil {
		return Integration{}, nil, err
	}
	integration, err := getIntegration(ctx, s.pool, orgID, productID)
	if err != nil {
		return Integration{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM product_integration_versions
		WHERE integration_id=$1 ORDER BY revision DESC
	`, integration.ID)
	if err != nil {
		return Integration{}, nil, err
	}
	defer rows.Close()
	versions := make([]Version, 0)
	for rows.Next() {
		var versionID uuid.UUID
		if err := rows.Scan(&versionID); err != nil {
			return Integration{}, nil, err
		}
		version, err := getVersion(ctx, s.pool, orgID, productID, versionID)
		if err != nil {
			return Integration{}, nil, err
		}
		versions = append(versions, version)
	}
	return integration, versions, rows.Err()
}

func (s *Service) Validate(
	ctx context.Context,
	orgID, productID, versionID uuid.UUID,
	actor string,
) (ValidationReport, error) {
	surface, role, err := s.requireProductManager(ctx, orgID, productID, actor)
	if err != nil {
		return ValidationReport{}, err
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, versionID)
	if err != nil {
		return ValidationReport{}, err
	}
	checks := []ValidationCheck{
		{Service: "bff", Code: "schema_version", Status: "pass"},
		{Service: "bff", Code: "organization_product", Status: "pass"},
		{Service: "bff", Code: "authentication", Status: "pass"},
	}
	snapshots := map[string]ServiceSnapshot{
		"bff": {Service: "bff", VersionID: version.ID, Valid: true, ContentHash: version.ContractHash},
	}
	valid := true
	for _, serviceName := range version.RequiredServices {
		if serviceName == "bff" {
			continue
		}
		participant, ok := s.participants.Participant(serviceName)
		if !ok {
			valid = false
			checks = append(checks, ValidationCheck{Service: serviceName, Code: "availability", Status: "fail", Message: "service integration validator is unavailable"})
			continue
		}
		section, applies, projectionErr := participant.Project(version.Contract)
		if projectionErr != nil || !applies {
			valid = false
			checks = append(checks, ValidationCheck{Service: serviceName, Code: "projection", Status: "fail", Message: "participant contract projection is unavailable"})
			continue
		}
		prepared, prepareErr := participant.Prepare(ctx, ServicePrepareInput{
			OrgID: orgID, ProductID: productID,
			ProductSurface: surface, Actor: actor, Role: role,
			SourceIntegrationID: version.IntegrationID, SourceVersionID: version.ID,
			SourceRevision: version.Revision, ContractHash: version.ContractHash,
			Section: section,
		})
		if prepareErr != nil {
			valid = false
			checks = append(checks, ValidationCheck{Service: serviceName, Code: "prepare", Status: "fail", Message: "service snapshot could not be prepared"})
			continue
		}
		validated, validateErr := participant.Validate(ctx, ServiceMutationInput{
			OrgID: orgID, ProductID: productID,
			ProductSurface: surface, Actor: actor, Role: role, VersionID: prepared.VersionID,
		})
		if validateErr != nil || !validated.Valid {
			valid = false
			checks = append(checks, ValidationCheck{Service: serviceName, Code: "compatibility", Status: "fail", Message: "service contract is incompatible"})
			prepared.Valid = false
			snapshots[serviceName] = prepared
			continue
		}
		prepared.Valid = true
		prepared.ContentHash = validated.ContentHash
		snapshots[serviceName] = prepared
		checks = append(checks, ValidationCheck{Service: serviceName, Code: "compatibility", Status: "pass"})
	}
	checksJSON, _ := json.Marshal(checks)
	snapshotsJSON, _ := json.Marshal(snapshots)
	report := ValidationReport{
		ID: uuid.New(), VersionID: version.ID, ContractHash: version.ContractHash,
		Valid: valid, Checks: checks, ServiceSnapshots: snapshots, CreatedBy: actor, CreatedAt: s.now(),
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO product_integration_validation_reports(
			id,org_id,product_id,version_id,contract_hash,valid,checks_json,service_snapshots_json,created_by,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING created_at
	`, report.ID, orgID, productID, version.ID, version.ContractHash, report.Valid, checksJSON, snapshotsJSON, actor, report.CreatedAt).Scan(&report.CreatedAt)
	if err != nil {
		return ValidationReport{}, err
	}
	if valid {
		_, err = s.pool.Exec(ctx, `UPDATE product_integration_versions SET status='validated' WHERE id=$1 AND status='draft'`, version.ID)
	}
	return report, err
}

func (s *Service) Activate(
	ctx context.Context,
	orgID, productID, versionID uuid.UUID,
	actor string,
) (Readiness, error) {
	surface, role, err := s.requireProductManager(ctx, orgID, productID, actor)
	if err != nil {
		return Readiness{}, err
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, versionID)
	if err != nil {
		return Readiness{}, err
	}
	report, err := s.latestReport(ctx, orgID, productID, versionID)
	if err != nil || !report.Valid || report.ContractHash != version.ContractHash || s.now().Sub(report.CreatedAt) > validationFreshness {
		return Readiness{}, domainerr.Conflict("a fresh successful compatibility report is required")
	}
	for _, serviceName := range version.RequiredServices {
		if serviceName == "bff" {
			continue
		}
		participant, ok := s.participants.Participant(serviceName)
		if !ok {
			return Readiness{}, domainerr.Conflict("required integration participant is unavailable")
		}
		snapshot, ok := report.ServiceSnapshots[serviceName]
		if !ok || !snapshot.Valid {
			return Readiness{}, domainerr.Conflict("required service snapshot is not valid")
		}
		if err := participant.Activate(ctx, ServiceMutationInput{
			OrgID: orgID, ProductID: productID,
			ProductSurface: surface, Actor: actor, Role: role, VersionID: snapshot.VersionID,
		}); err != nil {
			return Readiness{}, domainerr.Conflict("required service snapshot could not be activated")
		}
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Readiness{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		UPDATE product_integration_versions SET status='retired'
		WHERE integration_id=$1 AND status='active' AND id<>$2
	`, version.IntegrationID, version.ID); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE product_integration_versions
		SET status='active',activated_by=$2,activated_at=$3 WHERE id=$1
	`, version.ID, actor, s.now()); err != nil {
		return Readiness{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE product_integrations
		SET lifecycle='active',active_version_id=$2,updated_at=$3 WHERE id=$1
	`, version.IntegrationID, version.ID, s.now()); err != nil {
		return Readiness{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Readiness{}, err
	}
	return s.Readiness(ctx, orgID, productID, actor)
}

func (s *Service) ChangeLifecycle(ctx context.Context, orgID, productID uuid.UUID, actor, lifecycle string) (Integration, error) {
	if _, _, err := s.requireProductManager(ctx, orgID, productID, actor); err != nil {
		return Integration{}, err
	}
	if lifecycle != "suspended" && lifecycle != "retired" {
		return Integration{}, domainerr.Validation("integration lifecycle transition is invalid")
	}
	var out Integration
	err := s.pool.QueryRow(ctx, `
		UPDATE product_integrations SET lifecycle=$4,updated_at=$5
		WHERE org_id=$1 AND product_id=$2 AND lifecycle<>'retired'
		RETURNING id,org_id,product_id,$3,lifecycle,active_version_id,created_at,updated_at
	`, orgID, productID, "", lifecycle, s.now()).Scan(
		&out.ID, &out.OrgID, &out.ProductID, &out.ProductSurface, &out.Lifecycle,
		&out.ActiveVersionID, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, domainerr.NotFound("product integration not found")
	}
	if err != nil {
		return Integration{}, err
	}
	surface, _, memberErr := s.requireProductMember(ctx, orgID, productID, actor)
	if memberErr == nil {
		out.ProductSurface = surface
	}
	return out, nil
}

func (s *Service) Readiness(ctx context.Context, orgID, productID uuid.UUID, actor string) (Readiness, error) {
	surface, role, err := s.requireProductMember(ctx, orgID, productID, actor)
	if err != nil {
		return Readiness{}, err
	}
	integration, err := getIntegration(ctx, s.pool, orgID, productID)
	if err != nil {
		return Readiness{}, err
	}
	out := Readiness{
		ProductID: productID, ProductSurface: surface, Lifecycle: integration.Lifecycle,
		Status: "blocked", Services: map[string]ServiceReadiness{}, CheckedAt: s.now(),
	}
	if integration.Lifecycle == "suspended" || integration.Lifecycle == "retired" {
		out.Status = "inactive"
		return out, nil
	}
	if integration.ActiveVersionID == nil {
		return out, nil
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, *integration.ActiveVersionID)
	if err != nil {
		return out, err
	}
	out.ContractHash, out.Version = version.ContractHash, version.Revision
	out.Status = "ready"
	out.Services["bff"] = ServiceReadiness{Service: "bff", Status: "ready"}
	for _, serviceName := range version.RequiredServices {
		if serviceName == "bff" {
			continue
		}
		participant, ok := s.participants.Participant(serviceName)
		if !ok {
			out.Services[serviceName] = ServiceReadiness{Service: serviceName, Status: "unavailable"}
			out.Status = "partial"
			continue
		}
		readiness, serviceErr := participant.Readiness(ctx, ServiceReadinessInput{
			OrgID:     orgID,
			ProductID: productID, ProductSurface: surface, Actor: actor, Role: role,
		})
		if serviceErr != nil {
			readiness = ServiceReadiness{Service: serviceName, Status: "unavailable"}
			out.Status = "partial"
		} else if readiness.Status != "ready" {
			out.Status = "blocked"
		}
		out.Services[serviceName] = readiness
	}
	return out, nil
}

func (s *Service) CreateCredential(
	ctx context.Context,
	orgID, productID uuid.UUID,
	actor string,
	input CreateCredentialInput,
) (Credential, error) {
	if _, _, err := s.requireProductManager(ctx, orgID, productID, actor); err != nil {
		return Credential{}, err
	}
	integration, version, err := s.activeIntegration(ctx, orgID, productID)
	if err != nil {
		return Credential{}, err
	}
	input.ServicePrincipal = strings.TrimSpace(input.ServicePrincipal)
	if input.ServicePrincipal == "" || len(input.ServicePrincipal) > 256 {
		return Credential{}, domainerr.Validation("service_principal is invalid")
	}
	scopes, err := normalizeCodes(input.Scopes)
	if err != nil || len(scopes) == 0 {
		return Credential{}, domainerr.Validation("credential scopes are invalid")
	}
	for _, scope := range scopes {
		if !slices.Contains(version.Contract.Authentication.Scopes, scope) {
			return Credential{}, domainerr.Forbidden("credential scope is not authorized by the active contract")
		}
	}
	return s.insertCredential(ctx, orgID, productID, integration.ID, actor, input.ServicePrincipal, scopes)
}

func (s *Service) ListCredentials(ctx context.Context, orgID, productID uuid.UUID, actor string) ([]Credential, error) {
	if _, _, err := s.requireProductMember(ctx, orgID, productID, actor); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id,org_id,product_id,integration_id,key_prefix,service_principal,scopes,status,
			created_by,created_at,rotated_at,COALESCE(revoked_by,''),revoked_at
		FROM product_credentials WHERE org_id=$1 AND product_id=$2 ORDER BY created_at DESC
	`, orgID, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Credential, 0)
	for rows.Next() {
		var item Credential
		if err := rows.Scan(
			&item.ID, &item.OrgID, &item.ProductID, &item.IntegrationID, &item.KeyPrefix,
			&item.ServicePrincipal, &item.Scopes, &item.Status, &item.CreatedBy, &item.CreatedAt,
			&item.RotatedAt, &item.RevokedBy, &item.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) RotateCredential(
	ctx context.Context,
	orgID, productID, credentialID uuid.UUID,
	actor string,
) (Credential, error) {
	if _, _, err := s.requireProductManager(ctx, orgID, productID, actor); err != nil {
		return Credential{}, err
	}
	var principal string
	var scopes []string
	var integrationID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE product_credentials
		SET status='revoked',revoked_by=$4,revoked_at=$5,rotated_at=$5
		WHERE org_id=$1 AND product_id=$2 AND id=$3 AND status='active'
		RETURNING service_principal,scopes,integration_id
	`, orgID, productID, credentialID, actor, s.now()).Scan(&principal, &scopes, &integrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, domainerr.NotFound("active product credential not found")
	}
	if err != nil {
		return Credential{}, err
	}
	return s.insertCredential(ctx, orgID, productID, integrationID, actor, principal, scopes)
}

func (s *Service) RevokeCredential(ctx context.Context, orgID, productID, credentialID uuid.UUID, actor string) error {
	if _, _, err := s.requireProductManager(ctx, orgID, productID, actor); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE product_credentials SET status='revoked',revoked_by=$4,revoked_at=$5
		WHERE org_id=$1 AND product_id=$2 AND id=$3 AND status='active'
	`, orgID, productID, credentialID, actor, s.now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domainerr.NotFound("active product credential not found")
	}
	return nil
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (MachineBinding, error) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "axis_pk_") || len(key) < 32 {
		return MachineBinding{}, domainerr.Unauthorized("invalid product credential")
	}
	digest := sha256.Sum256([]byte(key))
	if s.credentials == nil {
		return MachineBinding{}, errors.New("product credential repository is unavailable")
	}
	record, err := s.credentials.ActiveByDigest(ctx, digest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return MachineBinding{}, domainerr.Unauthorized("invalid product credential")
	}
	if err != nil {
		return MachineBinding{}, err
	}
	binding := MachineBinding{Context: record.Context}
	binding.Context.IntegrationID = record.IntegrationID.String()
	binding.Context.AccessMode = "direct"
	binding.Context.PrincipalType = "service"
	var contract Contract
	if err := json.Unmarshal(record.Contract, &contract); err != nil {
		return MachineBinding{}, domainerr.Unauthorized("active product contract is invalid")
	}
	for _, scope := range binding.Context.Scopes {
		if !slices.Contains(contract.Authentication.Scopes, scope) {
			return MachineBinding{}, domainerr.Unauthorized("product credential exceeds the active contract")
		}
	}
	section, ok, projectionErr := s.participants.InvocationSection(contract)
	if projectionErr != nil {
		return MachineBinding{}, domainerr.Unauthorized("active product contract is invalid")
	}
	if !ok {
		return MachineBinding{}, domainerr.Forbidden("active product contract does not authorize product invocation")
	}
	for _, id := range section.VirployeeIDs {
		binding.AllowedVirployeeIDs = append(binding.AllowedVirployeeIDs, id.String())
	}
	for _, id := range section.PoolIDs {
		binding.AllowedPoolIDs = append(binding.AllowedPoolIDs, id.String())
	}
	binding.AllowedCapabilities = section.Capabilities
	binding.AllowedEvents = section.Events
	binding.MaxRequestBytes = contract.Limits.MaxRequestBytes
	return binding, nil
}

func (s *Service) insertCredential(
	ctx context.Context,
	orgID, productID, integrationID uuid.UUID,
	actor, principal string,
	scopes []string,
) (Credential, error) {
	secret, digest, prefix, err := newAPIKey()
	if err != nil {
		return Credential{}, err
	}
	var out Credential
	err = s.pool.QueryRow(ctx, `
		INSERT INTO product_credentials(
			id,org_id,product_id,integration_id,key_prefix,secret_digest,service_principal,scopes,created_by
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,org_id,product_id,integration_id,key_prefix,service_principal,scopes,status,
			created_by,created_at,rotated_at,COALESCE(revoked_by,''),revoked_at
	`, uuid.New(), orgID, productID, integrationID, prefix, digest, principal, scopes, actor).Scan(
		&out.ID, &out.OrgID, &out.ProductID, &out.IntegrationID, &out.KeyPrefix,
		&out.ServicePrincipal, &out.Scopes, &out.Status, &out.CreatedBy, &out.CreatedAt,
		&out.RotatedAt, &out.RevokedBy, &out.RevokedAt,
	)
	if err != nil {
		return Credential{}, err
	}
	out.Secret = secret
	return out, nil
}

func newAPIKey() (secret string, digest []byte, prefix string, err error) {
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return "", nil, "", err
	}
	secret = "axis_pk_" + base64.RawURLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(secret))
	prefix = secret[:min(16, len(secret))]
	return secret, sum[:], prefix, nil
}

func (s *Service) activeIntegration(ctx context.Context, orgID, productID uuid.UUID) (Integration, Version, error) {
	integration, err := getIntegration(ctx, s.pool, orgID, productID)
	if err != nil {
		return Integration{}, Version{}, err
	}
	if integration.Lifecycle != "active" || integration.ActiveVersionID == nil {
		return Integration{}, Version{}, domainerr.Conflict("product integration is not active")
	}
	version, err := getVersion(ctx, s.pool, orgID, productID, *integration.ActiveVersionID)
	return integration, version, err
}

func (s *Service) requireProductManager(ctx context.Context, orgID, productID uuid.UUID, actor string) (string, string, error) {
	surface, role, err := s.requireProductMember(ctx, orgID, productID, actor)
	if err != nil {
		return "", "", err
	}
	if role != "owner" && role != "admin" {
		return "", "", domainerr.Forbidden("organization owner or admin is required")
	}
	return surface, role, nil
}

func (s *Service) requireProductMember(ctx context.Context, orgID, productID uuid.UUID, actor string) (string, string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" || orgID == uuid.Nil || productID == uuid.Nil {
		return "", "", domainerr.Validation("organization, product, and actor are required")
	}
	var surface, role, productStatus, memberStatus string
	err := s.pool.QueryRow(ctx, `
		SELECT p.product_surface,p.status,m.role,m.status
		FROM axis_products p
		JOIN axis_org_members m ON m.org_id=p.org_id
		JOIN axis_users u ON u.id=m.user_id
		WHERE p.id=$1 AND p.org_id=$2 AND (m.user_id::text=$3 OR u.provider_user_id=$3)
			AND p.archived_at IS NULL AND p.trashed_at IS NULL
	`, productID, orgID, actor).Scan(&surface, &productStatus, &role, &memberStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domainerr.NotFound("organization product not found")
	}
	if err != nil {
		return "", "", err
	}
	if productStatus != "active" || memberStatus != "active" {
		return "", "", domainerr.Forbidden("organization product membership is inactive")
	}
	return surface, role, nil
}

func (s *Service) latestReport(ctx context.Context, orgID, productID, versionID uuid.UUID) (ValidationReport, error) {
	var out ValidationReport
	var checksJSON, snapshotsJSON json.RawMessage
	err := s.pool.QueryRow(ctx, `
		SELECT id,version_id,contract_hash,valid,checks_json,service_snapshots_json,created_by,created_at
		FROM product_integration_validation_reports
		WHERE org_id=$1 AND product_id=$2 AND version_id=$3
		ORDER BY created_at DESC LIMIT 1
	`, orgID, productID, versionID).Scan(
		&out.ID, &out.VersionID, &out.ContractHash, &out.Valid, &checksJSON,
		&snapshotsJSON, &out.CreatedBy, &out.CreatedAt,
	)
	if err != nil {
		return ValidationReport{}, err
	}
	if err := json.Unmarshal(checksJSON, &out.Checks); err != nil {
		return ValidationReport{}, err
	}
	if err := json.Unmarshal(snapshotsJSON, &out.ServiceSnapshots); err != nil {
		return ValidationReport{}, err
	}
	return out, nil
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getIntegration(ctx context.Context, q queryRower, orgID, productID uuid.UUID) (Integration, error) {
	var out Integration
	err := q.QueryRow(ctx, `
		SELECT i.id,i.org_id,i.product_id,p.product_surface,i.lifecycle,i.active_version_id,i.created_at,i.updated_at
		FROM product_integrations i
		JOIN axis_products p ON p.id=i.product_id AND p.org_id=i.org_id
		WHERE i.org_id=$1 AND i.product_id=$2
	`, orgID, productID).Scan(
		&out.ID, &out.OrgID, &out.ProductID, &out.ProductSurface, &out.Lifecycle,
		&out.ActiveVersionID, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, domainerr.NotFound("product integration not found")
	}
	return out, err
}

func getVersion(ctx context.Context, q queryRower, orgID, productID, versionID uuid.UUID) (Version, error) {
	var out Version
	var raw json.RawMessage
	err := q.QueryRow(ctx, `
		SELECT v.id,v.integration_id,v.revision,v.schema_version,v.contract_json,v.contract_hash,
			v.required_services,v.status,v.created_by,v.created_at,COALESCE(v.activated_by,''),v.activated_at
		FROM product_integration_versions v
		JOIN product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND i.product_id=$2 AND v.id=$3
	`, orgID, productID, versionID).Scan(
		&out.ID, &out.IntegrationID, &out.Revision, &out.SchemaVersion, &raw, &out.ContractHash,
		&out.RequiredServices, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.ActivatedBy, &out.ActivatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, domainerr.NotFound("product integration version not found")
	}
	if err != nil {
		return Version{}, err
	}
	err = json.Unmarshal(raw, &out.Contract)
	return out, err
}

func getVersionByHash(ctx context.Context, q queryRower, orgID, productID uuid.UUID, hash string) (Version, error) {
	var versionID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT v.id FROM product_integration_versions v
		JOIN product_integrations i ON i.id=v.integration_id
		WHERE i.org_id=$1 AND i.product_id=$2 AND v.contract_hash=$3
	`, orgID, productID, hash).Scan(&versionID)
	if err != nil {
		return Version{}, err
	}
	return getVersion(ctx, q, orgID, productID, versionID)
}
