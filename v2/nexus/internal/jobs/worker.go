package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
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
	RecoveryBatch  int
	ShardCount     int
	ShardIndex     int
	Backoff        func(attempt int) time.Duration
}

type Worker struct {
	repo     Repository
	config   WorkerConfig
	handlers map[string]Handler
	mu       sync.RWMutex
}

func NewWorker(repo Repository, config WorkerConfig) *Worker {
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.WorkerID == "" {
		config.WorkerID = "nexus-worker-" + uuid.NewString()
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 1
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	config.LeaseDuration = defaultDuration(config.LeaseDuration, DefaultLease)
	config.DefaultTimeout = defaultDuration(config.DefaultTimeout, DefaultTimeout)
	if config.RecoveryBatch <= 0 {
		config.RecoveryBatch = 100
	}
	if config.Backoff == nil {
		config.Backoff = ExponentialBackoff
	}
	return &Worker{repo: repo, config: config, handlers: make(map[string]Handler)}
}

func (w *Worker) Register(kind string, handler Handler) {
	kind = strings.TrimSpace(kind)
	if kind == "" || handler == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[kind] = handler
}

func (w *Worker) Run(ctx context.Context) {
	var waitGroup sync.WaitGroup
	for index := 0; index < w.config.Concurrency; index++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()
			w.runSlot(ctx, slot)
		}(index)
	}
	waitGroup.Wait()
}

func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	if _, err := w.repo.RecoverExpiredLeases(ctx, w.config.RecoveryBatch); err != nil {
		return 0, err
	}
	claimed, err := w.repo.Claim(ctx, ClaimOptions{
		WorkerID:      w.config.WorkerID,
		Kinds:         w.handlerKinds(),
		BatchSize:     w.config.Concurrency,
		LeaseDuration: w.config.LeaseDuration,
		ShardCount:    w.config.ShardCount,
		ShardIndex:    w.config.ShardIndex,
	})
	if err != nil {
		return 0, err
	}
	var waitGroup sync.WaitGroup
	for _, job := range claimed {
		job := job
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			w.runJob(ctx, job)
		}()
	}
	waitGroup.Wait()
	return len(claimed), nil
}

func (w *Worker) runSlot(ctx context.Context, slot int) {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	workerID := w.config.WorkerID + ":" + strconv.Itoa(slot)
	for {
		if ctx.Err() != nil {
			return
		}
		if slot == 0 {
			result, err := w.repo.RecoverExpiredLeases(ctx, w.config.RecoveryBatch)
			if err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "job lease recovery failed", "worker_id", w.config.WorkerID, "error", err)
			} else if result.Requeued > 0 || result.DeadLetter > 0 {
				slog.InfoContext(ctx, "job leases recovered", "worker_id", w.config.WorkerID, "requeued", result.Requeued, "dead_letter", result.DeadLetter)
			}
		}
		jobs, err := w.repo.Claim(ctx, ClaimOptions{
			WorkerID:      workerID,
			Kinds:         w.handlerKinds(),
			BatchSize:     1,
			LeaseDuration: w.config.LeaseDuration,
			ShardCount:    w.config.ShardCount,
			ShardIndex:    w.config.ShardIndex,
		})
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "job claim failed", "worker_id", workerID, "error", err)
			}
		} else if len(jobs) == 1 {
			w.runJobWithWorker(ctx, jobs[0], workerID)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) runJob(parent context.Context, job Job) {
	w.runJobWithWorker(parent, job, w.config.WorkerID)
}

func (w *Worker) runJobWithWorker(parent context.Context, job Job, workerID string) {
	handler := w.handler(job.Kind)
	if handler == nil {
		w.fail(parent, job, workerID, "handler_missing", false, nil)
		return
	}
	if job.DeadlineAt != nil && !time.Now().UTC().Before(*job.DeadlineAt) {
		w.fail(parent, job, workerID, "deadline_exceeded", false, nil)
		return
	}
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = w.config.DefaultTimeout
	}
	if job.DeadlineAt != nil {
		untilDeadline := time.Until(*job.DeadlineAt)
		if untilDeadline > 0 && untilDeadline < timeout {
			timeout = untilDeadline
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	done := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	heartbeatFailed := make(chan error, 1)
	go w.heartbeat(ctx, cancel, done, heartbeatStopped, heartbeatFailed, job.ID, workerID)
	evidence, handlerErr := handler(ctx, job)
	executionContextErr := ctx.Err()
	close(done)
	cancel()
	<-heartbeatStopped
	select {
	case err := <-heartbeatFailed:
		slog.WarnContext(parent, "job lease lost", "job_id", job.ID, "kind", job.Kind, "error", err)
		return
	default:
	}
	if handlerErr == nil && executionContextErr == nil {
		if err := w.repo.Complete(parent, job.ID, workerID, defaultJSON(evidence)); err != nil && !errors.Is(err, context.Canceled) {
			slog.ErrorContext(parent, "job completion persistence failed", "job_id", job.ID, "kind", job.Kind, "error", err)
		}
		return
	}
	if parent.Err() != nil {
		return // shutdown: leave the lease for another replica to recover
	}
	if executionContextErr != nil {
		w.fail(parent, job, workerID, "job_timeout", true, json.RawMessage(`{"reason":"timeout"}`))
		return
	}
	code, retryable := ClassifyHandlerError(handlerErr)
	w.fail(parent, job, workerID, code, retryable, evidence)
}

func (w *Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopped chan<- struct{}, failed chan<- error, jobID uuid.UUID, workerID string) {
	defer close(stopped)
	interval := w.config.LeaseDuration / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := w.repo.Heartbeat(ctx, jobID, workerID, w.config.LeaseDuration); err != nil {
				select {
				case failed <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (w *Worker) fail(ctx context.Context, job Job, workerID, code string, retryable bool, evidence json.RawMessage) {
	_, err := w.repo.Fail(ctx, FailInput{
		JobID: job.ID, WorkerID: workerID, ErrorCode: code, Retryable: retryable,
		Backoff: w.config.Backoff(job.Attempts), Evidence: defaultJSON(evidence),
	})
	if err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "job failure persistence failed", "job_id", job.ID, "kind", job.Kind, "error", err)
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
	kinds := make([]string, 0, len(w.handlers))
	for kind := range w.handlers {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func ExponentialBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}
