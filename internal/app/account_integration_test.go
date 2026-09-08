package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordChangeRevokesOtherSessionsAndRotatesCurrentSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rdpweb.db")
	ts, db := newTestApp(t, dbPath)
	defer ts.Close()
	defer db.Close()

	oldPassword := "correct horse battery staple"
	newPassword := "new correct horse battery staple"
	primary := &client{base: ts.URL}
	if r, body := primary.do(t, "POST", "/api/setup", `{"username":"owner","password":"`+oldPassword+`"}`); r.StatusCode != 200 {
		t.Fatalf("setup %d %s", r.StatusCode, body)
	}
	oldCookieValue := primary.cookie.Value

	secondary := &client{base: ts.URL}
	if r, body := secondary.do(t, "POST", "/api/login", `{"username":"owner","password":"`+oldPassword+`"}`); r.StatusCode != 200 {
		t.Fatalf("second login %d %s", r.StatusCode, body)
	}

	body := `{"currentPassword":"` + oldPassword + `","newPassword":"` + newPassword + `"}`
	if r, response := primary.do(t, "POST", "/api/account/password", body); r.StatusCode != 200 {
		t.Fatalf("password change %d %s", r.StatusCode, response)
	}
	if primary.cookie == nil || primary.cookie.Value == "" || primary.cookie.Value == oldCookieValue {
		t.Fatal("password rotation did not issue a fresh session cookie")
	}
	if primary.csrf == "" {
		t.Fatal("password rotation did not issue a fresh CSRF token")
	}
	if r, response := primary.do(t, "GET", "/api/me", ""); r.StatusCode != 200 {
		t.Fatalf("rotated current session is not valid: %d %s", r.StatusCode, response)
	}
	if r, _ := secondary.do(t, "GET", "/api/me", ""); r.StatusCode != 401 {
		t.Fatalf("secondary session survived password rotation: %d", r.StatusCode)
	}

	oldLogin := &client{base: ts.URL}
	if r, _ := oldLogin.do(t, "POST", "/api/login", `{"username":"owner","password":"`+oldPassword+`"}`); r.StatusCode != 401 {
		t.Fatalf("old password still logs in: %d", r.StatusCode)
	}
	newLogin := &client{base: ts.URL}
	if r, response := newLogin.do(t, "POST", "/api/login", `{"username":"owner","password":"`+newPassword+`"}`); r.StatusCode != 200 {
		t.Fatalf("new password login failed: %d %s", r.StatusCode, response)
	}
}

func TestPasswordChangeRejectsWrongCurrentPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rdpweb.db")
	ts, db := newTestApp(t, dbPath)
	defer ts.Close()
	defer db.Close()

	c := &client{base: ts.URL}
	if r, body := c.do(t, "POST", "/api/setup", `{"username":"owner","password":"correct horse battery staple"}`); r.StatusCode != 200 {
		t.Fatalf("setup %d %s", r.StatusCode, body)
	}
	if r, body := c.do(t, "POST", "/api/account/password", `{"currentPassword":"definitely wrong password","newPassword":"new correct horse battery staple"}`); r.StatusCode != 403 || !strings.Contains(string(body), "current password") {
		t.Fatalf("unexpected wrong-current response %d %s", r.StatusCode, body)
	}
	if r, _ := c.do(t, "GET", "/api/me", ""); r.StatusCode != 200 {
		t.Fatalf("wrong current password invalidated the session: %d", r.StatusCode)
	}
}
