from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if new in text:
        return text
    if old not in text:
        raise SystemExit(f"missing patch anchor: {label}")
    return text.replace(old, new, 1)


# --- store hardening -------------------------------------------------------
p = Path("internal/store/store.go")
s = p.read_text()

s = replace_once(
    s,
    'var ErrNotFound = errors.New("not found")',
    'var ErrNotFound = errors.New("not found")\nvar ErrSetupComplete = errors.New("setup is already complete")',
    "store sentinel",
)

s = replace_once(
    s,
    '''CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,''',
    '''CREATE UNIQUE INDEX IF NOT EXISTS idx_users_single_owner ON users((1));
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,''',
    "single-owner DB invariant",
)

s = replace_once(
    s,
    'CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);',
    '''CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS login_failures (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  failure_key TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_login_failures_key_time ON login_failures(failure_key, created_at);''',
    "login throttle schema",
)

marker = 'func (s *Store) UserCount(ctx context.Context) (int, error) {'
insert = r'''func (s *Store) Health(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

func (s *Store) CreateInitialOwner(ctx context.Context, username, passwordHash string) (User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\r\n\t") {
		return User{}, errors.New("username must be 3-64 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, ErrSetupComplete
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `INSERT INTO users(username,password_hash,created_at) VALUES(?,?,?)`, username, passwordHash, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrSetupComplete
		}
		return User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash}, nil
}

'''
if "func (s *Store) CreateInitialOwner" not in s:
    if marker not in s:
        raise SystemExit("missing patch anchor: store owner methods")
    s = s.replace(marker, insert + marker, 1)

marker = 'func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {'
insert = r'''func (s *Store) LoginFailureCount(ctx context.Context, key string, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_failures WHERE failure_key=? AND created_at>=?`, key, since.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}

func (s *Store) RecordLoginFailure(ctx context.Context, key string, now time.Time) error {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM login_failures WHERE created_at < ?`, now.Add(-24*time.Hour).UTC().Format(time.RFC3339Nano))
	_, err := s.db.ExecContext(ctx, `INSERT INTO login_failures(failure_key,created_at) VALUES(?,?)`, key, now.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ClearLoginFailures(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_failures WHERE failure_key=?`, key)
	return err
}

'''
if "func (s *Store) LoginFailureCount" not in s:
    if marker not in s:
        raise SystemExit("missing patch anchor: login throttle methods")
    s = s.replace(marker, insert + marker, 1)

marker = 'func ValidateGroup(g Group) error {'
insert = r'''type RestorePayload struct {
	Groups  []Group
	Devices []Device
}

func (s *Store) RestoreReplace(ctx context.Context, userID int64, payload RestorePayload) error {
	if len(payload.Groups) > 500 || len(payload.Devices) > 5000 {
		return errors.New("restore is too large")
	}
	groups := make(map[string]struct{}, len(payload.Groups))
	for _, g := range payload.Groups {
		if err := ValidateGroup(g); err != nil {
			return err
		}
		if _, exists := groups[g.ID]; exists {
			return errors.New("duplicate group id")
		}
		groups[g.ID] = struct{}{}
	}
	devices := make(map[string]struct{}, len(payload.Devices))
	for _, d := range payload.Devices {
		if err := ValidateDevice(d); err != nil {
			return err
		}
		if _, exists := devices[d.ID]; exists {
			return errors.New("duplicate device id")
		}
		devices[d.ID] = struct{}{}
		if d.GroupID != "" {
			if _, ok := groups[d.GroupID]; !ok {
				return errors.New("device references unknown group")
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE user_id=?`, userID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, g := range payload.Groups {
		if _, err := tx.ExecContext(ctx, `INSERT INTO groups(id,user_id,name,sort_order,created_at,updated_at) VALUES(?,?,?,?,?,?)`, g.ID, userID, strings.TrimSpace(g.Name), g.SortOrder, now, now); err != nil {
			return err
		}
	}
	for _, d := range payload.Devices {
		var group any
		if d.GroupID != "" {
			group = d.GroupID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id,user_id,group_id,name,host,port,username,domain,gateway,favorite,notes,use_multimon,redirect_clipboard,audio_mode,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, userID, group, strings.TrimSpace(d.Name), strings.TrimSpace(d.Host), d.Port, strings.TrimSpace(d.Username), strings.TrimSpace(d.Domain), strings.TrimSpace(d.Gateway), d.Favorite, strings.TrimSpace(d.Notes), d.UseMultimon, d.RedirectClipboard, d.AudioMode, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

'''
if "func (s *Store) RestoreReplace" not in s:
    if marker not in s:
        raise SystemExit("missing patch anchor: restore")
    s = s.replace(marker, insert + marker, 1)

