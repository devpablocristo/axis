package finops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Usage struct {
	OrgID, ProductID, Area, Service  string
	VirployeeID                      uuid.UUID
	CapabilityKey, CapabilityVersion string
	Model                            string
	InputUnits, OutputUnits          int64
	EstimatedCostMicroUSD            int64
	IdempotencyKey                   string
	OccurredAt                       time.Time
	Metadata                         map[string]any
}

type Event struct {
	ID                uuid.UUID       `json:"id"`
	OrgID             string          `json:"org_id"`
	ProductID         string          `json:"product_id"`
	Area              string          `json:"area"`
	Service           string          `json:"service"`
	VirployeeID       *uuid.UUID      `json:"virployee_id,omitempty"`
	CapabilityKey     string          `json:"capability_key"`
	CapabilityVersion string          `json:"capability_version"`
	Model             string          `json:"model"`
	InputUnits        int64           `json:"input_units"`
	OutputUnits       int64           `json:"output_units"`
	CostMicroUSD      *int64          `json:"cost_micro_usd,omitempty"`
	PricingStatus     string          `json:"pricing_status"`
	PriceHash         string          `json:"price_hash"`
	EventType         string          `json:"event_type"`
	IdempotencyKey    string          `json:"idempotency_key"`
	OccurredAt        time.Time       `json:"occurred_at"`
	RecordedAt        time.Time       `json:"recorded_at"`
	Metadata          json.RawMessage `json:"metadata"`
}

type Summary struct {
	Group        string `json:"group"`
	Events       int64  `json:"events"`
	InputUnits   int64  `json:"input_units"`
	OutputUnits  int64  `json:"output_units"`
	CostMicroUSD int64  `json:"cost_micro_usd"`
	Unpriced     int64  `json:"unpriced"`
}

type Budget struct {
	ID              uuid.UUID       `json:"id"`
	OrgID           string          `json:"org_id"`
	ScopeType       string          `json:"scope_type"`
	ProductID       string          `json:"product_id"`
	MonthStart      time.Time       `json:"month_start"`
	AmountMicroUSD  int64           `json:"amount_micro_usd"`
	AlertThresholds json.RawMessage `json:"alert_thresholds"`
	UpdatedBy       string          `json:"updated_by"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func trusted(org, actor string) error {
	if strings.TrimSpace(org) == "" || strings.TrimSpace(actor) == "" {
		return domainerr.Unauthorized("trusted organization and actor are required")
	}
	return nil
}

func (s *Service) Record(ctx context.Context, in Usage) error {
	if strings.TrimSpace(in.OrgID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" {
		return domainerr.Validation("org_id and idempotency_key are required")
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = s.now()
	}
	pricingStatus := "priced"
	var cost any = in.EstimatedCostMicroUSD
	price := sha256.Sum256([]byte("runtime-estimate.v1"))
	priceHash := hex.EncodeToString(price[:])
	if in.EstimatedCostMicroUSD <= 0 && in.InputUnits+in.OutputUnits > 0 {
		pricingStatus, cost, priceHash = "unpriced", nil, ""
	}
	metadata, _ := json.Marshal(in.Metadata)
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	var virployee any
	if in.VirployeeID != uuid.Nil {
		virployee = in.VirployeeID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO companion_finops_events(
		id,org_id,product_id,area,service,virployee_id,capability_key,capability_version,model,input_units,output_units,cost_micro_usd,pricing_status,price_hash,event_type,idempotency_key,occurred_at,metadata)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'usage',$15,$16,$17)
		ON CONFLICT(org_id,idempotency_key) DO NOTHING`,
		uuid.New(), strings.TrimSpace(in.OrgID), strings.TrimSpace(in.ProductID), strings.TrimSpace(in.Area), fallback(in.Service, "companion"),
		virployee, strings.TrimSpace(in.CapabilityKey), strings.TrimSpace(in.CapabilityVersion), strings.TrimSpace(in.Model),
		in.InputUnits, in.OutputUnits, cost, pricingStatus, priceHash, strings.TrimSpace(in.IdempotencyKey), in.OccurredAt, metadata)
	return err
}

