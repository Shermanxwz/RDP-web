package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	UserID    int64
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

type Device struct {
	ID                string `json:"id"`
	GroupID           string `json:"groupId"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	Domain            string `json:"domain"`
	Gateway           string `json:"gateway"`
	Favorite          bool   `json:"favorite"`
	Notes             string `json:"notes"`
	UseMultimon       bool   `json:"useMultimon"`
	RedirectClipboard bool   `json:"redirectClipboard"`
	AudioMode         int    `json:"audioMode"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

func Open(path string) (*Store, error) {
	path = filepath.Clean(path)
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS groups (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_groups_user ON groups(user_id, sort_order, name);
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id TEXT REFERENCES groups(id) ON DELETE SET NULL,
  name TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 3389 CHECK(port BETWEEN 1 AND 65535),
  username TEXT NOT NULL DEFAULT '',
  domain TEXT NOT NULL DEFAULT '',
  gateway TEXT NOT NULL DEFAULT '',
  favorite INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  use_multimon INTEGER NOT NULL DEFAULT 0,
  redirect_clipboard INTEGER NOT NULL DEFAULT 1,
  audio_mode INTEGER NOT NULL DEFAULT 0 CHECK(audio_mode BETWEEN 0 AND 2),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, host, port, username, domain)
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id, favorite DESC, name);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\r\n\t") {
		return User{}, errors.New("username must be 3-64 characters")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,created_at) VALUES(?,?,?)`, username, passwordHash, now)
	if err != nil {
		return User{}, err
	}
	id, _ := result.LastInsertId()
	return User{ID: id, Username: username, PasswordHash: passwordHash}, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash FROM users WHERE username=?`, strings.TrimSpace(username)).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,created_at) VALUES(?,?,?,?,?)`, tokenHash, userID, csrf, expires.UTC().Format(time.RFC3339Nano), now)
	return err
}

func (s *Store) SessionByHash(ctx context.Context, tokenHash string) (Session, error) {
	var sess Session
	var expiry string
	err := s.db.QueryRowContext(ctx, `SELECT s.user_id,u.username,s.csrf_token,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, tokenHash).Scan(&sess.UserID, &sess.Username, &sess.CSRFToken, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
	if err != nil || time.Now().After(sess.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
		return Session{}, ErrNotFound
	}
	return sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *Store) ListGroups(ctx context.Context, userID int64) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,sort_order FROM groups WHERE user_id=? ORDER BY sort_order,name COLLATE NOCASE`, userID)
	if err != nil { return nil, err }
	defer rows.Close()
	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.SortOrder); err != nil { return nil, err }
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, userID int64, g Group) error {
	if err := ValidateGroup(g); err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO groups(id,user_id,name,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?)`, g.ID, userID, strings.TrimSpace(g.Name), g.SortOrder, now, now)
	return err
}

func (s *Store) UpdateGroup(ctx context.Context, userID int64, g Group) error {
	if err := ValidateGroup(g); err != nil { return err }
	result, err := s.db.ExecContext(ctx, `UPDATE groups SET name=?,sort_order=?,updated_at=? WHERE id=? AND user_id=?`, strings.TrimSpace(g.Name), g.SortOrder, time.Now().UTC().Format(time.RFC3339Nano), g.ID, userID)
	if err != nil { return err }
	return requireAffected(result)
}

