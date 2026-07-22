package learning

import (
	"context"
	"errors"
	"testing"

	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

type fakeChecker struct {
	active bool
	err    error
}

func (f fakeChecker) IsActiveCapability(context.Context, string, string) (bool, error) {
	return f.active, f.err
}

func goodProposal() Proposal {
	return Proposal{
		ID:            uuid.New(),
		OrgID:         "organization-1",
		VirployeeID:   uuid.New(),
		CapabilityKey: "calendar.events.create",
		Title:         "Learned procedure: calendar.events.create",
		Content:       "1. Interpret the request. 2. Pass the gate. 3. Execute once.",
		Status:        StatusPending,
	}
}

func TestEvaluateGates(t *testing.T) {
	cases := []struct {
		name    string
		checker CapabilityChecker
		mutate  func(*Proposal)
		want    bool
		failKey string
	}{
		{"all pass", fakeChecker{active: true}, nil, true, ""},
		{"nil checker fails closed", nil, nil, false, "capability_real"},
		{"unknown capability blocks", fakeChecker{active: false}, nil, false, "capability_real"},
		{"secret in content blocks", fakeChecker{active: true}, func(p *Proposal) { p.Content += "\napi_key = sk-live-123" }, false, "no_secrets"},
		{"bearer token blocks", fakeChecker{active: true}, func(p *Proposal) { p.Content += "\nAuthorization: Bearer abcdef123456" }, false, "no_secrets"},
		{"email pii blocks", fakeChecker{active: true}, func(p *Proposal) { p.Content += "\ncontact ana@example.com" }, false, "no_pii"},
		{"empty content blocks", fakeChecker{active: true}, func(p *Proposal) { p.Content = "  " }, false, "installable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := goodProposal()
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			report, err := Evaluate(context.Background(), tc.checker, p)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if report.Passed != tc.want {
				t.Fatalf("passed = %v, want %v (checks: %+v)", report.Passed, tc.want, report.Checks)
			}
			if !tc.want {
				found := false
				for _, c := range report.Checks {
					if c.Key == tc.failKey && c.Status == EvalBlocked {
						found = true
					}
				}
				if !found {
					t.Fatalf("expected blocked check %q, got %+v", tc.failKey, report.Checks)
				}
			}
		})
	}
}

func TestEvaluatePropagatesCheckerError(t *testing.T) {
	if _, err := Evaluate(context.Background(), fakeChecker{err: errors.New("db down")}, goodProposal()); err == nil {
		t.Fatal("expected checker error to propagate")
	}
}

// --- Accept / Dismiss ---

type decideRepo struct {
	proposal   Proposal
	getErr     error
	decideErr  error
	decideCall int
	attachCall int
	lastStatus string
	lastMemory *uuid.UUID
	attached   *uuid.UUID
}

func (r *decideRepo) Create(context.Context, string, NormalizedCreateInput) (Proposal, error) {
	return Proposal{}, nil
}
func (r *decideRepo) List(context.Context, string, string, *uuid.UUID) ([]Proposal, error) {
	return nil, nil
}
func (r *decideRepo) Get(context.Context, string, uuid.UUID) (Proposal, error) {
	return r.proposal, r.getErr
}
func (r *decideRepo) Decide(_ context.Context, _ string, _ uuid.UUID, status, decidedBy string, memoryID *uuid.UUID) (Proposal, error) {
	r.decideCall++
	r.lastStatus = status
	r.lastMemory = memoryID
	if r.decideErr != nil {
		return Proposal{}, r.decideErr
	}
	// Persist the decision onto the fake's row, as the real UPDATE would.
	r.proposal.Status = status
	r.proposal.DecidedBy = decidedBy
	r.proposal.MemoryID = memoryID
	return r.proposal, nil
}
func (r *decideRepo) AttachMemory(_ context.Context, _ string, _, memoryID uuid.UUID) (Proposal, error) {
	r.attachCall++
	id := memoryID
	r.attached = &id
	// RETURNING reflects the persisted row: the decided_by set by Decide plus
	// the freshly attached memory id.
	r.proposal.MemoryID = &id
	return r.proposal, nil
}
func (r *decideRepo) Candidates(context.Context, string, int) ([]Candidate, error) { return nil, nil }
func (r *decideRepo) LatestForPair(context.Context, string, uuid.UUID, string) (*Proposal, error) {
	return nil, nil
}
func (r *decideRepo) SuccessfulExecutionTraceIDs(context.Context, string, uuid.UUID, string, int) ([]string, error) {
	return nil, nil
}

type spyInstaller struct {
	calls   int
	id      uuid.UUID
	err     error
	lastSrc string
}

