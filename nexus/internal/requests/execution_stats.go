package requests

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExecutionStats almacena estadísticas de éxito/fallo por action_type
type ExecutionStats struct {
	ActionType   string
	SuccessCount int
	FailureCount int
}

// SuccessRate calcula la tasa de éxito. Retorna -1 si no hay datos.
func (s ExecutionStats) SuccessRate() float64 {
	if s.SuccessCount < 0 || s.FailureCount < 0 {
		return -1
	}
	total := s.SuccessCount + s.FailureCount
	if total == 0 {
		return -1
	}
	return float64(s.SuccessCount) / float64(total)
}

// ExecutionStatsStore es el port para leer/escribir stats de ejecución
type ExecutionStatsStore interface {
	RecordSuccess(ctx context.Context, actionType string) error
	RecordFailure(ctx context.Context, actionType string) error
	GetByActionType(ctx context.Context, actionType string) (ExecutionStats, error)
}

// PostgresExecutionStatsStore implementa ExecutionStatsStore con PostgreSQL
type PostgresExecutionStatsStore struct {
	pool *pgxpool.Pool
}

// NewPostgresExecutionStatsStore crea un nuevo store de stats
func NewPostgresExecutionStatsStore(pool *pgxpool.Pool) *PostgresExecutionStatsStore {
	return &PostgresExecutionStatsStore{pool: pool}
}

// RecordSuccess incrementa el contador de éxito para un action_type
func (s *PostgresExecutionStatsStore) RecordSuccess(ctx context.Context, actionType string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO execution_stats (action_type, success_count, last_success_at, updated_at)
		VALUES ($1, 1, now(), now())
		ON CONFLICT (action_type) DO UPDATE SET
			success_count = execution_stats.success_count + 1,
			last_success_at = now(),
			updated_at = now()
	`, actionType)
	if err != nil {
		return fmt.Errorf("record success: %w", err)
	}
	return nil
}

// RecordFailure incrementa el contador de fallo para un action_type
func (s *PostgresExecutionStatsStore) RecordFailure(ctx context.Context, actionType string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO execution_stats (action_type, failure_count, last_failure_at, updated_at)
		VALUES ($1, 1, now(), now())
		ON CONFLICT (action_type) DO UPDATE SET
			failure_count = execution_stats.failure_count + 1,
			last_failure_at = now(),
			updated_at = now()
	`, actionType)
	if err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	return nil
}

// GetByActionType retorna las stats de un action_type.
// Si no existe la fila, retorna SuccessCount=-1, FailureCount=-1 (sin datos).
func (s *PostgresExecutionStatsStore) GetByActionType(ctx context.Context, actionType string) (ExecutionStats, error) {
	var stats ExecutionStats
	err := s.pool.QueryRow(ctx,
		`SELECT action_type, success_count, failure_count FROM execution_stats WHERE action_type = $1`,
		actionType,
	).Scan(&stats.ActionType, &stats.SuccessCount, &stats.FailureCount)
	if err != nil {
		// Si no existe, retornar -1/-1 para indicar "sin datos"
		return ExecutionStats{ActionType: actionType, SuccessCount: -1, FailureCount: -1}, nil
	}
	return stats, nil
}
