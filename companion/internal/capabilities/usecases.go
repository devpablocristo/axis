package capabilities

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/products"
)

type Usecases struct {
	repo            Repository
	productRegistry ProductRegistry
	sourceFetcher   ManifestSourceFetcher
}

type ProductRegistry interface {
	GetProduct(ctx context.Context, productSurface string) (products.Product, error)
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (uc *Usecases) WithProductRegistry(registry ProductRegistry) *Usecases {
	uc.productRegistry = registry
	return uc
}

func (uc *Usecases) WithManifestSourceFetcher(fetcher ManifestSourceFetcher) *Usecases {
	uc.sourceFetcher = fetcher
	return uc
}

func (uc *Usecases) SyncGenerated(ctx context.Context, manifests []Manifest) error {
	if uc == nil || uc.repo == nil {
		return nil
	}
	reg, err := NewRegistry(manifests)
	if err != nil {
		return err
	}
	for _, manifest := range reg.All() {
		if _, err := uc.repo.UpsertManifest(ctx, ManifestRecord{
			Manifest: manifest,
			Status:   ManifestStatusActive,
			Source:   ManifestSourceGenerated,
		}); err != nil {
			return fmt.Errorf("sync generated capability %s@%s: %w", manifest.CapabilityID, manifest.Version, err)
		}
	}
	return nil
}

func (uc *Usecases) ImportManifest(ctx context.Context, manifest Manifest, importedBy string) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	manifest = manifest.Normalize()
	if err := manifest.Validate(); err != nil {
		return ManifestRecord{}, err
	}
	return uc.repo.UpsertManifest(ctx, ManifestRecord{
		Manifest:   manifest,
		Status:     ManifestStatusDraft,
		Source:     ManifestSourceImported,
		ImportedBy: importedBy,
	})
}

type ImportManifestSourceInput struct {
	SourceURL              string
	ExpectedProductSurface string
	ImportedBy             string
}

type ManifestStatusChangeInput struct {
	CapabilityID string
	Version      string
	ActorID      string
	Reason       string
}

func (uc *Usecases) ImportManifestSource(ctx context.Context, in ImportManifestSourceInput) ([]ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return nil, fmt.Errorf("capability repository is not configured")
	}
	if uc.sourceFetcher == nil {
		return nil, fmt.Errorf("capability manifest source fetcher is not configured")
	}
	sourceURL := strings.TrimSpace(in.SourceURL)
	if sourceURL == "" {
		return nil, fmt.Errorf("%w: source_url is required", ErrInvalidManifest)
	}
	manifests, err := uc.sourceFetcher.FetchManifests(ctx, ManifestSourceRequest{SourceURL: sourceURL})
	if err != nil {
		return nil, err
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("%w: manifest source returned no capabilities", ErrInvalidManifest)
	}
	expectedProductSurface := strings.TrimSpace(in.ExpectedProductSurface)
	normalized := make([]Manifest, 0, len(manifests))
	for _, manifest := range manifests {
		manifest = manifest.Normalize()
		if expectedProductSurface != "" && manifest.ProductSurface != expectedProductSurface {
			return nil, fmt.Errorf("%w: manifest product_surface %q does not match expected %q", ErrInvalidManifest, manifest.ProductSurface, expectedProductSurface)
		}
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		normalized = append(normalized, manifest)
	}
	reg, err := NewRegistry(normalized)
	if err != nil {
		return nil, err
	}
	records := make([]ManifestRecord, 0, len(normalized))
	for _, manifest := range reg.All() {
		record, err := uc.repo.UpsertManifest(ctx, ManifestRecord{
			Manifest:   manifest,
			Status:     ManifestStatusDraft,
			Source:     ManifestSourceURL,
			SourceURI:  sourceURL,
			ImportedBy: strings.TrimSpace(in.ImportedBy),
		})
		if err != nil {
			return nil, fmt.Errorf("import capability %s@%s from source: %w", manifest.CapabilityID, manifest.Version, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func (uc *Usecases) ListManifests(ctx context.Context, filter ManifestFilter) ([]ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return nil, fmt.Errorf("capability repository is not configured")
	}
	return uc.repo.ListManifests(ctx, filter)
}

func (uc *Usecases) GetManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	return uc.repo.GetManifest(ctx, capabilityID, version)
}

func (uc *Usecases) PromoteManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	return uc.PromoteManifestWithAudit(ctx, ManifestStatusChangeInput{
		CapabilityID: capabilityID,
		Version:      version,
	})
}

