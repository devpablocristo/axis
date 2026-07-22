package wire

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/google/uuid"
)

type fakeNexusOutboxClient struct {
	executionCalls int
	auditCalls     int
	auditTenant    string
	auditKey       string
	auditEvent     nexusclient.AuditEvent
	auditErr       error
}

func (f *fakeNexusOutboxClient) ReportExecutionResult(context.Context, string, string, string, string, string, int64, map[string]any, string, string, string) error {
	f.executionCalls++
	return nil
}

func (f *fakeNexusOutboxClient) AppendAuditEventIdempotent(_ context.Context, tenantID, key string, event nexusclient.AuditEvent) error {
	f.auditCalls++
	f.auditTenant, f.auditKey, f.auditEvent = tenantID, key, event
	return f.auditErr
}

func TestNexusOutboxSenderDeliversMetadataOnlyAuthorityAudit(t *testing.T) {
	client := &fakeNexusOutboxClient{}
	messageID, subjectID := uuid.New(), uuid.New()
	payload, err := json.Marshal(outbox.NexusAuditEvent{
		VirployeeID: uuid.NewString(), ActorType: "human", ActorID: "owner-1",
		SubjectType: "delegation", SubjectID: subjectID.String(), EventType: "delegation_revoked",
		Summary: "professional delegation revoked", Revision: 2, SnapshotHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := newNexusOutboxSender(client)
	err = sender.Send(context.Background(), outbox.Message{
		ID: messageID, TenantID: "tenant-1", AggregateType: outbox.AggregateTypeProfessionalAuthority,
		AggregateID: subjectID, Kind: outbox.KindAuditEvent, Payload: payload,
	})
	if err != nil {
		t.Fatalf("send audit: %v", err)
	}
	if client.auditCalls != 1 || client.executionCalls != 0 || client.auditTenant != "tenant-1" || client.auditKey != messageID.String() {
		t.Fatalf("unexpected routing: %+v", client)
	}
	if len(client.auditEvent.Data) != 2 || client.auditEvent.Data["revision"] != int64(2) || client.auditEvent.Data["snapshot_hash"] != strings.Repeat("a", 64) {
		t.Fatalf("sender must construct the fixed metadata-only data map: %+v", client.auditEvent.Data)
	}
}

func TestNexusOutboxSenderRejectsUnknownAuditFieldsAndCrossedPair(t *testing.T) {
	client := &fakeNexusOutboxClient{}
	subjectID := uuid.New()
	raw := json.RawMessage(`{
		"virployee_id":"service:professional-authority","actor_type":"human","actor_id":"owner-1",
		"subject_type":"scope_policy","subject_id":"` + subjectID.String() + `",
		"event_type":"scope_policy_changed","summary":"professional scope policy changed",
		"revision":1,"snapshot_hash":"` + strings.Repeat("b", 64) + `","policy_text":"sensitive"
	}`)
	sender := newNexusOutboxSender(client)
	if err := sender.Send(context.Background(), outbox.Message{
		ID: uuid.New(), TenantID: "tenant-1", AggregateType: outbox.AggregateTypeProfessionalAuthority,
		AggregateID: subjectID, Kind: outbox.KindAuditEvent, Payload: raw,
	}); err == nil {
		t.Fatal("unknown audit payload fields must be rejected")
	}
	if err := sender.Send(context.Background(), outbox.Message{
		ID: uuid.New(), TenantID: "tenant-1", AggregateType: outbox.AggregateTypeExecutionAttempt,
		AggregateID: subjectID, Kind: outbox.KindAuditEvent, Payload: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatal("crossed aggregate/kind pair must be rejected")
	}
	if client.auditCalls != 0 || client.executionCalls != 0 {
		t.Fatalf("invalid messages must never reach Nexus: %+v", client)
	}
}

func TestNexusOutboxSenderKeepsExecutionResultRoute(t *testing.T) {
	client := &fakeNexusOutboxClient{}
	payload, _ := json.Marshal(outbox.NexusExecutionResult{
		GovernanceCheckID: "check-1", IdempotencyKey: "execution-1", BindingHash: "binding-1", Status: "succeeded",
	})
	sender := newNexusOutboxSender(client)
	if err := sender.Send(context.Background(), outbox.Message{
		ID: uuid.New(), TenantID: "tenant-1", AggregateType: outbox.AggregateTypeExecutionAttempt,
		AggregateID: uuid.New(), Kind: outbox.KindExecutionResult, Payload: payload,
	}); err != nil {
		t.Fatalf("send execution result: %v", err)
	}
	if client.executionCalls != 1 || client.auditCalls != 0 {
		t.Fatalf("execution result must keep its existing route: %+v", client)
	}
}

func TestNexusOutboxSenderDoesNotRetryRejectedAudit(t *testing.T) {
	client := &fakeNexusOutboxClient{auditErr: &nexusclient.HTTPStatusError{Operation: "append audit event", StatusCode: 409}}
	subjectID := uuid.New()
	payload, _ := json.Marshal(outbox.NexusAuditEvent{
		VirployeeID: "service:professional-authority", ActorType: "human", ActorID: "owner-1",
		SubjectType: "professional_policy_pack", SubjectID: subjectID.String(), EventType: "professional_policy_pack_created",
		Summary: "professional policy pack created", Revision: 1, SnapshotHash: strings.Repeat("c", 64),
	})
	err := newNexusOutboxSender(client).Send(context.Background(), outbox.Message{
		ID: uuid.New(), TenantID: "tenant-1", AggregateType: outbox.AggregateTypeProfessionalAuthority,
		AggregateID: subjectID, Kind: outbox.KindAuditEvent, Payload: payload,
	})
	if err == nil || !errors.Is(err, client.auditErr) {
		t.Fatalf("Nexus rejection must be returned as a delivery error, got %v", err)
	}
}