p.write_text(s)


# --- security helper -------------------------------------------------------
p = Path("internal/security/security.go")
s = p.read_text()
if "func ConstantEqual" not in s:
    s += r'''

func ConstantEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
'''
p.write_text(s)


# --- HTTP application hardening ------------------------------------------
p = Path("internal/app/app.go")
a = p.read_text()

a = replace_once(a, '\t"mime"\n\t"net/http"', '\t"mime"\n\t"net"\n\t"net/http"', "net import")

a = replace_once(
    a,
    '''\tPublicURL    string
\tSecureCookie bool
\tSessionTTL   time.Duration''',
    '''\tPublicURL    string
\tSecureCookie bool
\tSessionTTL   time.Duration
\tSetupToken   string''',
    "app config setup token",
)

a = replace_once(
    a,
    '''\tsecureCookie bool
\tsessionTTL   time.Duration
\tstatic       fs.FS''',
    '''\tsecureCookie bool
\tsessionTTL   time.Duration
\tsetupToken   string
\tstatic       fs.FS''',
    "app state setup token",
)

a = replace_once(
    a,
    'return &App{store: cfg.Store, logger: cfg.Logger, publicOrigin: origin, secureCookie: cfg.SecureCookie, sessionTTL: cfg.SessionTTL, static: assets}, nil',
    'return &App{store: cfg.Store, logger: cfg.Logger, publicOrigin: origin, secureCookie: cfg.SecureCookie, sessionTTL: cfg.SessionTTL, setupToken: cfg.SetupToken, static: assets}, nil',
    "app constructor",
)

a = replace_once(
    a,
    '''func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
\tstart := time.Now()
\tww := &statusWriter{ResponseWriter: w, status: 200}
\ta.securityHeaders(ww)''',
    '''func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
\tstart := time.Now()
\tww := &statusWriter{ResponseWriter: w, status: 200}
\ta.securityHeaders(ww)
\tif strings.HasPrefix(r.URL.Path, "/api/") {
\t\tww.Header().Set("Cache-Control", "no-store")
\t}''',
    "API cache policy",
)

a = replace_once(
    a,
    '''\tif r.URL.Path == "/healthz" {
\t\twriteJSON(w, http.StatusOK, map[string]any{"ok": true})
\t\treturn
\t}''',
    '''\tif r.URL.Path == "/healthz" {
\t\tif err := a.store.Health(r.Context()); err != nil {
\t\t\twriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
\t\t\treturn
\t\t}
\t\twriteJSON(w, http.StatusOK, map[string]any{"ok": true})
\t\treturn
\t}''',
    "DB-backed health",
)

a = replace_once(
    a,
    '''\tcase r.Method == http.MethodPost && r.URL.Path == "/api/import":
\t\ta.importData(w, r)
\tdefault:''',
    '''\tcase r.Method == http.MethodPost && r.URL.Path == "/api/import":
\t\ta.importData(w, r)
\tcase r.Method == http.MethodPost && r.URL.Path == "/api/restore":
\t\ta.restoreData(w, r)
\tdefault:''',
    "restore route",
)

old_setup = r'''func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid origin")
		return
	}
	count, err := a.store.UserCount(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	if count != 0 {
		writeError(w, http.StatusConflict, "setup is already complete")
		return
	}
	var in struct{ Username, Password string }
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := a.store.CreateUser(r.Context(), in.Username, hash)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to create owner account")
		return
	}
	a.startSession(w, r, user)
}'''
new_setup = r'''func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid origin")
		return
	}
	var in struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		SetupToken string `json:"setupToken"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.setupToken != "" && !security.ConstantEqual(in.SetupToken, a.setupToken) {
		writeError(w, http.StatusForbidden, "invalid setup token")
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := a.store.CreateInitialOwner(r.Context(), in.Username, hash)
	if errors.Is(err, store.ErrSetupComplete) {
		writeError(w, http.StatusConflict, "setup is already complete")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "unable to create owner account")
		return
	}
	a.startSession(w, r, user)
}'''
a = replace_once(a, old_setup, new_setup, "atomic setup")