func (uc *Usecases) PromoteManifestWithAudit(ctx context.Context, in ManifestStatusChangeInput) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	record, err := uc.repo.GetManifest(ctx, in.CapabilityID, in.Version)
	if err != nil {
		return ManifestRecord{}, err
	}
	if record.Status == ManifestStatusBlocked {
		_, _ = uc.repo.SaveConformanceRun(ctx, ConformanceRun{
			CapabilityID: record.Manifest.CapabilityID,
			Version:      record.Manifest.Version,
			Status:       ConformanceStatusFailed,
			Checks:       map[string]bool{"blocked_promotion_rejected": false},
			Errors:       []string{"blocked capability manifests cannot be promoted without reimport"},
			Evidence: manifestTransitionEvidence(record, ManifestStatusActive, "promote", in.ActorID, in.Reason, map[string]any{
				"blocked_promotion_rejected": true,
				"evaluated_at":               time.Now().UTC().Format(time.RFC3339Nano),
			}),
			CreatedBy: strings.TrimSpace(in.ActorID),
		})
		return ManifestRecord{}, fmt.Errorf("%w: blocked capability manifests cannot be promoted", ErrInvalidManifest)
	}
	checks, errs := uc.checkManifestConformance(ctx, record.Manifest)
	if len(errs) > 0 {
		_, _ = uc.repo.SaveConformanceRun(ctx, ConformanceRun{
			CapabilityID: record.Manifest.CapabilityID,
			Version:      record.Manifest.Version,
			Status:       ConformanceStatusFailed,
			Checks:       checks,
			Errors:       errs,
			Evidence: manifestTransitionEvidence(record, ManifestStatusActive, "promote", in.ActorID, in.Reason, map[string]any{
				"promotion_blocked": true,
				"evaluated_at":      time.Now().UTC().Format(time.RFC3339Nano),
			}),
			CreatedBy: strings.TrimSpace(in.ActorID),
		})
		return ManifestRecord{}, fmt.Errorf("%w: conformance failed: %s", ErrInvalidManifest, strings.Join(errs, "; "))
	}
	if _, err := uc.repo.SaveConformanceRun(ctx, ConformanceRun{
		CapabilityID: record.Manifest.CapabilityID,
		Version:      record.Manifest.Version,
		Status:       ConformanceStatusPassed,
		Checks:       checks,
		Evidence: manifestTransitionEvidence(record, ManifestStatusActive, "promote", in.ActorID, in.Reason, map[string]any{
			"promotion_gate": true,
			"evaluated_at":   time.Now().UTC().Format(time.RFC3339Nano),
		}),
		CreatedBy: strings.TrimSpace(in.ActorID),
	}); err != nil {
		return ManifestRecord{}, err
	}
	return uc.repo.UpdateManifestStatus(ctx, in.CapabilityID, in.Version, ManifestStatusActive)
}

func (uc *Usecases) DeprecateManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	return uc.DeprecateManifestWithAudit(ctx, ManifestStatusChangeInput{
		CapabilityID: capabilityID,
		Version:      version,
	})
}

func (uc *Usecases) DeprecateManifestWithAudit(ctx context.Context, in ManifestStatusChangeInput) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	record, err := uc.repo.GetManifest(ctx, in.CapabilityID, in.Version)
	if err != nil {
		return ManifestRecord{}, err
	}
	updated, err := uc.repo.UpdateManifestStatus(ctx, in.CapabilityID, in.Version, ManifestStatusDeprecated)
	if err != nil {
		return ManifestRecord{}, err
	}
	_, _ = uc.repo.SaveConformanceRun(ctx, transitionRun(record, ManifestStatusDeprecated, "deprecate", in.ActorID, in.Reason))
	return updated, nil
}

func (uc *Usecases) BlockManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	return uc.BlockManifestWithAudit(ctx, ManifestStatusChangeInput{
		CapabilityID: capabilityID,
		Version:      version,
	})
}

func (uc *Usecases) BlockManifestWithAudit(ctx context.Context, in ManifestStatusChangeInput) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	record, err := uc.repo.GetManifest(ctx, in.CapabilityID, in.Version)
	if err != nil {
		return ManifestRecord{}, err
	}
	updated, err := uc.repo.UpdateManifestStatus(ctx, in.CapabilityID, in.Version, ManifestStatusBlocked)
	if err != nil {
		return ManifestRecord{}, err
	}
	_, _ = uc.repo.SaveConformanceRun(ctx, transitionRun(record, ManifestStatusBlocked, "block", in.ActorID, in.Reason))
	return updated, nil
}

type ConformanceInput struct {
	OrgID        string
	CapabilityID string
	Version      string
	Manifest     *Manifest
	CreatedBy    string
}

