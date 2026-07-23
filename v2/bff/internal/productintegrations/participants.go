package productintegrations

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// IntegrationParticipant is the functional boundary implemented by every
// subsystem that participates in integration validation and activation.
// Application code does not know participant URLs or transport DTOs.
type IntegrationParticipant interface {
	Name() string
	Project(Contract) (ServiceSection, bool, error)
	Prepare(context.Context, ServicePrepareInput) (ServiceSnapshot, error)
	Validate(context.Context, ServiceMutationInput) (ServiceSnapshot, error)
	Activate(context.Context, ServiceMutationInput) error
	Readiness(context.Context, ServiceReadinessInput) (ServiceReadiness, error)
}

type ParticipantRegistry struct {
	participants        map[string]IntegrationParticipant
	invocationProjector ContractProjection
}

func NewParticipantRegistry(participants ...IntegrationParticipant) ParticipantRegistry {
	registry := ParticipantRegistry{participants: make(map[string]IntegrationParticipant, len(participants))}
	for _, participant := range participants {
		if participant == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(participant.Name()))
		if name == "" {
			continue
		}
		registry.participants[name] = participant
	}
	return registry
}

func (r ParticipantRegistry) WithInvocationProjection(projector ContractProjection) ParticipantRegistry {
	r.invocationProjector = projector
	return r
}

func (r ParticipantRegistry) Participant(name string) (IntegrationParticipant, bool) {
	participant, ok := r.participants[strings.ToLower(strings.TrimSpace(name))]
	return participant, ok
}

func (r ParticipantRegistry) Names() []string {
	names := make([]string, 0, len(r.participants))
	for name := range r.participants {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r ParticipantRegistry) Required(contract Contract) ([]string, error) {
	if contract.SchemaVersion == SchemaVersion {
		return append([]string(nil), contract.RequiredServices...), nil
	}
	names := []string{"bff"}
	for _, name := range r.Names() {
		_, applies, err := r.participants[name].Project(contract)
		if err != nil {
			return nil, err
		}
		if applies {
			names = append(names, name)
		}
	}
	return names, nil
}

func (r ParticipantRegistry) InvocationSection(contract Contract) (ServiceSection, bool, error) {
	if r.invocationProjector == nil {
		return ServiceSection{}, false, nil
	}
	return r.invocationProjector(contract)
}

type ContractProjection func(Contract) (ServiceSection, bool, error)

type httpParticipant struct {
	name       string
	baseURL    string
	client     ServiceClient
	projection ContractProjection
}

func NewHTTPParticipant(name, baseURL string, client ServiceClient) IntegrationParticipant {
	name = strings.ToLower(strings.TrimSpace(name))
	return NewHTTPParticipantWithProjection(name, baseURL, client, func(contract Contract) (ServiceSection, bool, error) {
		section, ok := contract.Services[name]
		return section, ok, nil
	})
}

func NewHTTPParticipantWithProjection(
	name, baseURL string,
	client ServiceClient,
	projection ContractProjection,
) IntegrationParticipant {
	return &httpParticipant{
		name:       strings.ToLower(strings.TrimSpace(name)),
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:     client,
		projection: projection,
	}
}

func (p *httpParticipant) Name() string { return p.name }

func (p *httpParticipant) Project(contract Contract) (ServiceSection, bool, error) {
	if p.projection == nil {
		return ServiceSection{}, false, nil
	}
	return p.projection(contract)
}

func (p *httpParticipant) Prepare(ctx context.Context, input ServicePrepareInput) (ServiceSnapshot, error) {
	if p.client == nil || p.baseURL == "" {
		return ServiceSnapshot{}, errors.New("integration participant is unavailable")
	}
	input.Service, input.BaseURL = p.name, p.baseURL
	return p.client.Prepare(ctx, input)
}

func (p *httpParticipant) Validate(ctx context.Context, input ServiceMutationInput) (ServiceSnapshot, error) {
	if p.client == nil || p.baseURL == "" {
		return ServiceSnapshot{}, errors.New("integration participant is unavailable")
	}
	input.Service, input.BaseURL = p.name, p.baseURL
	return p.client.Validate(ctx, input)
}

func (p *httpParticipant) Activate(ctx context.Context, input ServiceMutationInput) error {
	if p.client == nil || p.baseURL == "" {
		return errors.New("integration participant is unavailable")
	}
	input.Service, input.BaseURL = p.name, p.baseURL
	return p.client.Activate(ctx, input)
}

func (p *httpParticipant) Readiness(ctx context.Context, input ServiceReadinessInput) (ServiceReadiness, error) {
	if p.client == nil || p.baseURL == "" {
		return ServiceReadiness{Service: p.name, Status: "unavailable"}, errors.New("integration participant is unavailable")
	}
	input.Service, input.BaseURL = p.name, p.baseURL
	return p.client.Readiness(ctx, input)
}

var _ IntegrationParticipant = (*httpParticipant)(nil)
