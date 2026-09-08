package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shermanxwz/RDP-web/internal/security"
	"github.com/Shermanxwz/RDP-web/internal/store"
)

func TestResetPasswordCommandReplacesPasswordAndRevokesSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RDPWEB_DATA_DIR", dir)
	db, err := store.Open(filepath.Join(dir, "rdpweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	oldPassword := "correct horse battery staple"
	oldHash, err := security.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.CreateInitialOwner(context.Background(), "owner", oldHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(context.Background(), "old-session", owner.ID, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	newPassword := "new correct horse battery staple"
	var out bytes.Buffer
	if err := resetPasswordCommand([]string{"--password-stdin"}, strings.NewReader(newPassword+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), newPassword) || strings.Contains(out.String(), oldPassword) {
		t.Fatal("reset output leaked a password")
	}

	db, err = store.Open(filepath.Join(dir, "rdpweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Owner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if security.VerifyPassword(got.PasswordHash, oldPassword) {
		t.Fatal("old password still verifies")
	}
	if !security.VerifyPassword(got.PasswordHash, newPassword) {
		t.Fatal("new password does not verify")
	}
	if _, err := db.SessionByHash(context.Background(), "old-session"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old session survived reset: %v", err)
	}
}

func TestResetPasswordCommandRequiresExistingDatabase(t *testing.T) {
	t.Setenv("RDPWEB_DATA_DIR", t.TempDir())
	err := resetPasswordCommand([]string{"--password-stdin"}, strings.NewReader("long enough replacement password"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "database not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResetPasswordCommandRequiresStdinMode(t *testing.T) {
	err := resetPasswordCommand(nil, strings.NewReader("long enough replacement password"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("unexpected error: %v", err)
	}
}
