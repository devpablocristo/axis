package main

import (
	"context"
	"errors"
	"testing"
)

// updateMemberFailingIAMStore exercises the local IAM fallback path of
// updateIAMUser: UpdateMember reports the member is missing (errNotFound), so
// the code must fall back to UpsertMember. Here UpsertMember fails, and the
// returned error must surface to the caller.
type updateMemberFailingIAMStore struct {
	IAMStore
	upsertErr        error
	upsertMemberSeen bool
}

func (s *updateMemberFailingIAMStore) UpdateUser(_ context.Context, userID string, _ IAMUser) (IAMUser, error) {
	return IAMUser{ID: userID}, nil
}

func (s *updateMemberFailingIAMStore) UpdateMember(_ context.Context, _ string, _ string, _ IAMMember) (IAMMember, error) {
	return IAMMember{}, errNotFound
}

func (s *updateMemberFailingIAMStore) UpsertMember(_ context.Context, _ IAMMember) (IAMMember, error) {
	s.upsertMemberSeen = true
	return IAMMember{}, s.upsertErr
}

// TestUpdateIAMUserPropagatesUpsertMemberError guards against the swallowed
// error bug: when the identity provider is not configured (local IAM fallback)
// and the member does not yet exist, updateIAMUser must propagate a failure
// from the UpsertMember fallback instead of returning success.
func TestUpdateIAMUserPropagatesUpsertMemberError(t *testing.T) {
	wantErr := errors.New("upsert member boom")
	store := &updateMemberFailingIAMStore{upsertErr: wantErr}
	srv := &server{iam: store}

	_, err := srv.updateIAMUser(context.Background(), "org-1", "user-1", IAMUser{Role: "admin"})

	if !store.upsertMemberSeen {
		t.Fatalf("expected UpsertMember fallback to be invoked")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected upsert error to be propagated, got %v", err)
	}
}