old_login = r'''func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid origin")
		return
	}
	var in struct{ Username, Password string }
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := a.store.UserByUsername(r.Context(), in.Username)
	if err != nil || !security.VerifyPassword(user.PasswordHash, in.Password) {
		time.Sleep(150 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.startSession(w, r, user)
}'''
new_login = r'''func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid origin")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := loginFailureKey(r, in.Username)
	count, err := a.store.LoginFailureCount(r.Context(), key, time.Now().Add(-15*time.Minute))
	if err != nil {
		a.internal(w, err)
		return
	}
	if count >= 12 {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	user, err := a.store.UserByUsername(r.Context(), in.Username)
	if err != nil || !security.VerifyPassword(user.PasswordHash, in.Password) {
		_ = a.store.RecordLoginFailure(r.Context(), key, time.Now())
		time.Sleep(150 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	_ = a.store.ClearLoginFailures(r.Context(), key)
	a.startSession(w, r, user)
}'''
a = replace_once(a, old_login, new_login, "login throttle")

marker = 'func (a *App) startSession(w http.ResponseWriter, r *http.Request, user store.User) {'
helper = r'''func loginFailureKey(r *http.Request, username string) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	return security.TokenHash(strings.ToLower(strings.TrimSpace(username)) + "|" + host)
}

'''
if "func loginFailureKey" not in a:
    if marker not in a:
        raise SystemExit("missing patch anchor: login key")
    a = a.replace(marker, helper + marker, 1)

restore_fn = r'''func (a *App) restoreData(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	var raw backup
	if err := decodeJSONCompat(r, &raw, 2<<20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if raw.Schema != 1 || raw.App != "RDP Web" {
		writeError(w, http.StatusBadRequest, "restore requires an RDP Web schema-1 backup")
		return
	}
	if err := a.store.RestoreReplace(r.Context(), s.UserID, store.RestorePayload{Groups: raw.Groups, Devices: raw.Devices}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groupsRestored": len(raw.Groups), "devicesRestored": len(raw.Devices)})
}

'''
marker = 'func (a *App) validOrigin(r *http.Request) bool {'
if "func (a *App) restoreData" not in a:
    if marker not in a:
        raise SystemExit("missing patch anchor: restore handler")
    a = a.replace(marker, restore_fn + marker, 1)

a = replace_once(
    a,
    '''\tw.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
}''',
    '''\tw.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
\tw.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
\tw.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
\tw.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
\tif a.secureCookie {
\t\tw.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
\t}
}''',
    "security headers",
)

old_decode = r'''func decodeJSONCompat(r *http.Request, dst any, limit int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid JSON: multiple values")
	}
	return nil
}

func decodeJSONLimit(r *http.Request, dst any, limit int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid JSON: multiple values")
	}
	return nil
}'''
new_decode = r'''func decodeJSONCompat(r *http.Request, dst any, limit int64) error {
	body, err := readBodyLimit(r, limit)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid JSON: multiple values")
	}
	return nil
}

func decodeJSONLimit(r *http.Request, dst any, limit int64) error {
	body, err := readBodyLimit(r, limit)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid JSON: multiple values")
	}
	return nil
}

func readBodyLimit(r *http.Request, limit int64) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("request body is too large")
	}
	return body, nil
}'''
a = replace_once(a, old_decode, new_decode, "body limit")

p.write_text(a)


# --- main: generate a first-run provisioning token ------------------------
p = Path("cmd/rdpweb/main.go")
m = p.read_text()
m = replace_once(
    m,
    '"github.com/Shermanxwz/RDP-web/internal/app"\n\t"github.com/Shermanxwz/RDP-web/internal/store"',
    '"github.com/Shermanxwz/RDP-web/internal/app"\n\t"github.com/Shermanxwz/RDP-web/internal/security"\n\t"github.com/Shermanxwz/RDP-web/internal/store"',
    "main security import",
)

