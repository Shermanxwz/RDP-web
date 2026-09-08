package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReplaceOwnerPasswordRevokesSessions(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "rdpweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	owner, err := s.CreateInitialOwner(ctx, "owner", "old-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "session-hash", owner.ID, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordLoginFailure(ctx, "failure-key", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := s.ReplaceOwnerPassword(ctx, owner.ID, "new-hash"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Owner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "new-hash" {
		t.Fatalf("password hash not replaced: %q", got.PasswordHash)
	}
	if _, err := s.SessionByHash(ctx, "session-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old session survived password rotation: %v", err)
	}
	count, err := s.LoginFailureCount(ctx, "failure-key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("login failure state survived password rotation: %d", count)
	}
}

func TestOwnerMissing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "rdpweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Owner(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
