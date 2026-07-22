package watchers

import (
	"context"
	"testing"
	"time"

	auditdomain "github.com/devpablocristo/nexus-v2/internal/audit/usecases/domain"
	"github.com/google/uuid"
)

type fakeRepository struct {
	items []ExpiredApproval
	now   time.Time
	batch int
}

func (f *fakeRepository) ExpireApprovals(_ context.Context, now time.Time, batch int) ([]ExpiredApproval, error) {
	f.now, f.batch = now, batch
	return f.items, nil
}

type fakeAudit struct{ inputs []auditdomain.AppendInput }

func (f *fakeAudit) Append(_ context.Context, _ string, in auditdomain.AppendInput) (auditdomain.AuditEvent, error) {
	f.inputs = append(f.inputs, in)
	return auditdomain.AuditEvent{}, nil
}

func TestRunOnceExpiresAndAuditsEachApproval(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{items: []ExpiredApproval{{
		ID: uuid.New(), TenantID: "tenant-1", GovernanceCheckID: uuid.New(),
		VirployeeID: uuid.NewString(), BindingHash: "sha256:binding", ExpiresAt: now.Add(-time.Minute),
	}}}
	audit := &fakeAudit{}
	watcher := New(repo, audit)
	watcher.now = func() time.Time { return now }

	count, err := watcher.RunOnce(context.Background(), 25)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if count != 1 || repo.batch != 25 || !repo.now.Equal(now) {
		t.Fatalf("unexpected reconciliation: count=%d batch=%d now=%s", count, repo.batch, repo.now)
	}
	if len(audit.inputs) != 1 || audit.inputs[0].EventType != "approval_expired" {
		t.Fatalf("expected approval_expired audit event, got %+v", audit.inputs)
	}
	if _, ok := audit.inputs[0].Data["binding_hash"]; !ok {
		t.Fatal("audit event must bind the expiration to the governed action")
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		New(&fakeRepository{}, &fakeAudit{}).Run(ctx, time.Millisecond, 10)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}