func (s *spyInstaller) InstallProcedure(_ context.Context, _ string, _ uuid.UUID, _, source, _, _ string) (uuid.UUID, error) {
	s.calls++
	s.lastSrc = source
	return s.id, s.err
}

// permissiveAuthz allows every decision unless err is set.
type permissiveAuthz struct{ err error }

func (a permissiveAuthz) Authorize(context.Context, string, uuid.UUID, string, string) error {
	return a.err
}

func acceptUseCases(repo RepositoryPort, installer MemoryInstaller, checker CapabilityChecker) *UseCases {
	u := NewUseCases(repo)
	u.SetMemoryInstaller(installer)
	u.SetCapabilityChecker(checker)
	u.SetAuthorizer(permissiveAuthz{})
	return u
}

func TestAcceptClaimsBeforeInstallingAndPinsMemory(t *testing.T) {
	p := goodProposal()
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{id: uuid.New()}
	u := acceptUseCases(repo, inst, fakeChecker{active: true})

	res, err := u.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin")
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if inst.calls != 1 {
		t.Fatalf("expected exactly one install, got %d", inst.calls)
	}
	// Claim-first: the pending→accepted Decide runs before the install, with no
	// memory id yet; the memory id is attached afterwards.
	if repo.decideCall != 1 || repo.lastStatus != StatusAccepted || repo.lastMemory != nil {
		t.Fatalf("claim must run first with nil memory id, got calls=%d status=%q mem=%v", repo.decideCall, repo.lastStatus, repo.lastMemory)
	}
	if repo.attachCall != 1 || repo.attached == nil || *repo.attached != inst.id {
		t.Fatalf("expected the installed memory id attached, got %v", repo.attached)
	}
	// The returned proposal must carry the decision provenance (finding #21).
	if res.Proposal.Status != StatusAccepted || res.Proposal.MemoryID == nil || *res.Proposal.MemoryID != inst.id {
		t.Fatalf("returned proposal must carry accepted status + memory id, got %+v", res.Proposal)
	}
	if res.Proposal.DecidedBy != "user_42" {
		t.Fatalf("returned proposal must carry decided_by, got %q", res.Proposal.DecidedBy)
	}
	if !res.Eval.Passed {
		t.Fatalf("eval must be reported as passed")
	}
	if inst.lastSrc != "learning-proposal:"+p.ID.String() {
		t.Fatalf("unexpected provenance source: %q", inst.lastSrc)
	}
}

func TestAcceptIdempotentReportsPassed(t *testing.T) {
	p := goodProposal()
	p.Status = StatusAccepted
	memID := uuid.New()
	p.MemoryID = &memID
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{id: uuid.New()}
	u := acceptUseCases(repo, inst, fakeChecker{active: true})

	res, err := u.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin")
	if err != nil {
		t.Fatalf("Accept idempotent: %v", err)
	}
	if inst.calls != 0 || repo.decideCall != 0 {
		t.Fatalf("re-accepting an accepted proposal must not install or decide again (installs=%d decides=%d)", inst.calls, repo.decideCall)
	}
	// Finding #1: the idempotent replay must not report passed=false.
	if !res.Eval.Passed {
		t.Fatalf("idempotent replay must report eval passed, got %+v", res.Eval)
	}
}

func TestAcceptSelfHealsWhenMemoryIDMissing(t *testing.T) {
	// Accepted but no memory id pinned (a prior install failed after the claim):
	// re-accept must install and attach, not skip.
	p := goodProposal()
	p.Status = StatusAccepted
	p.MemoryID = nil
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{id: uuid.New()}
	u := acceptUseCases(repo, inst, fakeChecker{active: true})

	if _, err := u.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin"); err != nil {
		t.Fatalf("self-heal accept: %v", err)
	}
	if inst.calls != 1 || repo.attachCall != 1 {
		t.Fatalf("expected a self-healing install+attach, got installs=%d attach=%d", inst.calls, repo.attachCall)
	}
	if repo.decideCall != 0 {
		t.Fatalf("already-accepted proposal must not be re-claimed")
	}
}

func TestAcceptRefusedWhenEvalFails(t *testing.T) {
	p := goodProposal()
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{id: uuid.New()}
	u := acceptUseCases(repo, inst, fakeChecker{active: false}) // capability not real

	_, err := u.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin")
	if !domainerr.IsValidation(err) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if inst.calls != 0 || repo.decideCall != 0 {
		t.Fatalf("a failed eval must install nothing and decide nothing (installs=%d decides=%d)", inst.calls, repo.decideCall)
	}
}

