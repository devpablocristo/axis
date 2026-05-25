package capabilities

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
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
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	record, err := uc.repo.GetManifest(ctx, capabilityID, version)
	if err != nil {
		return ManifestRecord{}, err
	}
	checks, errs := CheckManifestConformance(record.Manifest)
	if len(errs) > 0 {
		_, _ = uc.repo.SaveConformanceRun(ctx, ConformanceRun{
			CapabilityID: record.Manifest.CapabilityID,
			Version:      record.Manifest.Version,
			Status:       ConformanceStatusFailed,
			Checks:       checks,
			Errors:       errs,
			Evidence: map[string]any{
				"promotion_blocked": true,
				"evaluated_at":      time.Now().UTC().Format(time.RFC3339Nano),
			},
		})
		return ManifestRecord{}, fmt.Errorf("%w: conformance failed: %s", ErrInvalidManifest, strings.Join(errs, "; "))
	}
	if _, err := uc.repo.SaveConformanceRun(ctx, ConformanceRun{
		CapabilityID: record.Manifest.CapabilityID,
		Version:      record.Manifest.Version,
		Status:       ConformanceStatusPassed,
		Checks:       checks,
		Evidence: map[string]any{
			"promotion_gate": true,
			"evaluated_at":   time.Now().UTC().Format(time.RFC3339Nano),
		},
	}); err != nil {
		return ManifestRecord{}, err
	}
	return uc.repo.UpdateManifestStatus(ctx, capabilityID, version, ManifestStatusActive)
}

func (uc *Usecases) DeprecateManifest(ctx context.Context, capabilityID, version string) (ManifestRecord, error) {
	if uc == nil || uc.repo == nil {
		return ManifestRecord{}, fmt.Errorf("capability repository is not configured")
	}
	return uc.repo.UpdateManifestStatus(ctx, capabilityID, version, ManifestStatusDeprecated)
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
	checks, errs := CheckManifestConformance(manifest)
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
