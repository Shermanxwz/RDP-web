package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Owner returns the unique site owner. The database enforces that at most one
// user row can exist, so callers never need to choose between accounts.
func (s *Store) Owner(ctx context.Context) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash FROM users LIMIT 1`).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// ReplaceOwnerPassword atomically changes the web-owner password and revokes
// every active web session. RDP credentials are unrelated and are never
// touched by this operation.
func (s *Store) ReplaceOwnerPassword(ctx context.Context, userID int64, passwordHash string) error {
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("password hash is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, passwordHash, userID)
	if err != nil {
		return err
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	// There can only be one owner, so all persisted login-failure keys belong
	// to this account lifecycle. Clear them after a successful local/admin
	// password rotation so stale lockout state cannot survive the reset.
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_failures`); err != nil {
		return err
	}
	return tx.Commit()
}
