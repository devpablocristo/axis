package outbox

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Sender interface {
	Send(context.Context, Message) error
}

type SenderFunc func(context.Context, Message) error

func (f SenderFunc) Send(ctx context.Context, message Message) error { return f(ctx, message) }

type DispatcherConfig struct {
	WorkerID      string
	Concurrency   int
	PollInterval  time.Duration
	Lease         time.Duration
	Timeout       time.Duration
	RecoveryBatch int
	BaseBackoff   time.Duration
}

type Dispatcher struct {
	repository RepositoryPort
	sender     Sender
	config     DispatcherConfig
}

func NewDispatcher(repository RepositoryPort, sender Sender, config DispatcherConfig) *Dispatcher {
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.WorkerID == "" {
		config.WorkerID = "governance-outbox-" + uuid.NewString()
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 1
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.Lease <= 0 {
		config.Lease = 30 * time.Second
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.RecoveryBatch <= 0 {
		config.RecoveryBatch = 100
	}
	if config.BaseBackoff <= 0 {
		config.BaseBackoff = 5 * time.Second
	}
	return &Dispatcher{repository: repository, sender: sender, config: config}
}

func (d *Dispatcher) Run(ctx context.Context) {
	var waitGroup sync.WaitGroup
	for slot := 0; slot < d.config.Concurrency; slot++ {
		waitGroup.Add(1)
		go func(slot int) {
			defer waitGroup.Done()
			d.runSlot(ctx, slot)
		}(slot)
	}
	waitGroup.Wait()
}

func (d *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	if _, err := d.repository.RecoverExpiredLeases(ctx, d.config.RecoveryBatch); err != nil {
		return 0, err
	}
	messages, err := d.repository.Claim(ctx, ClaimOptions{
		WorkerID: d.config.WorkerID, Batch: d.config.Concurrency, Lease: d.config.Lease,
	})
	if err != nil {
		return 0, err
	}
	var waitGroup sync.WaitGroup
	for _, message := range messages {
		message := message
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			d.deliver(ctx, message, d.config.WorkerID)
		}()
	}
	waitGroup.Wait()
	return len(messages), nil
}

func (d *Dispatcher) runSlot(ctx context.Context, slot int) {
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()
	workerID := d.config.WorkerID + ":" + strconv.Itoa(slot)
	for {
		if ctx.Err() != nil {
			return
		}
		if slot == 0 {
			result, err := d.repository.RecoverExpiredLeases(ctx, d.config.RecoveryBatch)
			if err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "outbox lease recovery failed", "error", err)
			} else if result.Pending > 0 || result.Dead > 0 {
				slog.InfoContext(ctx, "outbox leases recovered", "pending", result.Pending, "dead", result.Dead)
			}
		}
		messages, err := d.repository.Claim(ctx, ClaimOptions{WorkerID: workerID, Batch: 1, Lease: d.config.Lease})
		if err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "outbox claim failed", "error", err)
			}
		} else if len(messages) == 1 {
			d.deliver(ctx, messages[0], workerID)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) deliver(parent context.Context, message Message, workerID string) {
	if d.sender == nil {
		d.fail(parent, message, workerID, "sender_unconfigured", false)
		return
	}
	ctx, cancel := context.WithTimeout(parent, d.config.Timeout)
	done := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	heartbeatFailed := make(chan error, 1)
	go d.heartbeat(ctx, cancel, done, heartbeatStopped, heartbeatFailed, message.ID, workerID)
	deliveryErr := d.sender.Send(ctx, message)
	deliveryContextErr := ctx.Err()
	close(done)
	cancel()
	<-heartbeatStopped
	select {
	case err := <-heartbeatFailed:
		slog.WarnContext(parent, "outbox lease lost", "outbox_id", message.ID, "error", err)
		return
	default:
	}
	if deliveryErr == nil && deliveryContextErr == nil {
		if err := d.repository.MarkDelivered(parent, message.ID, workerID); err != nil && parent.Err() == nil {
			slog.ErrorContext(parent, "outbox delivery persistence failed", "outbox_id", message.ID, "error", err)
		}
		return
	}
	if parent.Err() != nil {
		return
	}
	if deliveryContextErr != nil {
		d.fail(parent, message, workerID, "delivery_timeout", true)
		return
	}
	code, retryable := classifyError(deliveryErr)
	d.fail(parent, message, workerID, code, retryable)
}

func (d *Dispatcher) heartbeat(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopped chan<- struct{}, failed chan<- error, id uuid.UUID, workerID string) {
	defer close(stopped)
	interval := d.config.Lease / 3
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
			if err := d.repository.Heartbeat(ctx, id, workerID, d.config.Lease); err != nil {
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

func (d *Dispatcher) fail(ctx context.Context, message Message, workerID, code string, retryable bool) {
	_, err := d.repository.MarkFailed(ctx, message.ID, workerID, code, retryable, d.backoff(message.Attempts))
	if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
		slog.ErrorContext(ctx, "outbox failure persistence failed", "outbox_id", message.ID, "error", err)
	}
}

func (d *Dispatcher) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > MaxDeliveryAttempts {
		attempt = MaxDeliveryAttempts
	}
	return d.config.BaseBackoff * time.Duration(1<<uint(attempt-1))
}
