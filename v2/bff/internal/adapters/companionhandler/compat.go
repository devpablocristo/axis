// Package companionhandler preserves legacy handler composition without
// making the inbound application package depend on a concrete adapter.
package companionhandler

import (
	"net/http"

	"github.com/devpablocristo/bff-v2/internal/adapters/companionhttp"
	"github.com/devpablocristo/bff-v2/internal/inbound"
	"github.com/devpablocristo/bff-v2/internal/productedge"
)

func NewLegacyHandler(
	bindings map[string]inbound.Binding,
	companionBaseURL, internalAuthSecret string,
	client *http.Client,
) *inbound.Handler {
	adapter := companionhttp.New(companionBaseURL, internalAuthSecret, client)
	return inbound.NewHandlerWithPorts(nil, bindings, productedge.Ports{
		StartAssist:         adapter,
		GetAssistRun:        adapter,
		PublishProductEvent: adapter,
		ResolveRouting:      adapter,
	}, inbound.HandlerOptions{
		AllowLegacyBindings: true,
		RouteSigningSecret:  internalAuthSecret,
	})
}

func NewGovernedHandler(
	authenticator productedge.ProductAuthenticator,
	legacyBindings map[string]inbound.Binding,
	companionBaseURL, internalAuthSecret string,
	client *http.Client,
	options inbound.HandlerOptions,
) *inbound.Handler {
	adapter := companionhttp.New(companionBaseURL, internalAuthSecret, client)
	return inbound.NewHandlerWithPorts(authenticator, legacyBindings, productedge.Ports{
		StartAssist:         adapter,
		GetAssistRun:        adapter,
		PublishProductEvent: adapter,
		ResolveRouting:      adapter,
	}, options)
}
