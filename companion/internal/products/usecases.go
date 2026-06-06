package products

import (
	"context"
	"fmt"
	"strings"
)

type Repository interface {
	UpsertProduct(ctx context.Context, product Product) (Product, error)
	GetProduct(ctx context.Context, productSurface string) (Product, error)
	ListProducts(ctx context.Context) ([]Product, error)
	UpsertInstallation(ctx context.Context, installation Installation) (Installation, error)
	GetInstallation(ctx context.Context, orgID, productSurface string) (Installation, error)
	ListInstallations(ctx context.Context, orgID string) ([]Installation, error)
}

type Usecases struct {
	repo Repository
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) SaveProduct(ctx context.Context, product Product) (Product, error) {
	product = normalizeProduct(product)
	if err := validateProduct(product); err != nil {
		return Product{}, err
	}
	return u.repo.UpsertProduct(ctx, product)
}

func (u *Usecases) GetProduct(ctx context.Context, productSurface string) (Product, error) {
	productSurface = normalizeProductSurface(productSurface)
	if !validProductSurface(productSurface) {
		return Product{}, fmt.Errorf("%w: valid product_surface is required", ErrValidation)
	}
	return u.repo.GetProduct(ctx, productSurface)
}

func (u *Usecases) ListProducts(ctx context.Context) ([]Product, error) {
	return u.repo.ListProducts(ctx)
}

func (u *Usecases) SaveInstallation(ctx context.Context, installation Installation) (Installation, error) {
	installation = normalizeInstallation(installation)
	if err := validateInstallation(installation); err != nil {
		return Installation{}, err
	}
	product, err := u.repo.GetProduct(ctx, installation.ProductSurface)
	if err != nil {
		return Installation{}, err
	}
	if product.Status != ProductStatusActive {
		return Installation{}, ErrProductDisabled
	}
	return u.repo.UpsertInstallation(ctx, installation)
}

func (u *Usecases) GetInstallation(ctx context.Context, orgID, productSurface string) (Installation, error) {
	orgID = strings.TrimSpace(orgID)
	productSurface = normalizeProductSurface(productSurface)
	if orgID == "" {
		return Installation{}, fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	if !validProductSurface(productSurface) {
		return Installation{}, fmt.Errorf("%w: valid product_surface is required", ErrValidation)
	}
	return u.repo.GetInstallation(ctx, orgID, productSurface)
}

func (u *Usecases) ListInstallations(ctx context.Context, orgID string) ([]Installation, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("%w: org_id is required", ErrValidation)
	}
	return u.repo.ListInstallations(ctx, orgID)
}

func (u *Usecases) ResolveInstallation(ctx context.Context, orgID, productSurface string) (Installation, error) {
	installation, err := u.GetInstallation(ctx, orgID, productSurface)
	if err != nil {
		return Installation{}, err
	}
	if !installation.Enabled {
		return Installation{}, ErrInstallationDisabled
	}
	product, err := u.repo.GetProduct(ctx, installation.ProductSurface)
	if err != nil {
		return Installation{}, err
	}
	if product.Status != ProductStatusActive {
		return Installation{}, ErrProductDisabled
	}
	return installation, nil
}
