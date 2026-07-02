package agentprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/platform/databases/postgres/go"
)

type PostgresRepository struct {
	db *sharedpostgres.DB
}

func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectProfileSQL = `
	SELECT id, profile_id, family_id, version_label, name, description, system_prompt,
	       max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
	       llm_config_json, enabled, archived_at, trashed_at, created_at, updated_at
	FROM agent_profiles`

const selectVersionSQL = `
	SELECT id, agent_profile_id, profile_id, family_id, version_label, name, description,
	       system_prompt, max_autonomy, allowed_tools, allowed_capabilities,
	       memory_policy_json, llm_config_json, enabled, archived_at, trashed_at,
	       original_created_at, original_updated_at, saved_at
	FROM agent_profile_versions`

func (r *PostgresRepository) ListProfiles(ctx context.Context, lifecycle LifecycleView) ([]Profile, error) {
	query := selectProfileSQL
	switch lifecycle {
	case LifecycleArchived:
		query += ` WHERE archived_at IS NOT NULL AND trashed_at IS NULL`
	case LifecycleTrash:
		query += ` WHERE trashed_at IS NOT NULL`
	case LifecycleAll:
	case LifecycleNonTrash:
		query += ` WHERE trashed_at IS NULL`
	default:
		query += ` WHERE archived_at IS NULL AND trashed_at IS NULL`
	}
	query += ` ORDER BY profile_id`
	rows, err := r.db.Pool().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer rows.Close()
	out := make([]Profile, 0)
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetProfile(ctx context.Context, profileID string) (Profile, error) {
	predicate, arg := profileLookupPredicate(profileID, "$1")
	row := r.db.Pool().QueryRow(ctx, selectProfileSQL+` WHERE `+predicate, arg)
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("get agent profile: %w", err)
	}
	return profile, nil
}

// IsArchivedOrTrashed reports the lifecycle flags of a profile by its natural
// key, so the usecase can refuse to silently revive a retired profile on
// upsert. Returns ErrNotFound when the profile does not exist.
func (r *PostgresRepository) IsArchivedOrTrashed(ctx context.Context, profileID string) (bool, bool, error) {
	var archived, trashed bool
	predicate, arg := profileLookupPredicate(profileID, "$1")
	err := r.db.Pool().QueryRow(ctx, `
		SELECT archived_at IS NOT NULL, trashed_at IS NOT NULL
		FROM agent_profiles WHERE `+predicate, arg).Scan(&archived, &trashed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, ErrNotFound
		}
		return false, false, fmt.Errorf("check agent profile lifecycle: %w", err)
	}
	return archived, trashed, nil
}

func (r *PostgresRepository) UpsertProfile(ctx context.Context, profile Profile) (Profile, error) {
	profile = normalizeProfile(profile)
	memoryPolicy, err := json.Marshal(nonNilMap(profile.MemoryPolicy))
	if err != nil {
		return Profile{}, fmt.Errorf("marshal memory policy: %w", err)
	}
	llmConfig, err := json.Marshal(nonNilMap(profile.LLMConfig))
	if err != nil {
		return Profile{}, fmt.Errorf("marshal llm config: %w", err)
	}
	row := r.db.Pool().QueryRow(ctx, `
		INSERT INTO agent_profiles (
			profile_id, family_id, version_label, name, description, system_prompt,
			max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
			llm_config_json, enabled, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
		ON CONFLICT (profile_id)
		DO UPDATE SET
			family_id = EXCLUDED.family_id,
			version_label = EXCLUDED.version_label,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			system_prompt = EXCLUDED.system_prompt,
			max_autonomy = EXCLUDED.max_autonomy,
			allowed_tools = EXCLUDED.allowed_tools,
			allowed_capabilities = EXCLUDED.allowed_capabilities,
			memory_policy_json = EXCLUDED.memory_policy_json,
			llm_config_json = EXCLUDED.llm_config_json,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING id, profile_id, family_id, version_label, name, description, system_prompt,
		          max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
		          llm_config_json, enabled, archived_at, trashed_at, created_at, updated_at
	`, profile.ProfileID, profile.FamilyID, profile.VersionLabel, profile.Name, profile.Description,
		profile.SystemPrompt, profile.MaxAutonomy, profile.AllowedTools, profile.AllowedCapabilities,
		memoryPolicy, llmConfig, profile.Enabled)
	out, err := scanProfile(row)
	if err != nil {
		return Profile{}, fmt.Errorf("upsert agent profile: %w", err)
	}
	return out, nil
}

