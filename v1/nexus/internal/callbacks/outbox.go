package callbacks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type OutboxApprovalPublisher struct {
	db       *sharedpostgres.DB
	token    string
	targets  map[string][]string
	client   *http.Client
	workerID string
}

func NewOutboxApprovalPublisher(db *sharedpostgres.DB, token string, pendingURLs, resolvedURLs []string) *OutboxApprovalPublisher {
	return &OutboxApprovalPublisher{
		db:       db,
		token:    strings.TrimSpace(token),
		workerID: "callback-worker-" + uuid.NewString(),
		targets: map[string][]string{
			EventApprovalPending:  compactURLs(pendingURLs),
			EventApprovalResolved: compactURLs(resolvedURLs),
		},
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *OutboxApprovalPublisher) Publish(ctx context.Context, event ApprovalEvent) error {
	urls := p.targets[event.Event]
	if len(urls) == 0 {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal callback event: %w", err)
	}
	idempotencyKey := callbackIdempotencyKey(event)
	tx, err := p.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin callback outbox: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var outboxID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO nexus_outbox_events
			(org_id, event_type, subject_type, subject_id, payload, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now()
		RETURNING id
	`, emptyStringToNil(event.OrgID), event.Event, "approval", firstNonEmpty(event.ApprovalID, event.RequestID), string(payload), idempotencyKey).Scan(&outboxID)
	if err != nil {
		return fmt.Errorf("insert callback outbox event: %w", err)
	}
	for _, targetURL := range urls {
		if _, err := tx.Exec(ctx, `
			INSERT INTO nexus_callback_deliveries (outbox_event_id, target_url)
			VALUES ($1,$2)
			ON CONFLICT (outbox_event_id, target_url) DO NOTHING
		`, outboxID, targetURL); err != nil {
			return fmt.Errorf("insert callback delivery: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit callback outbox: %w", err)
	}
	return nil
}

func (p *OutboxApprovalPublisher) StartWorker(ctx context.Context, interval time.Duration, batchSize int) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 25
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				delivered, err := p.DeliverDue(ctx, batchSize)
				if err != nil {
					slog.Error("callback outbox delivery failed", "error", err)
					continue
				}
				if delivered > 0 {
					slog.Info("callback outbox delivered batch", "count", delivered)
				}
			}
		}
	}()
}

func (p *OutboxApprovalPublisher) DeliverDue(ctx context.Context, limit int) (int, error) {
	type dueDelivery struct {
		id          uuid.UUID
		orgID       *string
		attempts    int
		maxAttempts int
		targetURL   string
		payload     []byte
	}
	tx, err := p.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin callback delivery claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		WITH due AS (
			SELECT d.id
			FROM nexus_callback_deliveries d
			JOIN nexus_outbox_events e ON e.id = d.outbox_event_id
			WHERE d.status IN ('pending', 'delivering')
			  AND d.next_attempt_at <= now()
			  AND (d.leased_until IS NULL OR d.leased_until < now())
			ORDER BY d.created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE nexus_callback_deliveries d
		SET status = 'delivering',
		    lease_owner = $2,
		    leased_until = now() + ($3::int * interval '1 second'),
		    last_attempt_at = now(),
		    updated_at = now()
		FROM due, nexus_outbox_events e
		WHERE d.id = due.id
		  AND e.id = d.outbox_event_id
		RETURNING d.id, e.org_id, d.attempts, d.max_attempts, d.target_url, e.payload
	`, limit, p.workerID, 60)
	if err != nil {
		return 0, fmt.Errorf("claim due callback deliveries: %w", err)
	}
	defer rows.Close()

	due := make([]dueDelivery, 0)
	for rows.Next() {
		var item dueDelivery
		if err := rows.Scan(&item.id, &item.orgID, &item.attempts, &item.maxAttempts, &item.targetURL, &item.payload); err != nil {
			return 0, fmt.Errorf("scan due callback delivery: %w", err)
		}
		due = append(due, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit callback delivery claim: %w", err)
	}
	delivered := 0
	for _, item := range due {
		ok, responseStatus, responseHash, deliveryErr := p.deliverOne(ctx, item.targetURL, item.payload)
		if ok {
			delivered++
			_, err = p.db.Pool().Exec(ctx, `
				UPDATE nexus_callback_deliveries
				SET status = 'delivered', attempts = attempts + 1, response_status = $2,
				    response_body_hash = $3, delivered_at = now(), updated_at = now(), last_error = NULL,
				    lease_owner = NULL, leased_until = NULL
				WHERE id = $1 AND lease_owner = $4
			`, item.id, responseStatus, responseHash, p.workerID)
			if err != nil {
				return delivered, fmt.Errorf("mark callback delivered: %w", err)
			}
			p.recordDeliveryEvent(ctx, item.id, item.orgID, "delivered", map[string]any{
				"response_status": responseStatus,
				"response_hash":   responseHash,
			})
			continue
		}
		attempts := item.attempts + 1
		status := "pending"
		if attempts >= item.maxAttempts {
			status = "dead"
		}
		_, err = p.db.Pool().Exec(ctx, `
			UPDATE nexus_callback_deliveries
			SET status = $2, attempts = attempts + 1, response_status = $3,
			    response_body_hash = $4, last_error = $5, next_attempt_at = $6, updated_at = now(),
			    lease_owner = NULL, leased_until = NULL
			WHERE id = $1 AND lease_owner = $7
		`, item.id, status, responseStatus, responseHash, deliveryErr, nextAttemptAt(attempts), p.workerID)
		if err != nil {
			return delivered, fmt.Errorf("mark callback failed: %w", err)
		}
		p.recordDeliveryEvent(ctx, item.id, item.orgID, status, map[string]any{
			"response_status": responseStatus,
			"response_hash":   responseHash,
			"error":           deliveryErr,
			"attempts":        attempts,
		})
	}
	return delivered, nil
}

func (p *OutboxApprovalPublisher) recordDeliveryEvent(ctx context.Context, deliveryID uuid.UUID, orgID *string, eventType string, data map[string]any) {
	if data == nil {
		data = make(map[string]any)
	}
	if _, err := p.db.Pool().Exec(ctx, `
		INSERT INTO nexus_callback_delivery_events (delivery_id, org_id, event_type, actor_id, data)
		VALUES ($1,$2,$3,$4,$5)
	`, deliveryID, orgID, eventType, p.workerID, data); err != nil {
		slog.Error("record callback delivery event failed", "error", err, "delivery_id", deliveryID)
	}
}

func (p *OutboxApprovalPublisher) deliverOne(ctx context.Context, targetURL string, payload []byte) (bool, int, string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		return false, 0, "", err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Callback-Timestamp", time.Now().UTC().Format(time.RFC3339Nano))
	if p.token != "" {
		req.Header.Set("X-Nexus-Callback-Signature", signCallback(p.token, req.Header.Get("X-Nexus-Callback-Timestamp"), payload))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false, 0, "", err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyHash := hashBytes(body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, resp.StatusCode, bodyHash, ""
	}
	return false, resp.StatusCode, bodyHash, fmt.Sprintf("callback returned HTTP %d", resp.StatusCode)
}

func callbackIdempotencyKey(event ApprovalEvent) string {
	parts := []string{event.Event, event.OrgID, event.RequestID, event.ApprovalID, event.Decision}
	sum := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(sum[:])
}

func compactURLs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func nextAttemptAt(attempts int) time.Time {
	if attempts < 0 {
		attempts = 0
	}
	delay := time.Duration(1<<min(attempts, 8)) * time.Second
	return time.Now().UTC().Add(delay)
}

func signCallback(token, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func emptyStringToNil(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