anchor = '''\tdb, err := store.Open(filepath.Join(dataDir, "rdpweb.db"))
\tif err != nil {
\t\tlogger.Error("open database", "error", err)
\t\tos.Exit(1)
\t}
\tdefer db.Close()

\thandler, err := app.New(app.Config{'''
replacement = '''\tdb, err := store.Open(filepath.Join(dataDir, "rdpweb.db"))
\tif err != nil {
\t\tlogger.Error("open database", "error", err)
\t\tos.Exit(1)
\t}
\tdefer db.Close()

\tsetupToken := strings.TrimSpace(os.Getenv("RDPWEB_SETUP_TOKEN"))
\tcount, err := db.UserCount(context.Background())
\tif err != nil {
\t\tlogger.Error("read setup state", "error", err)
\t\tos.Exit(1)
\t}
\tif count == 0 && setupToken == "" {
\t\tsetupToken, err = security.RandomToken(18)
\t\tif err != nil {
\t\t\tlogger.Error("generate setup token", "error", err)
\t\t\tos.Exit(1)
\t\t}
\t\tlogger.Warn("first-run setup token generated; append it as ?setup=TOKEN when opening RDP Web", "setup_token", setupToken)
\t}

\thandler, err := app.New(app.Config{'''
m = replace_once(m, anchor, replacement, "setup token generation")
m = replace_once(
    m,
    '''\t\tSecureCookie: secureCookie,
\t\tSessionTTL:   ttl,
\t})''',
    '''\t\tSecureCookie: secureCookie,
\t\tSessionTTL:   ttl,
\t\tSetupToken:   setupToken,
\t})''',
    "pass setup token",
)
p.write_text(m)


# --- browser: carry setup token from URL and make restore explicit --------
p = Path("internal/app/static/app.js")
j = p.read_text()
j = replace_once(
    j,
    "const state={csrf:'',user:'',devices:[],groups:[],filter:'all',query:'',setup:false};",
    "const state={csrf:'',user:'',devices:[],groups:[],filter:'all',query:'',setup:false,setupToken:new URLSearchParams(location.search).get('setup')||''};if(state.setupToken)history.replaceState(null,'',location.pathname+location.hash);",
    "frontend setup token",
)
j = replace_once(
    j,
    "body:JSON.stringify({username:$('#auth-username').value,password:$('#auth-password').value})",
    "body:JSON.stringify({username:$('#auth-username').value,password:$('#auth-password').value,setupToken:state.setupToken})",
    "frontend setup payload",
)
old_import = "const result=await api('/api/import',{method:'POST',body:text});await loadAll();$('#import-dialog').close();render();toast(`已导入 ${result.devicesImported} 个连接，密码导入 0 个`)"
new_import = "const parsed=JSON.parse(text);const isBackup=parsed?.app==='RDP Web'&&parsed?.schema===1;const endpoint=isBackup?'/api/restore':'/api/import';if(isBackup&&!confirm('这会用备份完整替换当前分组和连接，继续吗？'))return;const result=await api(endpoint,{method:'POST',body:text});await loadAll();$('#import-dialog').close();render();toast(isBackup?`已恢复 ${result.devicesRestored} 个连接`:`已导入 ${result.devicesImported} 个连接，密码导入 0 个`)"
j = replace_once(j, old_import, new_import, "frontend restore")
p.write_text(j)


# --- compose: secure, writable persistence without UID footguns -----------
p = Path("docker-compose.yml")
p.write_text('''services:\n  rdp-web:\n    build: .\n    container_name: rdp-web\n    restart: unless-stopped\n    init: true\n    stop_grace_period: 15s\n    ports:\n      - "${RDPWEB_BIND_IP:-127.0.0.1}:8080:8080"\n    environment:\n      RDPWEB_ADDR: ":8080"\n      RDPWEB_DATA_DIR: "/data"\n      RDPWEB_PUBLIC_URL: "${RDPWEB_PUBLIC_URL:-}"\n      RDPWEB_SECURE_COOKIE: "${RDPWEB_SECURE_COOKIE:-false}"\n      RDPWEB_SETUP_TOKEN: "${RDPWEB_SETUP_TOKEN:-}"\n    volumes:\n      - rdpweb-data:/data\n    read_only: true\n    tmpfs:\n      - /tmp:size=16m,mode=1777\n    security_opt:\n      - no-new-privileges:true\n    cap_drop:\n      - ALL\n\nvolumes:\n  rdpweb-data:\n''')