func (s *Service) ListEvents(ctx context.Context, org, actor string, from, to time.Time) ([]Event, error) {
	if err := trusted(org, actor); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,org_id,product_id,area,service,virployee_id,capability_key,capability_version,model,input_units,output_units,cost_micro_usd,pricing_status,price_hash,event_type,idempotency_key,occurred_at,recorded_at,metadata
		FROM companion_finops_events WHERE org_id=$1 AND occurred_at >= $2 AND occurred_at < $3 ORDER BY occurred_at DESC,id DESC LIMIT 500`, org, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var item Event
		if err := rows.Scan(&item.ID, &item.OrgID, &item.ProductID, &item.Area, &item.Service, &item.VirployeeID, &item.CapabilityKey, &item.CapabilityVersion, &item.Model, &item.InputUnits, &item.OutputUnits, &item.CostMicroUSD, &item.PricingStatus, &item.PriceHash, &item.EventType, &item.IdempotencyKey, &item.OccurredAt, &item.RecordedAt, &item.Metadata); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) Summary(ctx context.Context, org, actor, groupBy string, from, to time.Time) ([]Summary, error) {
	if err := trusted(org, actor); err != nil {
		return nil, err
	}
	column := map[string]string{
		"product": "product_id", "virployee": "COALESCE(virployee_id::text,'')",
		"capability": "capability_key", "model": "model", "area": "area",
	}[groupBy]
	if column == "" {
		return nil, domainerr.Validation("group_by must be product, virployee, capability, model or area")
	}
	query := `SELECT ` + column + `,count(*),sum(input_units),sum(output_units),COALESCE(sum(cost_micro_usd),0),count(*) FILTER (WHERE pricing_status='unpriced')
		FROM companion_finops_events WHERE org_id=$1 AND occurred_at >= $2 AND occurred_at < $3 GROUP BY 1 ORDER BY 5 DESC,1`
	rows, err := s.pool.Query(ctx, query, org, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var item Summary
		if err := rows.Scan(&item.Group, &item.Events, &item.InputUnits, &item.OutputUnits, &item.CostMicroUSD, &item.Unpriced); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) PutBudget(ctx context.Context, org, actor, role string, in Budget) (Budget, error) {
	if err := trusted(org, actor); err != nil {
		return Budget{}, err
	}
	if role != "owner" && role != "admin" {
		return Budget{}, domainerr.Forbidden("owner or admin is required")
	}
	in.ScopeType, in.ProductID = strings.ToLower(strings.TrimSpace(in.ScopeType)), strings.TrimSpace(in.ProductID)
	if in.ScopeType != "organization" && in.ScopeType != "product" {
		return Budget{}, domainerr.Validation("scope_type is invalid")
	}
	if (in.ScopeType == "organization" && in.ProductID != "") || (in.ScopeType == "product" && in.ProductID == "") || in.AmountMicroUSD <= 0 {
		return Budget{}, domainerr.Validation("budget scope or amount is invalid")
	}
	if len(in.AlertThresholds) == 0 {
		in.AlertThresholds = json.RawMessage(`[80,100]`)
	}
	var thresholds []int
	if json.Unmarshal(in.AlertThresholds, &thresholds) != nil {
		return Budget{}, domainerr.Validation("alert_thresholds must be an array")
	}
	in.ID, in.OrgID, in.UpdatedBy, in.UpdatedAt = uuid.New(), org, actor, s.now()
	err := s.pool.QueryRow(ctx, `INSERT INTO companion_finops_budgets(id,org_id,scope_type,product_id,month_start,amount_micro_usd,alert_thresholds,updated_by,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT(org_id,scope_type,product_id,month_start) DO UPDATE SET amount_micro_usd=EXCLUDED.amount_micro_usd,alert_thresholds=EXCLUDED.alert_thresholds,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at
		RETURNING id`, in.ID, org, in.ScopeType, in.ProductID, in.MonthStart, in.AmountMicroUSD, in.AlertThresholds, actor, in.UpdatedAt).Scan(&in.ID)
	return in, err
}

func (s *Service) ListBudgets(ctx context.Context, org, actor string) ([]Budget, error) {
	if err := trusted(org, actor); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,org_id,scope_type,product_id,month_start,amount_micro_usd,alert_thresholds,updated_by,updated_at FROM companion_finops_budgets WHERE org_id=$1 ORDER BY month_start DESC,scope_type,product_id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Budget
	for rows.Next() {
		var item Budget
		if err := rows.Scan(&item.ID, &item.OrgID, &item.ScopeType, &item.ProductID, &item.MonthStart, &item.AmountMicroUSD, &item.AlertThresholds, &item.UpdatedBy, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}
