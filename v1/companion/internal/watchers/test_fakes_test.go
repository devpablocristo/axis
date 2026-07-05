package watchers

import (
	"context"
	"sync"
	"time"

	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
	"github.com/google/uuid"
)

type fakeRepo struct {
	mu        sync.Mutex
	watchers  map[uuid.UUID]domain.Watcher
	proposals map[uuid.UUID][]domain.Proposal
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		watchers:  make(map[uuid.UUID]domain.Watcher),
		proposals: make(map[uuid.UUID][]domain.Proposal),
	}
}

func (r *fakeRepo) CreateWatcher(_ context.Context, watcher domain.Watcher) (domain.Watcher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if watcher.ID == uuid.Nil {
		watcher.ID = uuid.New()
	}
	now := time.Now().UTC()
	watcher.CreatedAt = now
	watcher.UpdatedAt = now
	r.watchers[watcher.ID] = watcher
	return watcher, nil
}

func (r *fakeRepo) GetWatcher(_ context.Context, id uuid.UUID) (domain.Watcher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	watcher, ok := r.watchers[id]
	if !ok {
		return domain.Watcher{}, ErrNotFound
	}
	return watcher, nil
}

func (r *fakeRepo) ListWatchers(_ context.Context, orgID string) ([]domain.Watcher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Watcher, 0, len(r.watchers))
	for _, watcher := range r.watchers {
		if watcher.OrgID == orgID {
			out = append(out, watcher)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListEnabledOrgIDs(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{})
	for _, watcher := range r.watchers {
		if watcher.Enabled {
			seen[watcher.OrgID] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for orgID := range seen {
		out = append(out, orgID)
	}
	return out, nil
}

func (r *fakeRepo) UpdateWatcher(_ context.Context, watcher domain.Watcher) (domain.Watcher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.watchers[watcher.ID]; !ok {
		return domain.Watcher{}, ErrNotFound
	}
	watcher.UpdatedAt = time.Now().UTC()
	r.watchers[watcher.ID] = watcher
	return watcher, nil
}

func (r *fakeRepo) DeleteWatcher(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.watchers[id]; !ok {
		return ErrNotFound
	}
	delete(r.watchers, id)
	return nil
}

func (r *fakeRepo) CreateProposal(_ context.Context, proposal domain.Proposal) (domain.Proposal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if proposal.ID == uuid.Nil {
		proposal.ID = uuid.New()
	}
	proposal.CreatedAt = time.Now().UTC()
	r.proposals[proposal.WatcherID] = append(r.proposals[proposal.WatcherID], proposal)
	return proposal, nil
}

func (r *fakeRepo) UpdateProposal(_ context.Context, proposal domain.Proposal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for watcherID, proposals := range r.proposals {
		for i := range proposals {
			if proposals[i].ID == proposal.ID {
				proposals[i] = proposal
				r.proposals[watcherID] = proposals
				return nil
			}
		}
	}
	return ErrNotFound
}

func (r *fakeRepo) ListProposalsByWatcher(_ context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	proposals := append([]domain.Proposal(nil), r.proposals[watcherID]...)
	if limit > 0 && limit < len(proposals) {
		proposals = proposals[:limit]
	}
	return proposals, nil
}

func (r *fakeRepo) PendingProposals(_ context.Context, orgID string) ([]domain.Proposal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Proposal
	for _, proposals := range r.proposals {
		for _, proposal := range proposals {
			if proposal.OrgID == orgID && proposal.ExecutionStatus == domain.ProposalPending {
				out = append(out, proposal)
			}
		}
	}
	return out, nil
}