# --- integration tests -----------------------------------------------------
Path("internal/app/app_integration_test.go").write_text(r'''package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shermanxwz/RDP-web/internal/store"
)

type testClient struct {
	base   string
	csrf   string
	cookie *http.Cookie
}

func newTestApp(t *testing.T, dbPath, setupToken string) (*httptest.Server, *store.Store) {
	t.Helper()
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Config{Store: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), SessionTTL: time.Hour, SetupToken: setupToken})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return httptest.NewServer(h), db
}

func (c *testClient) do(t *testing.T, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if c.csrf != "" && method != http.MethodGet {
		req.Header.Set("X-RDPWeb-CSRF", c.csrf)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, ck := range resp.Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck
		}
	}
	var obj map[string]any
	if json.Unmarshal(data, &obj) == nil {
		if token, ok := obj["csrfToken"].(string); ok {
			c.csrf = token
		}
	}
	return resp, data
}

func TestFullHTTPPersistenceRestoreAndLegacyImport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rdpweb.db")
	ts, db := newTestApp(t, dbPath, "setup-secret")
	c := &testClient{base: ts.URL}

	if r, _ := c.do(t, http.MethodGet, "/healthz", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("health=%d", r.StatusCode)
	}
	if r, _ := c.do(t, http.MethodPost, "/api/setup", `{"username":"owner","password":"correct horse battery staple","setupToken":"wrong"}`); r.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong setup token=%d", r.StatusCode)
	}
	if r, b := c.do(t, http.MethodPost, "/api/setup", `{"username":"owner","password":"correct horse battery staple","setupToken":"setup-secret"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("setup=%d %s", r.StatusCode, b)
	}
	if r, _ := c.do(t, http.MethodPost, "/api/groups", `{"id":"g1","name":"Prod","sortOrder":0}`); r.StatusCode != http.StatusCreated {
		t.Fatalf("group=%d", r.StatusCode)
	}
	dev := `{"id":"d1","groupId":"g1","name":"Server","host":"example.com","port":3389,"username":"admin","domain":"","gateway":"","favorite":true,"notes":"n","useMultimon":false,"redirectClipboard":true,"audioMode":0}`
	if r, b := c.do(t, http.MethodPost, "/api/devices", dev); r.StatusCode != http.StatusCreated {
		t.Fatalf("device=%d %s", r.StatusCode, b)
	}
	r, backup := c.do(t, http.MethodGet, "/api/export", "")
	if r.StatusCode != http.StatusOK || !bytes.Contains(backup, []byte(`"d1"`)) || bytes.Contains(bytes.ToLower(backup), []byte("password")) {
		t.Fatalf("bad backup=%d %s", r.StatusCode, backup)
	}
	if r, b := c.do(t, http.MethodGet, "/api/devices/d1/uri", ""); r.StatusCode != http.StatusOK || !bytes.Contains(b, []byte("rdp://")) {
		t.Fatalf("uri=%d %s", r.StatusCode, b)
	}
	if r, b := c.do(t, http.MethodGet, "/api/devices/d1/rdp", ""); r.StatusCode != http.StatusOK || len(b) < 4 || b[0] != 0xff || b[1] != 0xfe {
		t.Fatalf("rdp=%d", r.StatusCode)
	}

	ts.Close()
	db.Close()
	ts2, db2 := newTestApp(t, dbPath, "ignored-after-setup")
	defer ts2.Close()
	defer db2.Close()
	c2 := &testClient{base: ts2.URL}
	if r, b := c2.do(t, http.MethodPost, "/api/login", `{"username":"owner","password":"correct horse battery staple"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("relogin=%d %s", r.StatusCode, b)
	}
	if r, b := c2.do(t, http.MethodGet, "/api/devices", ""); r.StatusCode != http.StatusOK || !bytes.Contains(b, []byte(`"d1"`)) {
		t.Fatalf("persist=%d %s", r.StatusCode, b)
	}
	if r, _ := c2.do(t, http.MethodDelete, "/api/devices/d1", ""); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete=%d", r.StatusCode)
	}
	if r, b := c2.do(t, http.MethodPost, "/api/restore", string(backup)); r.StatusCode != http.StatusOK {
		t.Fatalf("restore=%d %s", r.StatusCode, b)
	}
	if r, b := c2.do(t, http.MethodGet, "/api/devices", ""); r.StatusCode != http.StatusOK || !bytes.Contains(b, []byte(`"d1"`)) {
		t.Fatalf("restored=%d %s", r.StatusCode, b)
	}

	badBackup := strings.Replace(string(backup), `"host":"example.com"`, `"host":"https://bad"`, 1)
	if r, _ := c2.do(t, http.MethodPost, "/api/restore", badBackup); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid restore=%d", r.StatusCode)
	}
	if r, b := c2.do(t, http.MethodGet, "/api/devices", ""); r.StatusCode != http.StatusOK || !bytes.Contains(b, []byte(`"d1"`)) {
		t.Fatalf("restore rollback failed=%d %s", r.StatusCode, b)
	}

	legacy := `{"schema":1,"app":"RdpSync","updatedAt":1,"devices":[{"id":42,"name":"Legacy","host":"legacy.example.com","port":3389,"username":"legacy","password":"TOP-SECRET","domain":"","rdpServerName":"old","width":1280,"height":720}]}`
	if r, b := c2.do(t, http.MethodPost, "/api/import", legacy); r.StatusCode != http.StatusOK || bytes.Contains(bytes.ToLower(b), []byte("top-secret")) {
		t.Fatalf("legacy import=%d %s", r.StatusCode, b)
	}
	if r, b := c2.do(t, http.MethodGet, "/api/export", ""); r.StatusCode != http.StatusOK || !bytes.Contains(b, []byte("legacy.example.com")) || bytes.Contains(b, []byte("TOP-SECRET")) {
		t.Fatalf("legacy secret leaked=%d %s", r.StatusCode, b)
	}
}

func TestSetupSingleOwnerUnderConcurrency(t *testing.T) {
	ts, db := newTestApp(t, filepath.Join(t.TempDir(), "db.sqlite"), "setup-secret")
	defer ts.Close()
	defer db.Close()

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, username := range []string{"ownerone", "ownertwo"} {
		username := username
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := `{"username":"` + username + `","password":"correct horse battery staple","setupToken":"setup-secret"}`
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/setup", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			statuses <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for status := range statuses {
		if status == http.StatusOK {
			ok++
		}
		if status == http.StatusConflict {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("expected one setup success and one conflict: ok=%d conflict=%d", ok, conflict)
	}
	count, err := db.UserCount(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("user count=%d err=%v", count, err)
	}
}

func TestLoginRateLimitAndBodyLimit(t *testing.T) {
	ts, db := newTestApp(t, filepath.Join(t.TempDir(), "db.sqlite"), "setup-secret")
	defer ts.Close()
	defer db.Close()
	c := &testClient{base: ts.URL}
	if r, _ := c.do(t, http.MethodPost, "/api/setup", `{"username":"owner","password":"correct horse battery staple","setupToken":"setup-secret"}`); r.StatusCode != http.StatusOK {
		t.Fatal(r.StatusCode)
	}
	c.cookie, c.csrf = nil, ""
	for i := 0; i < 12; i++ {
		r, _ := c.do(t, http.MethodPost, "/api/login", `{"username":"owner","password":"wrong password value"}`)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d=%d", i, r.StatusCode)
		}
	}
	if r, _ := c.do(t, http.MethodPost, "/api/login", `{"username":"owner","password":"wrong password value"}`); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 got %d", r.StatusCode)
	}

	tooBig := `{"username":"owner","password":"` + strings.Repeat("x", (1<<20)+32) + `"}`
	if r, _ := c.do(t, http.MethodPost, "/api/login", tooBig); r.StatusCode != http.StatusBadRequest && r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("oversized body=%d", r.StatusCode)
	}
}
''')