func (r *PostgresRepository) ArchiveProfile(ctx context.Context, profileID string) (Profile, error) {
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE agent_profiles SET archived_at = now(), trashed_at = NULL, updated_at = now()
		WHERE profile_id = $1 AND archived_at IS NULL AND trashed_at IS NULL
		RETURNING id, profile_id, family_id, version_label, name, description, system_prompt,
		          max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
		          llm_config_json, enabled, archived_at, trashed_at, created_at, updated_at
	`, strings.TrimSpace(profileID))
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("archive agent profile: %w", err)
	}
	return profile, nil
}

func (r *PostgresRepository) RestoreProfile(ctx context.Context, profileID string) (Profile, error) {
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE agent_profiles SET archived_at = NULL, trashed_at = NULL, updated_at = now()
		WHERE profile_id = $1 AND (archived_at IS NOT NULL OR trashed_at IS NOT NULL)
		RETURNING id, profile_id, family_id, version_label, name, description, system_prompt,
		          max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
		          llm_config_json, enabled, archived_at, trashed_at, created_at, updated_at
	`, strings.TrimSpace(profileID))
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("restore agent profile: %w", err)
	}
	return profile, nil
}

func (r *PostgresRepository) TrashProfile(ctx context.Context, profileID string) (Profile, error) {
	row := r.db.Pool().QueryRow(ctx, `
		UPDATE agent_profiles SET archived_at = NULL, trashed_at = now(), updated_at = now()
		WHERE profile_id = $1 AND trashed_at IS NULL
		RETURNING id, profile_id, family_id, version_label, name, description, system_prompt,
		          max_autonomy, allowed_tools, allowed_capabilities, memory_policy_json,
		          llm_config_json, enabled, archived_at, trashed_at, created_at, updated_at
	`, strings.TrimSpace(profileID))
	profile, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("trash agent profile: %w", err)
	}
	return profile, nil
}

func (r *PostgresRepository) PurgeProfile(ctx context.Context, profileID string) error {
	profileID = strings.TrimSpace(profileID)
	var activeAgents int
	if err := r.db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM companion_agents
		WHERE profile_id = $1
		  AND status = 'active'
		  AND COALESCE(lifecycle_status, 'active') = 'active'
		  AND COALESCE(review_status, 'approved') = 'approved'
	`, profileID).Scan(&activeAgents); err != nil {
		return fmt.Errorf("check active agents for profile purge: %w", err)
	}
	if activeAgents > 0 {
		return fmt.Errorf("%w: profile is used by active agents", ErrConflict)
	}
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM agent_profiles WHERE profile_id = $1 AND trashed_at IS NOT NULL`, profileID)
	if err != nil {
		return fmt.Errorf("purge agent profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListVersions(ctx context.Context, profileID string, limit int) ([]Version, error) {
	rows, err := r.db.Pool().Query(ctx, selectVersionSQL+`
		WHERE profile_id = $1
		ORDER BY saved_at DESC
		LIMIT $2
	`, strings.TrimSpace(profileID), limit)
	if err != nil {
		return nil, fmt.Errorf("list agent profile versions: %w", err)
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (Profile, error) {
	var profile Profile
	var memoryRaw, llmRaw []byte
	err := row.Scan(
		&profile.ID, &profile.ProfileID, &profile.FamilyID, &profile.VersionLabel,
		&profile.Name, &profile.Description, &profile.SystemPrompt, &profile.MaxAutonomy,
		&profile.AllowedTools, &profile.AllowedCapabilities, &memoryRaw, &llmRaw,
		&profile.Enabled, &profile.ArchivedAt, &profile.TrashedAt, &profile.CreatedAt, &profile.UpdatedAt,
	)
	if err != nil {
		return Profile{}, err
	}
	if len(memoryRaw) > 0 {
		if err := json.Unmarshal(memoryRaw, &profile.MemoryPolicy); err != nil {
			return Profile{}, fmt.Errorf("unmarshal memory policy: %w", err)
		}
	}
	if len(llmRaw) > 0 {
		if err := json.Unmarshal(llmRaw, &profile.LLMConfig); err != nil {
			return Profile{}, fmt.Errorf("unmarshal llm config: %w", err)
		}
	}
	return profile, nil
}

func scanVersion(row rowScanner) (Version, error) {
	var version Version
	var memoryRaw, llmRaw []byte
	err := row.Scan(
		&version.ID, &version.AgentProfileID, &version.ProfileID, &version.FamilyID,
		&version.VersionLabel, &version.Name, &version.Description, &version.SystemPrompt,
		&version.MaxAutonomy, &version.AllowedTools, &version.AllowedCapabilities,
		&memoryRaw, &llmRaw, &version.Enabled, &version.ArchivedAt, &version.TrashedAt,
		&version.OriginalCreatedAt, &version.OriginalUpdatedAt, &version.SavedAt,
	)
	if err != nil {
		return Version{}, err
	}
	if len(memoryRaw) > 0 {
		if err := json.Unmarshal(memoryRaw, &version.MemoryPolicy); err != nil {
			return Version{}, fmt.Errorf("unmarshal version memory policy: %w", err)
		}
	}
	if len(llmRaw) > 0 {
		if err := json.Unmarshal(llmRaw, &version.LLMConfig); err != nil {
			return Version{}, fmt.Errorf("unmarshal version llm config: %w", err)
		}
	}
	return version, nil
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func profileLookupPredicate(identifier, placeholder string) (string, any) {
	identifier = strings.TrimSpace(identifier)
	if parsed, err := uuid.Parse(identifier); err == nil {
		return "id = " + placeholder, parsed
	}
	return "profile_id = " + placeholder, identifier
}
