package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Shermanxwz/RDP-web/internal/rdp"
	"github.com/Shermanxwz/RDP-web/internal/security"
	"github.com/Shermanxwz/RDP-web/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

const cookieName = "rdpweb_session"

type Config struct {
	Store        *store.Store
	Logger       *slog.Logger
	PublicURL    string
	SecureCookie bool
	SessionTTL   time.Duration
	SetupToken   string
}

type App struct {
	store        *store.Store
	logger       *slog.Logger
	publicOrigin string
	secureCookie bool
	sessionTTL   time.Duration
	setupToken   string
	static       fs.FS
}

type contextKey int

const sessionKey contextKey = 1

func New(cfg Config) (*App, error) {
	if cfg.Store == nil {
		return nil, errors.New("store is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	var origin string
	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, errors.New("invalid RDPWEB_PUBLIC_URL")
		}
		origin = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	return &App{store: cfg.Store, logger: cfg.Logger, publicOrigin: origin, secureCookie: cfg.SecureCookie, sessionTTL: cfg.SessionTTL, setupToken: cfg.SetupToken, static: assets}, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ww := &statusWriter{ResponseWriter: w, status: 200}
	a.securityHeaders(ww)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		ww.Header().Set("Cache-Control", "no-store")
	}
	a.route(ww, r)
	a.logger.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.status, "duration_ms", time.Since(start).Milliseconds())
}

func (a *App) route(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if err := a.store.Health(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.api(w, r)
		return
	}
	a.serveStatic(w, r)
}

