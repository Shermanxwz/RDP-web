# HTTP API

The API is same-origin and cookie authenticated. It is primarily an internal contract for the bundled PWA.

All API responses set `Cache-Control: no-store`. Request bodies are size limited and normal product endpoints reject unknown JSON fields. The legacy migration importer is intentionally more tolerant so older RdpSync payloads can contain fields the web product does not use.

## Public

### `GET /healthz`

Database-backed readiness check. Returns HTTP 200 with `{"ok":true}` only when SQLite can execute a query; otherwise returns HTTP 503.

### `GET /api/bootstrap`

Returns whether the instance still needs its unique owner to be provisioned.

### `POST /api/setup`

Available only before the owner exists. The request includes:

```json
{
  "username": "owner",
  "password": "a strong owner password",
  "setupToken": "first-run provisioning token"
}
```

If the service was started without `RDPWEB_SETUP_TOKEN`, a random token is generated on a fresh database and written to the service log. The database enforces a single-owner invariant, including under concurrent setup requests.

Successful setup creates an authenticated session and returns the same session metadata shape as login.

### `POST /api/login`

```json
{
  "username": "owner",
  "password": "owner password"
}
```

Repeated failed attempts are persistently throttled. A throttled request returns HTTP 429 and a `Retry-After` header.

## Authenticated

- `GET /api/me`
- `POST /api/logout`
- `GET|POST /api/groups`
- `PUT|DELETE /api/groups/{id}`
- `GET|POST /api/devices`
- `GET|PUT|DELETE /api/devices/{id}`
- `GET /api/devices/{id}/uri`
- `GET /api/devices/{id}/rdp`
- `GET /api/export`
- `POST /api/import` — tolerant additive legacy RdpSync migration import
- `POST /api/restore` — atomic RDP Web backup replacement

State-changing authenticated requests must include the `X-RDPWeb-CSRF` value returned by setup/login or `GET /api/me`. Requests carrying an `Origin` header must match the configured public origin/current host.

## Session response

Successful setup/login and `GET /api/me` provide:

```json
{
  "username": "owner",
  "csrfToken": "opaque-per-session-value"
}
```

The actual session token is stored in an HttpOnly SameSite=Strict cookie and is never exposed to JavaScript.

## Device schema

```json
{
  "id": "uuid",
  "groupId": "",
  "name": "SG Amazon 01",
  "host": "10.0.0.10",
  "port": 3389,
  "username": "Administrator",
  "domain": "",
  "gateway": "",
  "favorite": true,
  "notes": "",
  "useMultimon": false,
  "redirectClipboard": true,
  "audioMode": 0
}
```

There is deliberately no password field in the supported device API.

## `GET /api/export`

Returns an RDP Web schema-1 portable profile backup containing groups and devices. It does not contain RDP passwords.

## `POST /api/import`

Migration-oriented additive importer. It accepts legacy RdpSync Android schema-1 payloads, including historical extra fields and numeric legacy IDs. New IDs are generated for imported devices and any legacy `password` value is deliberately discarded before persistence.

This endpoint is not the disaster-recovery replacement primitive.

## `POST /api/restore`

Accepts an RDP Web schema-1 backup and atomically replaces the authenticated owner's groups and devices. The complete payload is validated before mutation; replacement happens in one SQLite transaction and any error leaves the previous dataset intact.

## RDP handoff endpoints

`GET /api/devices/{id}/uri` returns the Microsoft-compatible `rdp://` URI used by supported Android/iOS/macOS clients. `GET /api/devices/{id}/rdp` downloads the UTF-16LE `.rdp` file intended for Windows Remote Desktop Connection.

Neither handoff contains an RDP password.
