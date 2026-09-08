# HTTP API

The API is same-origin and cookie authenticated. It is primarily an internal contract for the bundled PWA.

## Public

- `GET /healthz`
- `GET /api/bootstrap`
- `POST /api/setup` — available only before the first user exists
- `POST /api/login`

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
- `POST /api/import`

State-changing authenticated requests must include the `X-RDPWeb-CSRF` value returned by login/setup or `GET /api/me`.

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

## POST /api/restore

Atomically replaces the authenticated owner's groups and devices from an RDP Web schema-1 backup. Validation happens before mutation; any failure rolls the transaction back.