# --- browser E2E -----------------------------------------------------------
Path("package.json").write_text('''{\n  "name": "rdp-web-tests",\n  "private": true,\n  "devDependencies": {\n    "@playwright/test": "1.55.0"\n  }\n}\n''')
Path("tests").mkdir(exist_ok=True)
Path("tests/browser.spec.js").write_text(r'''const { test, expect } = require('@playwright/test');
const fs = require('fs');

test.use({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140 Safari/537.36' });

test('first-run UI, CRUD, PWA, export and Windows RDP download', async ({ page }) => {
  await page.goto('http://127.0.0.1:18081/?setup=browser-setup-token');
  await expect(page.locator('#auth')).toBeVisible();
  await page.locator('#auth-username').fill('browserowner');
  await page.locator('#auth-password').fill('correct horse battery staple');
  await page.locator('#auth-submit').click();
  await expect(page.locator('#app')).toBeVisible();

  const sw = await page.evaluate(async () => {
    if (!('serviceWorker' in navigator)) return false;
    await navigator.serviceWorker.ready;
    return !!(await navigator.serviceWorker.getRegistration());
  });
  expect(sw).toBeTruthy();

  await page.locator('#add-group').click();
  await page.locator('#group-name').fill('Production');
  await page.locator('#group-form button[type="submit"]').click();
  await expect(page.locator('#group-list')).toContainText('Production');

  await page.locator('#add-device').click();
  await page.locator('#device-name').fill('Browser Server');
  await page.locator('#device-host').fill('browser.example.com');
  await page.locator('#device-username').fill('Administrator');
  await page.locator('#device-group').selectOption({ label: 'Production' });
  await page.locator('#device-favorite').check();
  await page.locator('#device-form button[type="submit"]').click();
  await expect(page.locator('.device-card')).toContainText('Browser Server');

  await page.locator('#search').fill('browser.example.com');
  await expect(page.locator('.device-card')).toHaveCount(1);
  await page.locator('#search').fill('');

  const rdpDownload = page.waitForEvent('download');
  await page.locator('.connect').click();
  const rdp = await rdpDownload;
  const rdpPath = await rdp.path();
  const bytes = fs.readFileSync(rdpPath);
  expect(bytes[0]).toBe(0xff);
  expect(bytes[1]).toBe(0xfe);

  const exportDownload = page.waitForEvent('download');
  await page.locator('#export-btn').click();
  const exported = await exportDownload;
  const json = JSON.parse(fs.readFileSync(await exported.path(), 'utf8'));
  expect(json.app).toBe('RDP Web');
  expect(json.devices.some(d => d.host === 'browser.example.com')).toBeTruthy();

  await page.locator('#logout-btn').click();
  await expect(page.locator('#auth')).toBeVisible();
  await page.locator('#auth-username').fill('browserowner');
  await page.locator('#auth-password').fill('correct horse battery staple');
  await page.locator('#auth-submit').click();
  await expect(page.locator('.device-card')).toContainText('Browser Server');
});
''')


