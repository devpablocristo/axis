// Package domain contiene las entidades de dominio del módulo watchers.
package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WatcherType define qué observa el watcher.
type WatcherType string

const (
	WatcherStaleWorkOrders         WatcherType = "stale_work_orders"
	WatcherUnconfirmedAppointments WatcherType = "unconfirmed_appointments"
	WatcherLowStock                WatcherType = "low_stock"
	WatcherInactiveCustomers       WatcherType = "inactive_customers"
	WatcherRevenueDrop             WatcherType = "revenue_drop"
)

// Watcher observa una condición del negocio y propone acciones.
type Watcher struct {
	ID          uuid.UUID
	OrgID       string
	Name        string
	WatcherType WatcherType
	Config      json.RawMessage
	Enabled     bool
	LastRunAt   *time.Time
	LastResult  json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Proposal es una acción propuesta por un watcher.
type Proposal struct {
	ID              uuid.UUID
	WatcherID       uuid.UUID
	OrgID           string
	ActionType      string
	TargetResource  string
	Params          json.RawMessage
	Reason          string
	GovernanceRequestID *uuid.UUID
	GovernanceDecision  *string
	ExecutionStatus string
	ExecutionResult json.RawMessage
	CreatedAt       time.Time
	ResolvedAt      *time.Time
}

// Estados de ejecución de propuestas.
const (
	ProposalPending  = "pending"
	ProposalExecuted = "executed"
	ProposalFailed   = "failed"
	ProposalSkipped  = "skipped"
)

// WatcherResult es el resultado de ejecutar un watcher.
type WatcherResult struct {
	Found    int `json:"found"`
	Proposed int `json:"proposed"`
	Executed int `json:"executed"`
}

// PymesItem representa un item genérico devuelto por la API de Pymes.
type PymesItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Phone     string          `json:"phone"`
	PartyID   string          `json:"party_id"`
	Metadata  json.RawMessage `json:"metadata"`
	UpdatedAt string          `json:"updated_at"`
}

// RevenueComparison contiene la comparación de facturación mensual.
type RevenueComparison struct {
	CurrentMonth  float64 `json:"current_month"`
	PreviousMonth float64 `json:"previous_month"`
	DropPercent   float64 `json:"drop_percent"`
}

// Tipos de configuración por watcher.

type StaleWorkOrdersConfig struct {
	ThresholdDays int `json:"threshold_days"`
}

type UnconfirmedAppointmentsConfig struct {
	HoursBeforeAppointment int `json:"hours_before_appointment"`
}

type LowStockConfig struct {
	ThresholdUnits int `json:"threshold_units"`
}

type InactiveCustomersConfig struct {
	ThresholdMonths int `json:"threshold_months"`
}

type RevenueDropConfig struct {
	ThresholdPercent float64 `json:"threshold_percent"`
}
