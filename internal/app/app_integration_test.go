package app

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