# --- real Docker lifecycle smoke ------------------------------------------
Path("scripts/container-smoke.sh").write_text(r'''#!/usr/bin/env bash
set -euo pipefail
IMAGE="${RDPWEB_IMAGE:-rdp-web:ci}"
VOLUME="rdpweb-ci-data-${GITHUB_RUN_ID:-local}-$$"
NAME="rdp-web-smoke-${GITHUB_RUN_ID:-local}-$$"
BASE="http://127.0.0.1:18080"
COOKIE="${RUNNER_TEMP:-/tmp}/rdpweb-cookie-$$"
cleanup() {
  docker rm -f "$NAME" "${NAME}-2" >/dev/null 2>&1 || true
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  rm -f "$COOKIE" "${COOKIE}-2"
}
trap cleanup EXIT

docker volume create "$VOLUME" >/dev/null
docker run -d --name "$NAME" -p 18080:8080 \
  --read-only --tmpfs /tmp:size=16m,mode=1777 \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  -e RDPWEB_SETUP_TOKEN=container-setup-token \
  -v "$VOLUME:/data" "$IMAGE" >/dev/null
for _ in $(seq 1 40); do
  curl -fsS "$BASE/healthz" >/dev/null && break
  sleep 1
done
curl -fsS "$BASE/healthz" >/dev/null
setup=$(curl -fsS -c "$COOKIE" -H 'Content-Type: application/json' \
  -d '{"username":"containerowner","password":"correct horse battery staple","setupToken":"container-setup-token"}' "$BASE/api/setup")
csrf=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["csrfToken"])' <<<"$setup")
curl -fsS -b "$COOKIE" -H "X-RDPWeb-CSRF: $csrf" -H 'Content-Type: application/json' \
  -d '{"id":"container-device","groupId":"","name":"Container Server","host":"container.example.com","port":3389,"username":"Administrator","domain":"","gateway":"","favorite":false,"notes":"","useMultimon":false,"redirectClipboard":true,"audioMode":0}' \
  "$BASE/api/devices" >/dev/null

docker stop "$NAME" >/dev/null
docker rm "$NAME" >/dev/null

docker run -d --name "${NAME}-2" -p 18080:8080 \
  --read-only --tmpfs /tmp:size=16m,mode=1777 \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  -v "$VOLUME:/data" "$IMAGE" >/dev/null
for _ in $(seq 1 40); do
  curl -fsS "$BASE/healthz" >/dev/null && break
  sleep 1
done
curl -fsS "$BASE/healthz" >/dev/null
login=$(curl -fsS -c "${COOKIE}-2" -H 'Content-Type: application/json' \
  -d '{"username":"containerowner","password":"correct horse battery staple"}' "$BASE/api/login")
python3 - <<'PY' "$BASE" "${COOKIE}-2"
import json, subprocess, sys
base, cookie = sys.argv[1:]
raw = subprocess.check_output(['curl','-fsS','-b',cookie,base+'/api/devices'])
devices = json.loads(raw)
assert any(d['host'] == 'container.example.com' for d in devices), devices
PY
''')


