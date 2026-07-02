package config

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	configdomain "github.com/devpablocristo/nexus/internal/config/usecases/domain"
)

// configRepository es el port para persistir la configuración
type configRepository interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
}

const configKey = "system_config"

// Usecases gestiona la configuración del sistema
type Usecases struct {
	repo   configRepository
	mu     sync.RWMutex
	cached *configdomain.SystemConfig
}

// NewUsecases crea un nuevo Usecases de configuración
func NewUsecases(repo configRepository) *Usecases {
	return &Usecases{repo: repo}
}

// GetConfig retorna la configuración completa del sistema
func (u *Usecases) GetConfig(ctx context.Context) (*configdomain.SystemConfig, error) {
	u.mu.RLock()
	if u.cached != nil {
		defer u.mu.RUnlock()
		return u.cached, nil
	}
	u.mu.RUnlock()

	raw, err := u.repo.Get(ctx, configKey)
	if err != nil {
		// Si no existe en DB, retornar default
		cfg := configdomain.DefaultSystemConfig()
		return &cfg, nil
	}

	var cfg configdomain.SystemConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal system config: %w", err)
	}

	u.mu.Lock()
	u.cached = &cfg
	u.mu.Unlock()

	return &cfg, nil
}

// UpdateConfig actualiza la configuración completa
func (u *Usecases) UpdateConfig(ctx context.Context, cfg configdomain.SystemConfig) (*configdomain.SystemConfig, error) {
	if err := validate(cfg); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal system config: %w", err)
	}

	if err := u.repo.Set(ctx, configKey, raw); err != nil {
		return nil, fmt.Errorf("save system config: %w", err)
	}

	u.mu.Lock()
	u.cached = &cfg
	u.mu.Unlock()

	return &cfg, nil
}

// ResetConfig resetea a la configuración default
func (u *Usecases) ResetConfig(ctx context.Context) (*configdomain.SystemConfig, error) {
	cfg := configdomain.DefaultSystemConfig()
	return u.UpdateConfig(ctx, cfg)
}

// UpdateSection actualiza solo una sección específica sin tocar las demás
func (u *Usecases) UpdateSection(ctx context.Context, section string, data json.RawMessage) (*configdomain.SystemConfig, error) {
	current, err := u.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	switch section {
	case "risk":
		if err := json.Unmarshal(data, &current.Risk); err != nil {
			return nil, fmt.Errorf("unmarshal risk config: %w", err)
		}
	case "approvals":
		if err := json.Unmarshal(data, &current.Approvals); err != nil {
			return nil, fmt.Errorf("unmarshal approvals config: %w", err)
		}
	case "learning":
		if err := json.Unmarshal(data, &current.Learning); err != nil {
			return nil, fmt.Errorf("unmarshal learning config: %w", err)
		}
	case "general":
		if err := json.Unmarshal(data, &current.General); err != nil {
			return nil, fmt.Errorf("unmarshal general config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown config section: %s", section)
	}

	return u.UpdateConfig(ctx, *current)
}

func validate(cfg configdomain.SystemConfig) error {
	r := cfg.Risk
	if r.Thresholds.Deny <= r.Thresholds.RequireApproval {
		return fmt.Errorf("deny threshold must be greater than require_approval")
	}
	if r.Thresholds.RequireApproval <= r.Thresholds.EnhancedLog {
		return fmt.Errorf("require_approval threshold must be greater than enhanced_log")
	}
	if r.Thresholds.MaxAmplification <= 0 {
		return fmt.Errorf("max_amplification must be positive")
	}
	if r.BusinessHours.Start >= r.BusinessHours.End {
		return fmt.Errorf("business_hours start must be before end")
	}
	if cfg.Approvals.DefaultTTLSeconds <= 0 {
		return fmt.Errorf("approval TTL must be positive")
	}
	if cfg.Learning.MinSamples <= 0 {
		return fmt.Errorf("learning min_samples must be positive")
	}
	if cfg.Learning.MinApprovalRate <= 0 || cfg.Learning.MinApprovalRate > 1 {
		return fmt.Errorf("learning min_approval_rate must be between 0 and 1")
	}
	if cfg.General.DefaultListLimit <= 0 {
		return fmt.Errorf("default_list_limit must be positive")
	}
	if cfg.General.MaxListLimit <= 0 {
		return fmt.Errorf("max_list_limit must be positive")
	}
	return nil
}