func (uc *Usecases) RunConformance(ctx context.Context, in ConformanceInput) (ConformanceRun, error) {
	if uc == nil || uc.repo == nil {
		return ConformanceRun{}, fmt.Errorf("capability repository is not configured")
	}
	var manifest Manifest
	if in.Manifest != nil {
		manifest = in.Manifest.Normalize()
	} else {
		record, err := uc.repo.GetManifest(ctx, in.CapabilityID, in.Version)
		if err != nil {
			return ConformanceRun{}, err
		}
		manifest = record.Manifest.Normalize()
	}
	checks, errs := uc.checkManifestConformance(ctx, manifest)
	status := ConformanceStatusPassed
	if len(errs) > 0 {
		status = ConformanceStatusFailed
	}
	run := ConformanceRun{
		OrgID:        strings.TrimSpace(in.OrgID),
		CapabilityID: manifest.CapabilityID,
		Version:      manifest.Version,
		Status:       status,
		Checks:       checks,
		Errors:       errs,
		Evidence: map[string]any{
			"schema_version":     manifest.SchemaVersion,
			"connector":          manifest.Connector,
			"risk_level":         manifest.RiskLevel,
			"side_effect_type":   manifest.SideEffectType,
			"nexus_action_type":  manifest.NexusActionType,
			"approval_required":  manifest.ApprovalRequired,
			"rollback_supported": manifest.RollbackSupported,
			"evaluated_at":       time.Now().UTC().Format(time.RFC3339Nano),
		},
		CreatedBy: strings.TrimSpace(in.CreatedBy),
	}
	return uc.repo.SaveConformanceRun(ctx, run)
}

func (uc *Usecases) ListConformanceRuns(ctx context.Context, orgID, capabilityID string, limit int) ([]ConformanceRun, error) {
	if uc == nil || uc.repo == nil {
		return nil, fmt.Errorf("capability repository is not configured")
	}
	return uc.repo.ListConformanceRuns(ctx, orgID, capabilityID, limit)
}

func (uc *Usecases) CheckConformance(ctx context.Context, manifest Manifest) (map[string]bool, []string) {
	return uc.checkManifestConformance(ctx, manifest)
}

func (uc *Usecases) checkManifestConformance(ctx context.Context, manifest Manifest) (map[string]bool, []string) {
	manifest = manifest.Normalize()
	checks, errs := CheckManifestConformance(manifest)
	if uc == nil || uc.productRegistry == nil {
		return checks, errs
	}
	checks["product_active"] = false
	productSurface := strings.TrimSpace(manifest.ProductSurface)
	if productSurface == "" {
		errs = append(errs, "product_surface is required")
		return checks, dedupeStrings(errs)
	}
	product, err := uc.productRegistry.GetProduct(ctx, productSurface)
	if err != nil {
		errs = append(errs, fmt.Sprintf("product_surface %q is not registered or active", productSurface))
		return checks, dedupeStrings(errs)
	}
	if product.Status != products.ProductStatusActive {
		errs = append(errs, fmt.Sprintf("product_surface %q is disabled", productSurface))
		return checks, dedupeStrings(errs)
	}
	checks["product_active"] = true
	return checks, dedupeStrings(errs)
}

func transitionRun(record ManifestRecord, toStatus, operation, actorID, reason string) ConformanceRun {
	return ConformanceRun{
		CapabilityID: record.Manifest.CapabilityID,
		Version:      record.Manifest.Version,
		Status:       ConformanceStatusPassed,
		Checks:       map[string]bool{"status_transition": true},
		Evidence: manifestTransitionEvidence(record, toStatus, operation, actorID, reason, map[string]any{
			"evaluated_at": time.Now().UTC().Format(time.RFC3339Nano),
		}),
		CreatedBy: strings.TrimSpace(actorID),
	}
}

func manifestTransitionEvidence(record ManifestRecord, toStatus, operation, actorID, reason string, extra map[string]any) map[string]any {
	evidence := map[string]any{
		"operation": operation,
		"status_transition": map[string]any{
			"from": strings.TrimSpace(record.Status),
			"to":   strings.TrimSpace(toStatus),
		},
		"impact":          "unknown",
		"impact_reason":   "active capability consumers are not persisted yet",
		"source":          strings.TrimSpace(record.Source),
		"source_uri":      strings.TrimSpace(record.SourceURI),
		"imported_by":     strings.TrimSpace(record.ImportedBy),
		"capability_id":   record.Manifest.CapabilityID,
		"version":         record.Manifest.Version,
		"product_surface": strings.TrimSpace(record.Manifest.ProductSurface),
	}
	if !record.CreatedAt.IsZero() {
		evidence["imported_at"] = record.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !record.UpdatedAt.IsZero() {
		evidence["last_updated_at"] = record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if actorID = strings.TrimSpace(actorID); actorID != "" {
		evidence["actor_id"] = actorID
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		evidence["reason"] = reason
	}
	for key, value := range extra {
		evidence[key] = value
	}
	return evidence
}
