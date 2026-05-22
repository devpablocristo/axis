package config

import (
	"context"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = domainerr.NotFound("not found")

// PostgresRepository implementa configRepository usando PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository crea un nuevo repositorio de configuración
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Get obtiene un valor de configuración por clave
func (r *PostgresRepository) Get(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := r.pool.QueryRow(ctx,
		`SELECT value FROM config WHERE key = $1`, key,
	).Scan(&value)
	if err != nil {
		return nil, fmt.Errorf("get config %s: %w", key, err)
	}
	return value, nil
}

// Set guarda un valor de configuración (upsert)
func (r *PostgresRepository) Set(ctx context.Context, key string, value []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO config (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set config %s: %w", key, err)
	}
	return nil
}
