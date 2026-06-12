package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAlertSinkNotConfigured = errors.New("ops alert sink is not configured")

type AlertSink interface {
	SendAlerts(ctx context.Context, payload AlertDispatchPayload) (AlertDispatchResult, error)
}

type AlertDispatchPayload struct {
	OrgID          string    `json:"org_id"`
	ProductSurface string    `json:"product_surface,omitempty"`
	Period         string    `json:"period"`
	GeneratedAt    time.Time `json:"generated_at"`
	Alerts         []Alert   `json:"alerts"`
}

type AlertDispatchResult struct {
	Status        string    `json:"status"`
	AlertCount    int       `json:"alert_count"`
	Endpoint      string    `json:"endpoint,omitempty"`
	HTTPStatus    int       `json:"http_status,omitempty"`
	DispatchedAt  time.Time `json:"dispatched_at,omitempty"`
	NotConfigured bool      `json:"not_configured,omitempty"`
}

type WebhookAlertSink struct {
	endpoint string
	client   *http.Client
}

func NewWebhookAlertSink(endpoint string, timeout time.Duration) *WebhookAlertSink {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &WebhookAlertSink{
		endpoint: endpoint,
		client:   &http.Client{Timeout: timeout},
	}
}

func (s *WebhookAlertSink) SendAlerts(ctx context.Context, payload AlertDispatchPayload) (AlertDispatchResult, error) {
	if s == nil || strings.TrimSpace(s.endpoint) == "" {
		return AlertDispatchResult{Status: "not_configured", NotConfigured: true}, ErrAlertSinkNotConfigured
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return AlertDispatchResult{}, fmt.Errorf("marshal alert dispatch payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(raw))
	if err != nil {
		return AlertDispatchResult{}, fmt.Errorf("create alert webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "axis-companion-ops/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return AlertDispatchResult{}, fmt.Errorf("send alert webhook: %w", err)
	}
	defer resp.Body.Close()
	result := AlertDispatchResult{
		Status:       "dispatched",
		AlertCount:   len(payload.Alerts),
		Endpoint:     redactedEndpoint(s.endpoint),
		HTTPStatus:   resp.StatusCode,
		DispatchedAt: time.Now().UTC(),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "failed"
		return result, fmt.Errorf("alert webhook returned HTTP %d", resp.StatusCode)
	}
	return result, nil
}

func (u *Usecases) DispatchAlerts(ctx context.Context, q Query) (AlertDispatchResult, error) {
	if u.deps.AlertSink == nil {
		return AlertDispatchResult{Status: "not_configured", NotConfigured: true}, ErrAlertSinkNotConfigured
	}
	console, err := u.GetConsole(ctx, q)
	if err != nil {
		return AlertDispatchResult{}, err
	}
	payload := AlertDispatchPayload{
		OrgID:          console.OrgID,
		ProductSurface: console.ProductSurface,
		Period:         console.Period,
		GeneratedAt:    console.GeneratedAt,
		Alerts:         console.Alerts,
	}
	return u.deps.AlertSink.SendAlerts(ctx, payload)
}

func redactedEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "configured"
	}
	return parsed.Scheme + "://" + parsed.Host
}