func (s *Store) DeleteGroup(ctx context.Context, userID int64, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id=? AND user_id=?`, id, userID)
	if err != nil { return err }
	return requireAffected(result)
}

func (s *Store) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(group_id,''),name,host,port,username,domain,gateway,favorite,notes,use_multimon,redirect_clipboard,audio_mode,created_at,updated_at FROM devices WHERE user_id=? ORDER BY favorite DESC,name COLLATE NOCASE`, userID)
	if err != nil { return nil, err }
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID,&d.GroupID,&d.Name,&d.Host,&d.Port,&d.Username,&d.Domain,&d.Gateway,&d.Favorite,&d.Notes,&d.UseMultimon,&d.RedirectClipboard,&d.AudioMode,&d.CreatedAt,&d.UpdatedAt); err != nil { return nil, err }
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, userID int64, id string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(group_id,''),name,host,port,username,domain,gateway,favorite,notes,use_multimon,redirect_clipboard,audio_mode,created_at,updated_at FROM devices WHERE id=? AND user_id=?`, id, userID).Scan(&d.ID,&d.GroupID,&d.Name,&d.Host,&d.Port,&d.Username,&d.Domain,&d.Gateway,&d.Favorite,&d.Notes,&d.UseMultimon,&d.RedirectClipboard,&d.AudioMode,&d.CreatedAt,&d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) { return Device{}, ErrNotFound }
	return d, err
}

func (s *Store) CreateDevice(ctx context.Context, userID int64, d Device) error {
	if err := ValidateDevice(d); err != nil { return err }
	if err := s.validateGroupOwner(ctx, userID, d.GroupID); err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var group any
	if d.GroupID != "" { group = d.GroupID }
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,user_id,group_id,name,host,port,username,domain,gateway,favorite,notes,use_multimon,redirect_clipboard,audio_mode,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID,userID,group,strings.TrimSpace(d.Name),strings.TrimSpace(d.Host),d.Port,strings.TrimSpace(d.Username),strings.TrimSpace(d.Domain),strings.TrimSpace(d.Gateway),d.Favorite,strings.TrimSpace(d.Notes),d.UseMultimon,d.RedirectClipboard,d.AudioMode,now,now)
	return err
}

func (s *Store) UpdateDevice(ctx context.Context, userID int64, d Device) error {
	if err := ValidateDevice(d); err != nil { return err }
	if err := s.validateGroupOwner(ctx, userID, d.GroupID); err != nil { return err }
	var group any
	if d.GroupID != "" { group = d.GroupID }
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET group_id=?,name=?,host=?,port=?,username=?,domain=?,gateway=?,favorite=?,notes=?,use_multimon=?,redirect_clipboard=?,audio_mode=?,updated_at=? WHERE id=? AND user_id=?`, group,strings.TrimSpace(d.Name),strings.TrimSpace(d.Host),d.Port,strings.TrimSpace(d.Username),strings.TrimSpace(d.Domain),strings.TrimSpace(d.Gateway),d.Favorite,strings.TrimSpace(d.Notes),d.UseMultimon,d.RedirectClipboard,d.AudioMode,time.Now().UTC().Format(time.RFC3339Nano),d.ID,userID)
	if err != nil { return err }
	return requireAffected(result)
}

func (s *Store) DeleteDevice(ctx context.Context, userID int64, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id=? AND user_id=?`, id, userID)
	if err != nil { return err }
	return requireAffected(result)
}

func (s *Store) validateGroupOwner(ctx context.Context, userID int64, groupID string) error {
	if groupID == "" { return nil }
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE id=? AND user_id=?`, groupID, userID).Scan(&count); err != nil { return err }
	if count != 1 { return errors.New("invalid group") }
	return nil
}

func ValidateGroup(g Group) error {
	if strings.TrimSpace(g.ID) == "" || len(g.ID) > 64 { return errors.New("invalid group id") }
	name := strings.TrimSpace(g.Name)
	if name == "" || len(name) > 100 || hasLineBreak(name) { return errors.New("group name must be 1-100 characters") }
	if g.SortOrder < -100000 || g.SortOrder > 100000 { return errors.New("invalid group sort order") }
	return nil
}

func ValidateDevice(d Device) error {
	if strings.TrimSpace(d.ID) == "" || len(d.ID) > 64 { return errors.New("invalid device id") }
	if v := strings.TrimSpace(d.Name); v == "" || len(v) > 120 || hasLineBreak(v) { return errors.New("device name must be 1-120 characters") }
	host := strings.TrimSpace(d.Host)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "\r\n\t /\\") || strings.Contains(host, "://") { return errors.New("invalid host") }
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") { host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]") }
	if ip := net.ParseIP(host); ip == nil {
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") { return errors.New("invalid host") }
			for _, r := range label { if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') { return errors.New("invalid host") } }
		}
	}
	if d.Port < 1 || d.Port > 65535 { return errors.New("port must be between 1 and 65535") }
	for name, value := range map[string]string{"username": d.Username, "domain": d.Domain, "gateway": d.Gateway} {
		if len(value) > 255 || hasLineBreak(value) { return fmt.Errorf("invalid %s", name) }
	}
	if d.Gateway != "" {
		if _, _, err := net.SplitHostPort(d.Gateway); err != nil {
			gatewayHost := strings.Trim(d.Gateway, "[]")
			if net.ParseIP(gatewayHost) == nil && strings.ContainsAny(gatewayHost, " /\\") { return errors.New("invalid gateway") }
		}
	}
	if len(d.Notes) > 4000 { return errors.New("notes too long") }
	if d.AudioMode < 0 || d.AudioMode > 2 { return errors.New("audio mode must be 0, 1 or 2") }
	return nil
}

func Address(d Device) string {
	host := strings.TrimSpace(d.Host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") { host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]") }
	return net.JoinHostPort(host, strconv.Itoa(d.Port))
}

func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil { return err }
	if n == 0 { return ErrNotFound }
	return nil
}

func hasLineBreak(value string) bool { return strings.ContainsAny(value, "\r\n\x00") }
