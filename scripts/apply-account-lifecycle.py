from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if new in text:
        return text
    if old not in text:
        raise SystemExit(f"missing patch anchor: {label}")
    return text.replace(old, new, 1)


app = Path("internal/app/app.go")
s = app.read_text()

s = replace_once(
    s,
    '''\tcase r.Method == http.MethodGet && r.URL.Path == "/api/me":\n\t\ta.me(w, r)\n\tcase r.Method == http.MethodPost && r.URL.Path == "/api/logout":''',
    '''\tcase r.Method == http.MethodGet && r.URL.Path == "/api/me":\n\t\ta.me(w, r)\n\tcase r.Method == http.MethodPost && r.URL.Path == "/api/account/password":\n\t\ta.changePassword(w, r)\n\tcase r.Method == http.MethodPost && r.URL.Path == "/api/logout":''',
    "account password route",
)

s = replace_once(
    s,
    '''func (a *App) me(w http.ResponseWriter, r *http.Request) {\n\ts := mustSession(r)\n\twriteJSON(w, http.StatusOK, map[string]any{"username": s.Username, "csrfToken": s.CSRFToken})\n}\n\nfunc (a *App) logout(w http.ResponseWriter, r *http.Request) {''',
    '''func (a *App) me(w http.ResponseWriter, r *http.Request) {\n\ts := mustSession(r)\n\twriteJSON(w, http.StatusOK, map[string]any{"username": s.Username, "csrfToken": s.CSRFToken})\n}\n\nfunc (a *App) changePassword(w http.ResponseWriter, r *http.Request) {\n\ts := mustSession(r)\n\tvar in struct {\n\t\tCurrentPassword string `json:"currentPassword"`\n\t\tNewPassword     string `json:"newPassword"`\n\t}\n\tif err := decodeJSON(r, &in); err != nil {\n\t\twriteError(w, http.StatusBadRequest, err.Error())\n\t\treturn\n\t}\n\tuser, err := a.store.UserByUsername(r.Context(), s.Username)\n\tif err != nil {\n\t\ta.internal(w, err)\n\t\treturn\n\t}\n\tif !security.VerifyPassword(user.PasswordHash, in.CurrentPassword) {\n\t\ttime.Sleep(150 * time.Millisecond)\n\t\twriteError(w, http.StatusForbidden, "current password is incorrect")\n\t\treturn\n\t}\n\tif security.VerifyPassword(user.PasswordHash, in.NewPassword) {\n\t\twriteError(w, http.StatusBadRequest, "new password must be different from the current password")\n\t\treturn\n\t}\n\thash, err := security.HashPassword(in.NewPassword)\n\tif err != nil {\n\t\twriteError(w, http.StatusBadRequest, err.Error())\n\t\treturn\n\t}\n\tif err := a.store.ReplaceOwnerPassword(r.Context(), user.ID, hash); err != nil {\n\t\ta.internal(w, err)\n\t\treturn\n\t}\n\tuser.PasswordHash = hash\n\ta.startSession(w, r, user)\n}\n\nfunc (a *App) logout(w http.ResponseWriter, r *http.Request) {''',
    "password change handler",
)

# Never ignore CSPRNG failures while minting identifiers.
s = s.replace(
    '''\t\tif g.ID == "" {\n\t\t\tg.ID, _ = security.RandomToken(12)\n\t\t}''',
    '''\t\tif g.ID == "" {\n\t\t\tid, err := security.RandomToken(12)\n\t\t\tif err != nil {\n\t\t\t\ta.internal(w, err)\n\t\t\t\treturn\n\t\t\t}\n\t\t\tg.ID = id\n\t\t}''',
)
s = s.replace(
    '''\t\tif d.ID == "" {\n\t\t\td.ID, _ = security.RandomToken(12)\n\t\t}''',
    '''\t\tif d.ID == "" {\n\t\t\tid, err := security.RandomToken(12)\n\t\t\tif err != nil {\n\t\t\t\ta.internal(w, err)\n\t\t\t\treturn\n\t\t\t}\n\t\t\td.ID = id\n\t\t}''',
)
s = s.replace(
    '''\t\tid, _ := security.RandomToken(12)\n\t\tgroupID := item.GroupID''',
    '''\t\tid, err := security.RandomToken(12)\n\t\tif err != nil {\n\t\t\ta.internal(w, err)\n\t\t\treturn\n\t\t}\n\t\tgroupID := item.GroupID''',
)
app.write_text(s)

js = Path("internal/app/static/app.js")
j = js.read_text()
j = replace_once(
    j,
    "if(res.status===401){showAuth(false);throw new Error('登录已失效');}",
    "if(res.status===401&&path!=='/api/login'){showAuth(false);throw new Error('登录已失效');}",
    "login 401 handling",
)
j = replace_once(
    j,
    "function uuid(){return crypto.randomUUID?crypto.randomUUID():`${Date.now()}-${Math.random().toString(16).slice(2)}`}",
    "function uuid(){if(crypto.randomUUID)return crypto.randomUUID();const b=new Uint8Array(16);crypto.getRandomValues(b);b[6]=(b[6]&15)|64;b[8]=(b[8]&63)|128;const h=[...b].map(x=>x.toString(16).padStart(2,'0')).join('');return`${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`}",
    "cryptographic UUID fallback",
)
js.write_text(j)

print("account lifecycle patch applied")