func TestAcceptFailsClosedWithoutInstallerOrAuthz(t *testing.T) {
	p := goodProposal()

	// No installer wired.
	noInstaller := &decideRepo{proposal: p}
	u1 := NewUseCases(noInstaller)
	u1.SetCapabilityChecker(fakeChecker{active: true})
	u1.SetAuthorizer(permissiveAuthz{})
	if _, err := u1.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin"); !domainerr.IsValidation(err) {
		t.Fatalf("expected fail-closed without installer, got %v", err)
	}
	if noInstaller.decideCall != 0 {
		t.Fatalf("must not mark accepted when it could not install")
	}

	// No authorizer wired.
	noAuthz := &decideRepo{proposal: p}
	u2 := NewUseCases(noAuthz)
	u2.SetCapabilityChecker(fakeChecker{active: true})
	u2.SetMemoryInstaller(&spyInstaller{id: uuid.New()})
	if _, err := u2.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin"); !domainerr.IsValidation(err) {
		t.Fatalf("expected fail-closed without authorizer, got %v", err)
	}
}

func TestAcceptDeniedByAuthorizer(t *testing.T) {
	p := goodProposal()
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{id: uuid.New()}
	u := NewUseCases(repo)
	u.SetCapabilityChecker(fakeChecker{active: true})
	u.SetMemoryInstaller(inst)
	u.SetAuthorizer(permissiveAuthz{err: domainerr.Forbidden("not the supervisor")})

	if _, err := u.Accept(context.Background(), "organization-1", p.ID, "member", "member"); err == nil {
		t.Fatal("expected authorization to deny an unprivileged actor")
	}
	if inst.calls != 0 || repo.decideCall != 0 {
		t.Fatalf("a denied accept must install nothing and decide nothing (installs=%d decides=%d)", inst.calls, repo.decideCall)
	}
}

func TestAcceptTreatsMemoryConflictAsInstalled(t *testing.T) {
	p := goodProposal()
	repo := &decideRepo{proposal: p}
	inst := &spyInstaller{err: domainerr.Conflict("an active memory with the same content already exists")}
	u := acceptUseCases(repo, inst, fakeChecker{active: true})

	res, err := u.Accept(context.Background(), "organization-1", p.ID, "user_42", "admin")
	if err != nil {
		t.Fatalf("conflict must be treated as installed, got %v", err)
	}
	// The claim still happened (before the install attempt).
	if repo.decideCall != 1 || repo.lastStatus != StatusAccepted {
		t.Fatalf("proposal should be claimed accepted, got calls=%d status=%q", repo.decideCall, repo.lastStatus)
	}
	if repo.attachCall != 0 {
		t.Fatalf("no memory id is known on conflict, so nothing to attach, got attach=%d", repo.attachCall)
	}
	if res.Proposal.Status != StatusAccepted {
		t.Fatalf("expected accepted, got %q", res.Proposal.Status)
	}
}

func TestDismissPendingAndGuards(t *testing.T) {
	dismissUC := func(repo RepositoryPort) *UseCases {
		u := NewUseCases(repo)
		u.SetAuthorizer(permissiveAuthz{})
		return u
	}

	p := goodProposal()
	repo := &decideRepo{proposal: p}
	out, err := dismissUC(repo).Dismiss(context.Background(), "organization-1", p.ID, "user_42", "admin")
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if out.Status != StatusDismissed || repo.lastStatus != StatusDismissed {
		t.Fatalf("expected dismissed, got %q", out.Status)
	}

	// Accepted cannot be dismissed.
	accepted := &decideRepo{proposal: func() Proposal { q := goodProposal(); q.Status = StatusAccepted; return q }()}
	if _, err := dismissUC(accepted).Dismiss(context.Background(), "organization-1", accepted.proposal.ID, "u", "admin"); !domainerr.IsValidation(err) {
		t.Fatalf("dismissing an accepted proposal must fail, got %v", err)
	}
	if accepted.decideCall != 0 {
		t.Fatal("must not decide when dismissing an accepted proposal")
	}

	// Dismiss is idempotent.
	dismissed := &decideRepo{proposal: func() Proposal { q := goodProposal(); q.Status = StatusDismissed; return q }()}
	if _, err := dismissUC(dismissed).Dismiss(context.Background(), "organization-1", dismissed.proposal.ID, "u", "admin"); err != nil {
		t.Fatalf("re-dismiss should be idempotent, got %v", err)
	}
	if dismissed.decideCall != 0 {
		t.Fatal("idempotent dismiss must not decide again")
	}

	// Dismiss fails closed without an authorizer.
	noAuthz := &decideRepo{proposal: goodProposal()}
	if _, err := NewUseCases(noAuthz).Dismiss(context.Background(), "organization-1", noAuthz.proposal.ID, "u", "admin"); !domainerr.IsValidation(err) {
		t.Fatalf("expected fail-closed dismiss without authorizer, got %v", err)
	}
}