# --- permanent CI gates ----------------------------------------------------
p = Path(".github/workflows/ci.yml")
p.write_text('''name: CI\n\non:\n  push:\n    branches: [main]\n  pull_request:\n\npermissions:\n  contents: read\n\njobs:\n  go:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v5\n      - uses: actions/setup-go@v6\n        with:\n          go-version: '1.27.1'\n          cache: true\n      - name: Format\n        run: test -z "$(gofmt -l $(find . -name '*.go'))"\n      - name: Test\n        run: go test -race ./...\n      - name: Vet\n        run: go vet ./...\n      - name: Build\n        run: CGO_ENABLED=0 go build -trimpath ./cmd/rdpweb\n\n  docker:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v5\n      - uses: docker/setup-buildx-action@v3\n      - uses: docker/build-push-action@v6\n        with:\n          context: .\n          push: false\n          load: true\n          tags: rdp-web:ci\n      - name: Real container lifecycle and persistence\n        env:\n          RDPWEB_IMAGE: rdp-web:ci\n        run: bash scripts/container-smoke.sh\n\n  browser:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v5\n      - uses: actions/setup-go@v6\n        with:\n          go-version: '1.27.1'\n      - uses: actions/setup-node@v5\n        with:\n          node-version: '24'\n          cache: npm\n      - run: npm ci\n      - run: npx playwright install --with-deps chromium\n      - name: Start application\n        env:\n          RDPWEB_ADDR: ':18081'\n          RDPWEB_DATA_DIR: ${{ runner.temp }}/browser-data\n          RDPWEB_SETUP_TOKEN: browser-setup-token\n        run: |\n          go run ./cmd/rdpweb >"${{ runner.temp }}/rdpweb-browser.log" 2>&1 &\n          for i in $(seq 1 40); do\n            curl -fsS http://127.0.0.1:18081/healthz >/dev/null && break\n            sleep 1\n          done\n          curl -fsS http://127.0.0.1:18081/healthz >/dev/null\n      - run: npx playwright test tests/browser.spec.js --project=chromium\n''')

# playwright config keeps CI deterministic
Path("playwright.config.js").write_text(r'''const { defineConfig, devices } = require('@playwright/test');
module.exports = defineConfig({
  timeout: 30000,
  retries: 0,
  workers: 1,
  reporter: 'line',
  use: { trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
''')

# deployment docs and API docs
p = Path("docs/API.md")
d = p.read_text()
if "/api/restore" not in d:
    d += "\n## POST /api/restore\n\nAtomically replaces the authenticated owner's groups and devices from an RDP Web schema-1 backup. Validation happens before mutation; any failure rolls the transaction back.\n"
p.write_text(d)

p = Path("docs/DEPLOYMENT.md")
d = p.read_text()
if "first-run setup token" not in d.lower():
    d += '''\n## First-run provisioning\n\nWhen no owner exists and `RDPWEB_SETUP_TOKEN` is not set, the service generates a random first-run setup token and writes it to the container log. Open the site once with `?setup=TOKEN` to create the unique owner. After setup, the token is no longer used. You may instead set `RDPWEB_SETUP_TOKEN` explicitly.\n\nThe default Compose file binds HTTP to `127.0.0.1:8080`; put Caddy/Nginx in front for HTTPS. Set `RDPWEB_PUBLIC_URL=https://rdp.example.com` and `RDPWEB_SECURE_COOKIE=true`. The default persistent storage is the named volume `rdpweb-data`, avoiding host UID permission failures.\n'''
p.write_text(d)

print("seal hardening patch applied")
