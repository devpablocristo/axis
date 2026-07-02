// Package productctx carries the principal's product surface (the "product" half
// of a tenant = org x product) through the request context, so governance
// scope-helpers can partition data per product within an org — mirroring how
// orgctx carries the org. The value comes from the verified JWT claim
// "product_surface" (set by the auth middleware), NOT from a caller header, so
// it is not an escalation channel: a principal cannot widen its product scope.
package productctx

import (
	"context"
	"net/http"
	"strings"
)

type contextKey struct{}

// WithProduct stores the principal's product surface in the context. Empty
// values are not stored (=> unscoped / all products).
func WithProduct(ctx context.Context, product string) context.Context {
	product = strings.TrimSpace(product)
	if product == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, product)
}

// Product returns the principal's product surface, or "" when unscoped.
func Product(ctx context.Context) string {
	value, _ := ctx.Value(contextKey{}).(string)
	return value
}

// FromRequest is the HTTP helper.
func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return Product(r.Context())
}
