package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devpablocristo/companion-v2/internal/nexusclient"
	"github.com/devpablocristo/companion-v2/internal/outbox"
	"github.com/google/uuid"
)

type nexusOutboxClient interface {
	ReportExecutionResult(context.Context, string, string, string, string, string, int64, map[string]any, string, string, string) error
	AppendAuditEventIdempotent(context.Context, string, string, nexusclient.AuditEvent) error
}

func newNexusOutboxSender(client nexusOutboxClient) outbox.Sender {
	return outbox.SenderFunc(func(ctx context.Context, message outbox.Message) error {
		switch {
		case message.AggregateType == outbox.AggregateTypeExecutionAttempt && message.Kind == outbox.KindExecutionResult:
			return sendNexusExecutionResult(ctx, client, message)
		case message.AggregateType == outbox.AggregateTypeProfessionalAuthority && message.Kind == outbox.KindAuditEvent:
			return sendNexusAuthorityAudit(ctx, client, message)
		default:
			return outbox.Permanent("unsupported_outbox_message", fmt.Errorf("unsupported aggregate and kind"))
		}
	})
}

func sendNexusExecutionResult(ctx context.Context, client nexusOutboxClient, message outbox.Message) error {
	var payload outbox.NexusExecutionResult
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return outbox.Permanent("invalid_outbox_payload", err)
	}
	if payload.GovernanceCheckID == "" || payload.IdempotencyKey == "" || payload.BindingHash == "" || payload.Status == "" {
		return outbox.Permanent("invalid_outbox_payload", fmt.Errorf("required delivery metadata is missing"))
	}
	if err := client.ReportExecutionResult(ctx, message.TenantID, payload.GovernanceCheckID, payload.IdempotencyKey, payload.BindingHash, payload.Status, payload.DurationMS, payload.Result, payload.AttestationVersion, payload.ExecutorVersion, payload.Attestation); err != nil {
		return outbox.Retryable("nexus_unavailable", err)
	}
	return nil
}

func sendNexusAuthorityAudit(ctx context.Context, client nexusOutboxClient, message outbox.Message) error {
	payload, err := outbox.ParseNexusAuditEvent(message.Payload, message.AggregateID)
	if err != nil {
		return outbox.Permanent("invalid_outbox_payload", err)
	}
	if message.ID == uuid.Nil || strings.TrimSpace(message.TenantID) == "" {
		return outbox.Permanent("invalid_outbox_payload", fmt.Errorf("professional authority audit metadata is invalid"))
	}
	event := nexusclient.AuditEvent{
		VirployeeID: payload.VirployeeID,
		ActorType:   payload.ActorType,
		ActorID:     payload.ActorID,
		SubjectType: payload.SubjectType,
		SubjectID:   payload.SubjectID,
		EventType:   payload.EventType,
		Summary:     payload.Summary,
		Data: map[string]any{
			"revision":      payload.Revision,
			"snapshot_hash": payload.SnapshotHash,
		},
	}
	if err := client.AppendAuditEventIdempotent(ctx, message.TenantID, message.ID.String(), event); err != nil {
		if nexusclient.IsPermanentHTTPError(err) {
			return outbox.Permanent("nexus_rejected", err)
		}
		return outbox.Retryable("nexus_unavailable", err)
	}
	return nil
}
