package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Handler func(ctx context.Context, job Job) (json.RawMessage, error)

type WorkerConfig struct {
	WorkerID       string
	Concurrency    int
	PollInterval   time.Duration
	LeaseDuration  time.Duration
	DefaultTimeout time.Duration
	ShardCount     int
	ShardIndex     int
	Backoff        func(attempt int) time.Duration
	Breaker        *CircuitBreaker
}

type Worker struct {
	repo     Repository
	cfg      WorkerConfig
	handlers map[string]Handler
	mu       sync.RWMutex
}

func NewWorker(repo Repository, cfg WorkerConfig) *Worker {
	if cfg.WorkerID == "" {
		cfg.WorkerID = "companion-worker-" + uuid.NewString()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLease
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = DefaultTimeout
	}
	if cfg.Backoff == nil {
		cfg.Backoff = ExponentialBackoff
	}
	if cfg.Breaker == nil {
		cfg.Breaker = NewCircuitBreaker(5, time.Minute)
	}
	return &Worker{
		repo:     repo,
		cfg:      cfg,
		handlers: make(map[string]Handler),
	}
}

func (w *Worker) Register(kind string, handler Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if kind == "" || handler == nil {
		return
	}
	w.handlers[kind] = handler
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	var wg sync.WaitGroup
	sem := make(chan struct{}, w.cfg.Concurrency)
	defer wg.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claimed, err := w.claimAndDispatch(ctx, sem, &wg)
		if err != nil {
			slog.Error("job_worker_claim_failed", "worker_id", w.cfg.WorkerID, "error", err)
		}
		if claimed == 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, w.cfg.Concurrency)
	claimed, err := w.claimAndDispatch(ctx, sem, &wg)
	wg.Wait()
	return claimed, err
}

func (w *Worker) claimAndDispatch(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) (int, error) {
	kinds := w.handlerKinds()
	if len(kinds) == 0 {
		return 0, nil
	}
	jobs, err := w.repo.Claim(ctx, ClaimOptions{
		WorkerID:      w.cfg.WorkerID,
		Kinds:         kinds,
		BatchSize:     cap(sem),
		LeaseDuration: w.cfg.LeaseDuration,
		ShardCount:    w.cfg.ShardCount,
		ShardIndex:    w.cfg.ShardIndex,
	})
	if err != nil {
		return 0, err
	}
	for _, job := range jobs {
		job := job
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			w.runJob(ctx, job)
		}()
	}
	return len(jobs), nil
}

func (w *Worker) runJob(parent context.Context, job Job) {
	handler := w.handler(job.Kind)
	if handler == nil {
		_, err := w.repo.Fail(parent, FailInput{
			JobID:     job.ID,
			WorkerID:  w.cfg.WorkerID,
			Err:       Permanent(fmt.Errorf("no handler registered for job kind %s", job.Kind)),
			Retryable: false,
			Evidence:  json.RawMessage(`{"reason":"missing_handler"}`),
		})
		if err != nil {
			slog.Error("job_missing_handler_fail_failed", "job_id", job.ID, "error", err)
		}
		return
	}
	if !w.cfg.Breaker.Allow(job.Kind) {
		_, err := w.repo.Fail(parent, FailInput{
			JobID:     job.ID,
			WorkerID:  w.cfg.WorkerID,
			Err:       fmt.Errorf("circuit open for job kind %s", job.Kind),
			Retryable: true,
			Backoff:   w.cfg.Backoff(job.Attempts),
			Evidence:  json.RawMessage(`{"reason":"circuit_open"}`),
		})
		if err != nil {
			slog.Error("job_circuit_fail_failed", "job_id", job.ID, "error", err)
		}
		return
	}
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = w.cfg.DefaultTimeout
	}
	if job.DeadlineAt != nil {
		deadlineTimeout := time.Until(*job.DeadlineAt)
		if deadlineTimeout > 0 && deadlineTimeout < timeout {
			timeout = deadlineTimeout
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	evidence, err := handler(ctx, job)
	if err == nil {
		w.cfg.Breaker.RecordSuccess(job.Kind)
		if err := w.repo.Complete(parent, job.ID, w.cfg.WorkerID, defaultRaw(evidence)); err != nil {
			slog.Error("job_complete_failed", "job_id", job.ID, "error", err)
		}
		return
	}
	retryable := !IsPermanent(err)
	w.cfg.Breaker.RecordFailure(job.Kind)
	failInput := FailInput{
		JobID:     job.ID,
		WorkerID:  w.cfg.WorkerID,
		Err:       err,
		Retryable: retryable,
		Backoff:   w.cfg.Backoff(job.Attempts),
		Evidence:  defaultRaw(evidence),
	}
	if ctx.Err() != nil {
		failInput.Err = ctx.Err()
		failInput.Retryable = true
		failInput.Evidence = json.RawMessage(`{"reason":"timeout_or_cancelled"}`)
	}
	if _, failErr := w.repo.Fail(parent, failInput); failErr != nil {
		slog.Error("job_fail_failed", "job_id", job.ID, "error", failErr)
	}
}

func (w *Worker) handler(kind string) Handler {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.handlers[kind]
}

func (w *Worker) handlerKinds() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, 0, len(w.handlers))
	for kind := range w.handlers {
		out = append(out, kind)
	}
	return out
}

func ExponentialBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

type CircuitBreaker struct {
	mu           sync.Mutex
	threshold    int
	openDuration time.Duration
	state        map[string]circuitState
}

type circuitState struct {
	failures  int
	openUntil time.Time
}

func NewCircuitBreaker(threshold int, openDuration time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if openDuration <= 0 {
		openDuration = time.Minute
	}
	return &CircuitBreaker{
		threshold:    threshold,
		openDuration: openDuration,
		state:        make(map[string]circuitState),
	}
}

func (b *CircuitBreaker) Allow(kind string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.state[kind]
	if state.openUntil.IsZero() {
		return true
	}
	if time.Now().UTC().After(state.openUntil) {
		state.openUntil = time.Time{}
		state.failures = 0
		b.state[kind] = state
		return true
	}
	return false
}

func (b *CircuitBreaker) RecordSuccess(kind string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, kind)
}

func (b *CircuitBreaker) RecordFailure(kind string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.state[kind]
	state.failures++
	if state.failures >= b.threshold {
		state.openUntil = time.Now().UTC().Add(b.openDuration)
	}
	b.state[kind] = state
}
