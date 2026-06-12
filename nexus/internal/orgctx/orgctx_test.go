package orgctx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithRequestedAndRequested(t *testing.T) {
	t.Parallel()
	ctx := WithRequested(context.Background(), "  acme  ")
	if got := Requested(ctx); got != "acme" {
		t.Fatalf("Requested = %q, esperaba acme", got)
	}
	// Vacío no se guarda.
	empty := WithRequested(context.Background(), "   ")
	if got := Requested(empty); got != "" {
		t.Fatalf("Requested = %q, esperaba vacío", got)
	}
}

func TestNarrowedPrefersRequestedOverPrincipal(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithRequested(req.Context(), "acme"))
	if got := Narrowed(req, "globex"); got != "acme" {
		t.Fatalf("Narrowed = %q, esperaba acme (requested gana)", got)
	}
}

func TestNarrowedFallsBackToPrincipalOrg(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := Narrowed(req, "  globex "); got != "globex" {
		t.Fatalf("Narrowed = %q, esperaba globex (fallback al principal)", got)
	}
	if got := Narrowed(req, ""); got != "" {
		t.Fatalf("Narrowed = %q, esperaba vacío (sin acotar)", got)
	}
}
