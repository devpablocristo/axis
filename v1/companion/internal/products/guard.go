package products

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const InternalProductSurface = "companion"

type InstallationResolver interface {
	ResolveInstallation(ctx context.Context, orgID, productSurface string) (Installation, error)
}

type GuardrailEvent struct {
	OrgID          string
	ProductSurface string
	Reason         string
	Error          string
}

type GuardrailRecorder interface {
	RecordProductInstallationGuardrail(ctx context.Context, event GuardrailEvent) error
}

type InstallationGuard struct {
	resolver InstallationResolver
	recorder GuardrailRecorder
}

func NewInstallationGuard(resolver InstallationResolver) *InstallationGuard {
	return &InstallationGuard{resolver: resolver}
}

func (g *InstallationGuard) WithRecorder(recorder GuardrailRecorder) *InstallationGuard {
	if g != nil {
		g.recorder = recorder
	}
	return g
}

func (g *InstallationGuard) RequireActiveInstallation(ctx context.Context, orgID, productSurface, reason string) error {
	productSurface = normalizeProductSurface(productSurface)
	reason = strings.TrimSpace(reason)
	if productSurface == "" {
		err := fmt.Errorf("%w: product_surface is required", ErrValidation)
		g.record(ctx, orgID, productSurface, reason, err)
		return err
	}
	if productSurface == InternalProductSurface {
		return nil
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		err := fmt.Errorf("%w: org_id is required", ErrValidation)
		g.record(ctx, orgID, productSurface, reason, err)
		return err
	}
	if g == nil || g.resolver == nil {
		err := fmt.Errorf("%w: installation resolver is not configured", ErrInstallationRequired)
		g.record(ctx, orgID, productSurface, reason, err)
		return err
	}
	if _, err := g.resolver.ResolveInstallation(ctx, orgID, productSurface); err != nil {
		err = errors.Join(ErrInstallationRequired, err)
		g.record(ctx, orgID, productSurface, reason, err)
		return err
	}
	return nil
}

func (g *InstallationGuard) record(ctx context.Context, orgID, productSurface, reason string, err error) {
	if g == nil || g.recorder == nil || err == nil {
		return
	}
	_ = g.recorder.RecordProductInstallationGuardrail(ctx, GuardrailEvent{
		OrgID:          strings.TrimSpace(orgID),
		ProductSurface: normalizeProductSurface(productSurface),
		Reason:         strings.TrimSpace(reason),
		Error:          err.Error(),
	})
}

func IsInstallationGuardError(err error) bool {
	return errors.Is(err, ErrInstallationRequired) ||
		errors.Is(err, ErrInstallationNotFound) ||
		errors.Is(err, ErrInstallationDisabled) ||
		errors.Is(err, ErrProductDisabled)
}
