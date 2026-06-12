package products

import (
	"context"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion/internal/secrets"
)

type Repository interface {
	UpsertProduct(ctx context.Context, product Product) (Product, error)
	GetProduct(ctx context.Context, productSurface string) (Product, error)
	ListProducts(ctx context.Context) ([]Product, error)
	UpsertInstallation(ctx context.Context, installation Installation) (Installation, error)
	GetInstallation(ctx context.Context, orgID, productSurface string) (Installation, error)
	ListInstallations(ctx context.Context, orgID string) ([]Installation, error)
	ListInstallationsByProduct(ctx context.Context, productSurface string) ([]Installation, error)
}

type Usecases struct {
	repo           Repository
	secretResolver secrets.Resolver
}

func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

func (u *Usecases) WithSecretResolver(resolver secrets.Resolver) *Usecases {
	u.secretResolver = resolver
	return u
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

// ListInstallationsByProduct lista las instalaciones de un producto a través
// de todas las orgs. Lo usa el wiring del ProductConnector genérico para
// detectar productos con instalaciones `connector_mode=envelope.v1` y para
// resolver la instalación de discovery del manifest.
func (u *Usecases) ListInstallationsByProduct(ctx context.Context, productSurface string) ([]Installation, error) {
	productSurface = normalizeProductSurface(productSurface)
	if !validProductSurface(productSurface) {
		return nil, fmt.Errorf("%w: valid product_surface is required", ErrValidation)
	}
	return u.repo.ListInstallationsByProduct(ctx, productSurface)
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

func (u *Usecases) ResolveInstallationSecret(ctx context.Context, orgID, productSurface string) (secrets.Secret, error) {
	installation, err := u.ResolveInstallation(ctx, orgID, productSurface)
	if err != nil {
		return secrets.Secret{}, err
	}
	if installation.SecretRef == "" {
		return secrets.Secret{}, fmt.Errorf("%w: installation has no secret_ref", ErrValidation)
	}
	if u.secretResolver == nil {
		return secrets.Secret{}, ErrSecretResolverMissing
	}
	return u.secretResolver.Resolve(ctx, installation.SecretRef)
}
