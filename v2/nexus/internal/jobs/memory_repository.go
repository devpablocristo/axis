package jobs

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryRepository struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]Job
	now  func() time.Time
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		jobs: make(map[uuid.UUID]Job),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (r *MemoryRepository) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now == nil {
		r.now = func() time.Time { return time.Now().UTC() }
		return
	}
	r.now = now
}

func (r *MemoryRepository) Enqueue(_ context.Context, input EnqueueInput) (Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.RunAfter.IsZero() {
		input.RunAfter = r.now()
	}
	input, err := NormalizeEnqueueInput(input)
	if err != nil {
		return Job{}, false, err
	}
	for id, existing := range r.jobs {
		if sameDedupeScope(existing, input) {
			if input.ReplacePayload && existing.Status == StatusQueued {
				existing.Payload = cloneJSON(input.Payload)
				existing.RunAfter = input.RunAfter
				existing.Priority = input.Priority
				existing.DeadlineAt = input.DeadlineAt
				existing.TimeoutSeconds = durationSeconds(input.Timeout)
				existing.UpdatedAt = r.now()
				r.jobs[id] = existing
			}
			return existing, false, nil
		}
	}
	now := r.now()
	job := Job{
		ID:             input.ID,
		TenantID:       input.TenantID,
		ProductSurface: input.ProductSurface,
		Kind:           input.Kind,
		ShardKey:       input.ShardKey,
		DedupeKey:      input.DedupeKey,
		Payload:        cloneJSON(input.Payload),
		Status:         StatusQueued,
		Priority:       input.Priority,
		MaxAttempts:    input.MaxAttempts,
		RunAfter:       input.RunAfter,
		DeadlineAt:     input.DeadlineAt,
		TimeoutSeconds: durationSeconds(input.Timeout),
		Evidence:       json.RawMessage(`{}`),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.jobs[job.ID] = job
	return job, true, nil
}

func (r *MemoryRepository) Claim(_ context.Context, options ClaimOptions) ([]Job, error) {
	options = NormalizeClaimOptions(options)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	candidates := make([]Job, 0)
	for _, job := range r.jobs {
		if job.Status != StatusQueued || job.Attempts >= job.MaxAttempts || job.RunAfter.After(now) {
			continue
		}
		if len(options.Kinds) > 0 && !contains(options.Kinds, job.Kind) {
			continue
		}
		if options.ShardCount > 0 && stableShard(job.ShardKey, options.ShardCount) != options.ShardIndex {
			continue
		}
		candidates = append(candidates, job)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		if !candidates[i].RunAfter.Equal(candidates[j].RunAfter) {
			return candidates[i].RunAfter.Before(candidates[j].RunAfter)
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	if len(candidates) > options.BatchSize {
		candidates = candidates[:options.BatchSize]
	}
	claimed := make([]Job, 0, len(candidates))
	for _, job := range candidates {
		leaseUntil := now.Add(options.LeaseDuration)
		if job.LockedAt == nil {
			lockedAt := now
			job.LockedAt = &lockedAt
		}
		heartbeatAt := now
		job.Status = StatusRunning
		job.Attempts++
		job.LeaseOwner = options.WorkerID
		job.LeaseUntil = &leaseUntil
		job.HeartbeatAt = &heartbeatAt
		job.UpdatedAt = now
		r.jobs[job.ID] = job
		claimed = append(claimed, job)
	}
	return claimed, nil
}

func (r *MemoryRepository) Heartbeat(_ context.Context, jobID uuid.UUID, workerID string, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.LeaseOwner != strings.TrimSpace(workerID) {
		return ErrJobNotFound
	}
	if job.Status == StatusCancelRequested {
		now := r.now()
		job.Status = StatusCancelled
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.CompletedAt = &now
		job.UpdatedAt = now
		r.jobs[job.ID] = job
		return ErrJobCancelled
	}
	if job.Status != StatusRunning {
		return ErrJobNotFound
	}
	now := r.now()
	leaseUntil := now.Add(defaultDuration(lease, DefaultLease))
	job.HeartbeatAt = &now
	job.LeaseUntil = &leaseUntil
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return nil
}

func (r *MemoryRepository) Complete(_ context.Context, jobID uuid.UUID, workerID string, evidence json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || (job.Status != StatusRunning && job.Status != StatusCancelRequested) || job.LeaseOwner != strings.TrimSpace(workerID) {
		return ErrJobNotFound
	}
	now := r.now()
	if job.Status == StatusCancelRequested {
		job.Status = StatusCancelled
	} else {
		job.Status = StatusSucceeded
	}
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	if job.Status == StatusSucceeded {
		job.Evidence = cloneJSON(defaultJSON(evidence))
	}
	job.CompletedAt = &now
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return nil
}

func (r *MemoryRepository) Fail(_ context.Context, input FailInput) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[input.JobID]
	if !ok || (job.Status != StatusRunning && job.Status != StatusCancelRequested) || job.LeaseOwner != strings.TrimSpace(input.WorkerID) {
		return Job{}, ErrJobNotFound
	}
	now := r.now()
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.LastErrorCode = NormalizeErrorCode(input.ErrorCode)
	job.Evidence = cloneJSON(defaultJSON(input.Evidence))
	if job.Status == StatusCancelRequested {
		job.Status = StatusCancelled
		job.CompletedAt = &now
	} else if input.Retryable && job.Attempts < job.MaxAttempts {
		job.Status = StatusQueued
		job.RunAfter = now.Add(defaultDuration(input.Backoff, time.Second))
	} else {
		job.Status = StatusDeadLetter
		job.CompletedAt = &now
	}
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return job, nil
}

func (r *MemoryRepository) Cancel(_ context.Context, tenantID string, jobID uuid.UUID, reasonCode string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.TenantID != strings.TrimSpace(tenantID) || (job.Status != StatusQueued && job.Status != StatusRunning) {
		return ErrJobNotFound
	}
	now := r.now()
	if job.Status == StatusRunning {
		job.Status = StatusCancelRequested
	} else {
		job.Status = StatusCancelled
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.CompletedAt = &now
	}
	job.LastErrorCode = NormalizeErrorCode(reasonCode)
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, tenantID string, jobID uuid.UUID) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.TenantID != strings.TrimSpace(tenantID) {
		return Job{}, ErrJobNotFound
	}
	return job, nil
}

func (r *MemoryRepository) List(_ context.Context, tenantID, productSurface, status string, limit int) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	tenantID = strings.TrimSpace(tenantID)
	productSurface = strings.TrimSpace(strings.ToLower(productSurface))
	status = strings.TrimSpace(status)
	result := make([]Job, 0)
	for _, job := range r.jobs {
		if job.TenantID != tenantID || (productSurface != "" && job.ProductSurface != productSurface) || (status != "" && string(job.Status) != status) {
			continue
		}
		result = append(result, job)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *MemoryRepository) RecoverExpiredLeases(_ context.Context, limit int) (RecoveryResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	now := r.now()
	var result RecoveryResult
	for id, job := range r.jobs {
		if result.Requeued+result.DeadLetter >= int64(limit) {
			break
		}
		if job.Status != StatusRunning || job.LeaseUntil == nil || !job.LeaseUntil.Before(now) {
			continue
		}
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.HeartbeatAt = nil
		job.LastErrorCode = "lease_expired"
		job.UpdatedAt = now
		if job.Attempts < job.MaxAttempts {
			job.Status = StatusQueued
			job.RunAfter = now
			result.Requeued++
		} else {
			job.Status = StatusDeadLetter
			job.CompletedAt = &now
			result.DeadLetter++
		}
		r.jobs[id] = job
	}
	return result, nil
}

func (r *MemoryRepository) ReplayDeadLetter(_ context.Context, tenantID string, jobID uuid.UUID, runAfter time.Time) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.TenantID != strings.TrimSpace(tenantID) || job.Status != StatusDeadLetter {
		return Job{}, ErrJobNotFound
	}
	for id, active := range r.jobs {
		if id != jobID && active.Status != StatusSucceeded && active.Status != StatusDeadLetter && active.Status != StatusCancelled && sameJobDedupeScope(active, job) {
			return Job{}, ErrJobNotFound
		}
	}
	if runAfter.IsZero() {
		runAfter = r.now()
	}
	job.Status = StatusQueued
	job.Attempts = 0
	job.RunAfter = runAfter.UTC()
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.LockedAt = nil
	job.HeartbeatAt = nil
	job.LastErrorCode = ""
	job.CompletedAt = nil
	job.UpdatedAt = r.now()
	r.jobs[job.ID] = job
	return job, nil
}

func sameDedupeScope(job Job, input EnqueueInput) bool {
	return job.TenantID == input.TenantID && job.ProductSurface == input.ProductSurface && job.Kind == input.Kind && job.DedupeKey == input.DedupeKey
}

func sameJobDedupeScope(left, right Job) bool {
	return left.TenantID == right.TenantID && left.ProductSurface == right.ProductSurface && left.Kind == right.Kind && left.DedupeKey == right.DedupeKey
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	result := make([]byte, len(value))
	copy(result, value)
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func stableShard(value string, count int) int {
	if count <= 0 {
		return 0
	}
	var hash uint32 = 2166136261
	for _, character := range []byte(value) {
		hash ^= uint32(character)
		hash *= 16777619
	}
	return int(hash % uint32(count))
}