func (a *App) api(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/bootstrap" {
		a.bootstrap(w, r)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/setup" {
		a.setup(w, r)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/login" {
		a.login(w, r)
		return
	}

	sess, ok := a.authenticate(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), sessionKey, sess))

	if isMutation(r.Method) {
		if !a.validOrigin(r) {
			writeError(w, http.StatusForbidden, "invalid origin")
			return
		}
		if r.Header.Get("X-RDPWeb-CSRF") != sess.CSRFToken {
			writeError(w, http.StatusForbidden, "invalid csrf token")
			return
		}
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/me":
		a.me(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/logout":
		a.logout(w, r)
	case r.URL.Path == "/api/groups":
		a.groups(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/groups/"):
		a.group(w, r)
	case r.URL.Path == "/api/devices":
		a.devices(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/devices/"):
		a.device(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/export":
		a.export(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/import":
		a.importData(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/restore":
		a.restoreData(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (a *App) bootstrap(w http.ResponseWriter, r *http.Request) {
	count, err := a.store.UserCount(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needsSetup": count == 0})
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
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
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
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
}

func loginFailureKey(r *http.Request, username string) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	return security.TokenHash(strings.ToLower(strings.TrimSpace(username)) + "|" + host)
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, user store.User) {
	token, err := security.RandomToken(32)
	if err != nil {
		a.internal(w, err)
		return
	}
	csrf, err := security.RandomToken(24)
	if err != nil {
		a.internal(w, err)
		return
	}
	expires := time.Now().Add(a.sessionTTL)
	if err := a.store.CreateSession(r.Context(), security.TokenHash(token), user.ID, csrf, expires); err != nil {
		a.internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int(a.sessionTTL.Seconds()), Expires: expires})
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "csrfToken": csrf})
}

func (a *App) authenticate(r *http.Request) (store.Session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return store.Session{}, false
	}
	sess, err := a.store.SessionByHash(r.Context(), security.TokenHash(cookie.Value))
	return sess, err == nil
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	writeJSON(w, http.StatusOK, map[string]any{"username": s.Username, "csrfToken": s.CSRFToken})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		_ = a.store.DeleteSession(r.Context(), security.TokenHash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) groups(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	switch r.Method {
	case http.MethodGet:
		groups, err := a.store.ListGroups(r.Context(), s.UserID)
		if err != nil {
			a.internal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, groups)
	case http.MethodPost:
		var g store.Group
		if err := decodeJSON(r, &g); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if g.ID == "" {
			g.ID, _ = security.RandomToken(12)
		}
		if err := a.store.CreateGroup(r.Context(), s.UserID, g); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, g)
	default:
		methodNotAllowed(w)
	}
}

func (a *App) group(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/groups/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, 404, "not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var g store.Group
		if err := decodeJSON(r, &g); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		g.ID = id
		if err := a.store.UpdateGroup(r.Context(), s.UserID, g); err != nil {
			a.storeError(w, err)
			return
		}
		writeJSON(w, 200, g)
	case http.MethodDelete:
		if err := a.store.DeleteGroup(r.Context(), s.UserID, id); err != nil {
			a.storeError(w, err)
			return
		}
		w.WriteHeader(204)
	default:
		methodNotAllowed(w)
	}
}

func (a *App) devices(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	switch r.Method {
	case http.MethodGet:
		devices, err := a.store.ListDevices(r.Context(), s.UserID)
		if err != nil {
			a.internal(w, err)
			return
		}
		writeJSON(w, 200, devices)
	case http.MethodPost:
		var d store.Device
		if err := decodeJSON(r, &d); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if d.ID == "" {
			d.ID, _ = security.RandomToken(12)
		}
		if d.Port == 0 {
			d.Port = 3389
		}
		if err := a.store.CreateDevice(r.Context(), s.UserID, d); err != nil {
			writeError(w, 400, friendlyDBError(err))
			return
		}
		created, _ := a.store.GetDevice(r.Context(), s.UserID, d.ID)
		writeJSON(w, 201, created)
	default:
		methodNotAllowed(w)
	}
}

func (a *App) device(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && r.Method == http.MethodGet {
		d, err := a.store.GetDevice(r.Context(), s.UserID, parts[0])
		if err != nil {
			a.storeError(w, err)
			return
		}
		switch parts[1] {
		case "uri":
			writeJSON(w, 200, map[string]string{"uri": rdp.URI(d)})
		case "rdp":
			w.Header().Set("Content-Type", "application/x-rdp")
			w.Header().Set("Content-Disposition", `attachment; filename="rdpweb.rdp"`)
			w.Header().Set("Cache-Control", "no-store")
			w.Write(rdp.File(d))
		default:
			writeError(w, 404, "not found")
		}
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeError(w, 404, "not found")
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodGet:
		d, err := a.store.GetDevice(r.Context(), s.UserID, id)
		if err != nil {
			a.storeError(w, err)
			return
		}
		writeJSON(w, 200, d)
	case http.MethodPut:
		var d store.Device
		if err := decodeJSON(r, &d); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		d.ID = id
		if err := a.store.UpdateDevice(r.Context(), s.UserID, d); err != nil {
			a.storeError(w, err)
			return
		}
		updated, _ := a.store.GetDevice(r.Context(), s.UserID, id)
		writeJSON(w, 200, updated)
	case http.MethodDelete:
		if err := a.store.DeleteDevice(r.Context(), s.UserID, id); err != nil {
			a.storeError(w, err)
			return
		}
		w.WriteHeader(204)
	default:
		methodNotAllowed(w)
	}
}

type backup struct {
	Schema     int            `json:"schema"`
	App        string         `json:"app"`
	ExportedAt string         `json:"exportedAt"`
	Groups     []store.Group  `json:"groups"`
	Devices    []store.Device `json:"devices"`
}

func (a *App) export(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	groups, err := a.store.ListGroups(r.Context(), s.UserID)
	if err != nil {
		a.internal(w, err)
		return
	}
	devices, err := a.store.ListDevices(r.Context(), s.UserID)
	if err != nil {
		a.internal(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="rdpweb-backup.json"`)
	writeJSON(w, 200, backup{Schema: 1, App: "RDP Web", ExportedAt: time.Now().UTC().Format(time.RFC3339), Groups: groups, Devices: devices})
}

func (a *App) importData(w http.ResponseWriter, r *http.Request) {
	s := mustSession(r)
	var raw struct {
		Schema  int           `json:"schema"`
		Groups  []store.Group `json:"groups"`
		Devices []struct {
			Name              string `json:"name"`
			Host              string `json:"host"`
			Port              int    `json:"port"`
			Username          string `json:"username"`
			Password          string `json:"password"`
			Domain            string `json:"domain"`
			GroupID           string `json:"groupId"`
			Gateway           string `json:"gateway"`
			Favorite          bool   `json:"favorite"`
			Notes             string `json:"notes"`
			UseMultimon       bool   `json:"useMultimon"`
			RedirectClipboard *bool  `json:"redirectClipboard"`
			AudioMode         int    `json:"audioMode"`
		} `json:"devices"`
	}
	if err := decodeJSONCompat(r, &raw, 2<<20); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if len(raw.Devices) > 5000 || len(raw.Groups) > 500 {
		writeError(w, 400, "import is too large")
		return
	}
	groupMap := map[string]string{}
	groupsImported := 0
	for _, g := range raw.Groups {
		old := g.ID
		if g.ID == "" {
			g.ID, _ = security.RandomToken(12)
		}
		if err := a.store.CreateGroup(r.Context(), s.UserID, g); err == nil {
			groupMap[old] = g.ID
			groupsImported++
		}
	}
	devicesImported := 0
	for _, item := range raw.Devices {
		id, _ := security.RandomToken(12)
		groupID := item.GroupID
		if mapped, ok := groupMap[groupID]; ok {
			groupID = mapped
		}
		clipboard := true
		if item.RedirectClipboard != nil {
			clipboard = *item.RedirectClipboard
		}
		d := store.Device{ID: id, GroupID: groupID, Name: item.Name, Host: item.Host, Port: item.Port, Username: item.Username, Domain: item.Domain, Gateway: item.Gateway, Favorite: item.Favorite, Notes: item.Notes, UseMultimon: item.UseMultimon, RedirectClipboard: clipboard, AudioMode: item.AudioMode}
		if d.Port == 0 {
			d.Port = 3389
		}
		if err := a.store.CreateDevice(r.Context(), s.UserID, d); err == nil {
			devicesImported++
		}
	}
	writeJSON(w, 200, map[string]any{"groupsImported": groupsImported, "devicesImported": devicesImported, "passwordsImported": 0})
}

func (a *App) restoreData(w http.ResponseWriter, r *http.Request) {
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

func (a *App) validOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	candidate := strings.ToLower(u.Scheme + "://" + u.Host)
	if a.publicOrigin != "" {
		return candidate == a.publicOrigin
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(a.static, name)
	if err != nil {
		if strings.Contains(name, ".") {
			writeError(w, 404, "not found")
			return
		}
		data, err = fs.ReadFile(a.static, "index.html")
		if err != nil {
			a.internal(w, err)
			return
		}
		name = "index.html"
	}
	if ext := path.Ext(name); ext != "" {
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}
	if name == "index.html" || name == "sw.js" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	w.Write(data)
}

func (a *App) securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
	if a.secureCookie {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func (a *App) internal(w http.ResponseWriter, err error) {
	a.logger.Error("request failed", "error", err)
	writeError(w, 500, "internal server error")
}
func (a *App) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, 404, "not found")
	} else {
		writeError(w, 400, friendlyDBError(err))
	}
}
func friendlyDBError(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return "a matching connection already exists"
	}
	return err.Error()
}
func mustSession(r *http.Request) store.Session { return r.Context().Value(sessionKey).(store.Session) }
func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}
func methodNotAllowed(w http.ResponseWriter) { writeError(w, 405, "method not allowed") }

func decodeJSON(r *http.Request, dst any) error { return decodeJSONLimit(r, dst, 1<<20) }
func decodeJSONCompat(r *http.Request, dst any, limit int64) error {
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
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
