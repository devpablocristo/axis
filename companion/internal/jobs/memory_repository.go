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
	mu     sync.Mutex
	jobs   map[uuid.UUID]Job
	events []memoryJobEvent
	now    func() time.Time
}

type memoryJobEvent struct {
	JobID   uuid.UUID
	Event   string
	Payload json.RawMessage
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

func (r *MemoryRepository) Enqueue(_ context.Context, in EnqueueInput) (Job, bool, error) {
	in, err := NormalizeEnqueueInput(in)
	if err != nil {
		return Job{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.jobs {
		if existing.DedupeKey == in.DedupeKey && (existing.Status == StatusQueued || existing.Status == StatusRunning) {
			if in.ReplacePayload && existing.Status == StatusQueued {
				existing.Payload = cloneRaw(in.Payload)
				existing.ProductSurface = in.ProductSurface
				existing.RunAfter = in.RunAfter
				existing.Priority = in.Priority
				existing.UpdatedAt = r.now()
				r.jobs[existing.ID] = existing
				r.events = append(r.events, memoryJobEvent{JobID: existing.ID, Event: "payload_replaced", Payload: cloneRaw(in.Payload)})
			}
			return existing, false, nil
		}
	}
	now := r.now()
	job := Job{
		ID:             in.ID,
		OrgID:          in.OrgID,
		ProductSurface: in.ProductSurface,
		Kind:           in.Kind,
		ShardKey:       in.ShardKey,
		DedupeKey:      in.DedupeKey,
		Payload:        cloneRaw(in.Payload),
		Status:         StatusQueued,
		Priority:       in.Priority,
		MaxAttempts:    in.MaxAttempts,
		RunAfter:       in.RunAfter,
		DeadlineAt:     in.DeadlineAt,
		TimeoutSeconds: int(in.Timeout.Seconds()),
		Evidence:       json.RawMessage(`{}`),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.jobs[job.ID] = job
	r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "queued", Payload: json.RawMessage(`{"source":"enqueue"}`)})
	return job, true, nil
}

func (r *MemoryRepository) Claim(_ context.Context, opts ClaimOptions) ([]Job, error) {
	opts = NormalizeClaimOptions(opts)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	candidates := make([]Job, 0)
	for _, job := range r.jobs {
		if len(opts.Kinds) > 0 && !containsKind(opts.Kinds, job.Kind) {
			continue
		}
		if opts.ShardCount > 0 && shardIndex(job.ShardKey, opts.ShardCount) != opts.ShardIndex {
			continue
		}
		if job.RunAfter.After(now) {
			continue
		}
		if job.Status != StatusQueued && !(job.Status == StatusRunning && job.LeaseUntil != nil && job.LeaseUntil.Before(now)) {
			continue
		}
		candidates = append(candidates, job)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].RunAfter.Before(candidates[j].RunAfter)
	})
	if len(candidates) > opts.BatchSize {
		candidates = candidates[:opts.BatchSize]
	}
	out := make([]Job, 0, len(candidates))
	for _, job := range candidates {
		leaseUntil := now.Add(opts.LeaseDuration)
		if job.LockedAt == nil {
			lockedAt := now
			job.LockedAt = &lockedAt
		}
		heartbeatAt := now
		job.Status = StatusRunning
		job.Attempts++
		job.LeaseOwner = opts.WorkerID
		job.LeaseUntil = &leaseUntil
		job.HeartbeatAt = &heartbeatAt
		job.UpdatedAt = now
		r.jobs[job.ID] = job
		r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "claimed", Payload: json.RawMessage(`{}`)})
		out = append(out, job)
	}
	return out, nil
}

func (r *MemoryRepository) Heartbeat(_ context.Context, jobID uuid.UUID, workerID string, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || job.Status != StatusRunning || job.LeaseOwner != strings.TrimSpace(workerID) {
		return ErrJobNotFound
	}
	if lease <= 0 {
		lease = DefaultLease
	}
	now := r.now()
	leaseUntil := now.Add(lease)
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
	if !ok || job.Status != StatusRunning || job.LeaseOwner != strings.TrimSpace(workerID) {
		return ErrJobNotFound
	}
	now := r.now()
	job.Status = StatusSucceeded
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.Evidence = cloneRaw(defaultRaw(evidence))
	job.CompletedAt = &now
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "succeeded", Payload: cloneRaw(defaultRaw(evidence))})
	return nil
}

func (r *MemoryRepository) Fail(_ context.Context, in FailInput) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[in.JobID]
	if !ok || job.Status != StatusRunning || job.LeaseOwner != strings.TrimSpace(in.WorkerID) {
		return Job{}, ErrJobNotFound
	}
	now := r.now()
	if in.Backoff <= 0 {
		in.Backoff = time.Second
	}
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.LastError = errorString(in.Err)
	job.Evidence = cloneRaw(defaultRaw(in.Evidence))
	if in.Retryable && job.Attempts < job.MaxAttempts {
		job.Status = StatusQueued
		job.RunAfter = now.Add(in.Backoff)
		r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "retry_scheduled", Payload: cloneRaw(defaultRaw(in.Evidence))})
	} else {
		job.Status = StatusDeadLetter
		job.CompletedAt = &now
		r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "dead_letter", Payload: cloneRaw(defaultRaw(in.Evidence))})
	}
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	return job, nil
}

func (r *MemoryRepository) Cancel(_ context.Context, jobID uuid.UUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok || (job.Status != StatusQueued && job.Status != StatusRunning) {
		return ErrJobNotFound
	}
	now := r.now()
	job.Status = StatusCancelled
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	job.LastError = strings.TrimSpace(reason)
	job.CompletedAt = &now
	job.UpdatedAt = now
	r.jobs[job.ID] = job
	r.events = append(r.events, memoryJobEvent{JobID: job.ID, Event: "cancelled", Payload: json.RawMessage(`{}`)})
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, jobID uuid.UUID) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[jobID]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return job, nil
}

func (r *MemoryRepository) List(_ context.Context, orgID, productSurface, status string, limit int) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	orgID = strings.TrimSpace(orgID)
	productSurface = strings.TrimSpace(strings.ToLower(productSurface))
	status = strings.TrimSpace(status)
	out := make([]Job, 0)
	for _, job := range r.jobs {
		if orgID != "" && job.OrgID != orgID {
			continue
		}
		if productSurface != "" && strings.TrimSpace(strings.ToLower(job.ProductSurface)) != productSurface {
			continue
		}
		if status != "" && string(job.Status) != status {
			continue
		}
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *MemoryRepository) RecoverExpiredLeases(_ context.Context, limit int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	now := r.now()
	var count int64
	for id, job := range r.jobs {
		if count >= int64(limit) {
			break
		}
		if job.Status == StatusRunning && job.LeaseUntil != nil && job.LeaseUntil.Before(now) {
			job.Status = StatusQueued
			job.LeaseOwner = ""
			job.LeaseUntil = nil
			job.HeartbeatAt = nil
			job.RunAfter = now
			job.UpdatedAt = now
			r.jobs[id] = job
			count++
		}
	}
	return count, nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return json.RawMessage(out)
}

func defaultRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func containsKind(kinds []string, kind string) bool {
	for _, candidate := range kinds {
		if strings.TrimSpace(candidate) == kind {
			return true
		}
	}
	return false
}

func shardIndex(value string, count int) int {
	if count <= 0 {
		return 0
	}
	var sum int
	for _, r := range value {
		sum += int(r)
	}
	return sum % count
}
