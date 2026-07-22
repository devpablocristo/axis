package enterpriseops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NotificationDestinationResolver interface {
	ResolveNotificationDestination(context.Context, string) ([]byte, error)
}

type NotificationSender interface {
	SendNotification(context.Context, []byte, json.RawMessage) error
}

type HTTPNotificationSender struct {
	client    *http.Client
	allowHTTP bool
}

func NewHTTPNotificationSender(client *http.Client, allowHTTP bool) *HTTPNotificationSender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPNotificationSender{client: client, allowHTTP: allowHTTP}
}

func (s *HTTPNotificationSender) SendNotification(ctx context.Context, destination []byte, payload json.RawMessage) error {
	parsed, err := url.Parse(strings.TrimSpace(string(destination)))
	if err != nil {
		return errors.New("notification destination is invalid")
	}
	allowedScheme := parsed.Scheme == "https" || (s.allowHTTP && parsed.Scheme == "http")
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || !allowedScheme {
		return errors.New("notification destination is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "axis-nexus-operations/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint status %d", resp.StatusCode)
	}
	return nil
}

type notificationDelivery struct {
	id        string
	tenant    string
	reference string
	payload   json.RawMessage
	attempts  int
}

func (s *Service) ConfigureNotificationDelivery(resolver NotificationDestinationResolver, sender NotificationSender) {
	s.notificationResolver = resolver
	s.notificationSender = sender
}

func (s *Service) DeliverNotifications(ctx context.Context, limit int) (int, error) {
	if s.notificationResolver == nil || s.notificationSender == nil {
		return 0, nil
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `WITH picked AS(
		SELECT outbox.id FROM operational_notification_outbox AS outbox
		JOIN operational_notification_policy AS policy ON policy.tenant_id=outbox.tenant_id AND policy.enabled=true
		WHERE outbox.attempts<10 AND (
			(outbox.status='pending' AND outbox.available_at<=now()) OR
			(outbox.status='processing' AND outbox.lease_until<now())
		) ORDER BY outbox.available_at,outbox.created_at,outbox.id LIMIT $1 FOR UPDATE OF outbox SKIP LOCKED
	) UPDATE operational_notification_outbox AS outbox
	SET status='processing',attempts=attempts+1,lease_until=now()+interval '1 minute',updated_at=now()
	FROM picked,operational_notification_policy AS policy
	WHERE outbox.id=picked.id AND policy.tenant_id=outbox.tenant_id
	RETURNING outbox.id::text,outbox.tenant_id,policy.webhook_secret_ref,outbox.payload_json,outbox.attempts`, limit)
	if err != nil {
		return 0, err
	}
	deliveries := []notificationDelivery{}
	for rows.Next() {
		var item notificationDelivery
		if err = rows.Scan(&item.id, &item.tenant, &item.reference, &item.payload, &item.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		deliveries = append(deliveries, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}

	delivered := 0
	for _, item := range deliveries {
		code := "destination_unavailable"
		destination, resolveErr := s.notificationResolver.ResolveNotificationDestination(ctx, item.reference)
		deliveryErr := resolveErr
		if deliveryErr == nil {
			code = "webhook_unavailable"
			deliveryErr = s.notificationSender.SendNotification(ctx, destination, item.payload)
		}
		for index := range destination {
			destination[index] = 0
		}
		if deliveryErr == nil {
			tag, updateErr := s.pool.Exec(ctx, `UPDATE operational_notification_outbox SET status='delivered',lease_until=NULL,last_error_code='',updated_at=now() WHERE id=$1 AND status='processing'`, item.id)
			if updateErr != nil {
				return delivered, updateErr
			}
			if tag.RowsAffected() > 0 {
				delivered++
			}
			continue
		}
		backoff := 1 << min(item.attempts, 8)
		status := "pending"
		if item.attempts >= 10 {
			status = "dead"
		}
		if _, updateErr := s.pool.Exec(ctx, `UPDATE operational_notification_outbox SET status=$2,available_at=now()+make_interval(secs=>$3),lease_until=NULL,last_error_code=$4,updated_at=now() WHERE id=$1 AND status='processing'`, item.id, status, backoff, code); updateErr != nil {
			return delivered, updateErr
		}
	}
	return delivered, nil
}

func (s *Service) RunNotificationDispatcher(ctx context.Context, interval time.Duration, limit int) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _ = s.DeliverNotifications(ctx, limit)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
