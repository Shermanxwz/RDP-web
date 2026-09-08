# RDP Web

A self-hosted, cloud-synced connection list for Microsoft's Remote Desktop clients.

RDP Web does **not** implement RDP. It stores connection profiles and hands sessions to Microsoft's clients:

- Android / iOS / macOS: launches the documented `rdp://` URI scheme.
- Windows: downloads a generated `.rdp` file for Remote Desktop Connection (`mstsc.exe`).
- Other platforms: profile management and `.rdp` export remain available.

This split keeps the cloud-list problem in RDP Web and leaves rendering, input, codecs, NLA, clipboard and the RDP transport to Microsoft.

## Features

- Single-binary Go service with an embedded responsive PWA.
- SQLite persistence with WAL mode, foreign keys and a database-level single-owner invariant.
- Protected first-run provisioning token; public registration does not exist after setup.
- Argon2id password hashing, server-side opaque sessions, HttpOnly/SameSite cookies and CSRF tokens.
- Persistent login-failure throttling and database-backed readiness checks.
- Device groups, favorites, search, notes, RD Gateway and connection options.
- Microsoft-compatible `rdp://` generation for Android/iOS/macOS.
- UTF-16LE `.rdp` generation for Windows Remote Desktop Connection.
- **No RDP passwords are stored.** Credentials stay with the Microsoft client / OS credential store.
- Import of RdpSync Android schema-1 JSON; legacy `password` fields are deliberately discarded.
- Portable JSON export plus atomic all-or-nothing RDP Web backup restore.
- Hardened Docker/Compose deployment: non-root process, read-only root filesystem, dropped capabilities and persistent named volume.
- CI gates for race tests, vet/build, dependency audit, real container restart persistence and Chromium product E2E.

## Quick start

```bash
git clone https://github.com/Shermanxwz/RDP-web.git
cd RDP-web
docker compose up -d --build
```

The default Compose deployment binds only to `127.0.0.1:8080`.

On a fresh database, retrieve the generated first-run setup token:

```bash
docker compose logs rdp-web | grep setup_token
```

Then open:

```text
http://127.0.0.1:8080/?setup=TOKEN
```

The token is required only for the first owner creation. You can provide your own deterministic token through `RDPWEB_SETUP_TOKEN` instead.

For an internet-facing deployment, put RDP Web behind HTTPS and configure:

```bash
RDPWEB_PUBLIC_URL=https://rdp.example.com
RDPWEB_SECURE_COOKIE=auto
```

With `auto`, Secure cookies are enabled automatically when `RDPWEB_PUBLIC_URL` uses HTTPS. See [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

## Run without Docker

Requires Go 1.27+.

```bash
go run ./cmd/rdpweb
```

Environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `RDPWEB_ADDR` | `:8080` | Listen address |
| `RDPWEB_DATA_DIR` | `./data` | SQLite/data directory |
| `RDPWEB_PUBLIC_URL` | empty | Canonical public origin used for origin checks |
| `RDPWEB_SECURE_COOKIE` | `auto` behavior when unset | `auto`, `true` or `false`; auto follows HTTPS public URL |
| `RDPWEB_SESSION_TTL` | `720h` | Login session lifetime; minimum accepted value is 1 hour |
| `RDPWEB_SETUP_TOKEN` | generated on first boot | Optional explicit first-run owner provisioning token |
| `RDPWEB_BIND_IP` | `127.0.0.1` in Compose | Host address used by Docker Compose port publishing |

## Backup and restore

The Docker deployment uses the deterministic named volume `rdpweb-data`.

For portable profile backup, use the authenticated JSON export in the UI. Importing an RDP Web schema-1 backup performs an atomic replacement: validation completes first and any database failure rolls the transaction back. Legacy RdpSync imports remain additive and deliberately discard RDP passwords.

For a complete filesystem-level backup including the owner account and sessions, stop the service and archive the volume. Exact commands are in [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

## Security model

RDP Web stores metadata such as host, port and username. It intentionally does not accept or persist RDP passwords. Imported RdpSync Android JSON may contain a `password` property; it is ignored during import and never returned by exports.

The generated `.rdp` files are unsigned. Current Windows versions can show security warnings for downloaded RDP files. That is expected behavior and should not be bypassed. Organizations that distribute RDP files at scale should use Microsoft's documented signing and policy controls.

Read [`SECURITY.md`](SECURITY.md) before exposing an instance to the internet.

## Verification and sealed-release boundary

Every PR must pass:

- `gofmt`, `go test -race ./...`, `go vet ./...` and a production build.
- npm high-severity dependency audit for the browser E2E toolchain.
- A real Docker lifecycle test that creates data, destroys the container, starts a second container on the same named volume, logs in again and verifies persistence.
- A real Chromium product E2E covering first-run setup, PWA service worker, groups/devices, search, `.rdp` download, JSON export and re-login persistence.

See [`docs/SEAL-ACCEPTANCE.md`](docs/SEAL-ACCEPTANCE.md) for the exact acceptance contract.

One boundary cannot be truthfully simulated by Linux CI: whether Microsoft's proprietary client is installed and registered on a specific Windows, Android, iOS/iPadOS or macOS device. Real-device evidence for that OS/client handoff is tracked separately; the project does not claim those external gates passed until they are exercised on the corresponding platform.

## Development

```bash
make check
make run
```

## Architecture

```text
Browser / PWA
     |
     | HTTPS + JSON API
     v
RDP Web (Go)
     |
     +-- SQLite (profiles, groups, auth sessions)
     |
     +-- rdp:// URI ------> Microsoft client on Android/iOS/macOS
     |
     +-- .rdp download ---> mstsc.exe on Windows
```

More detail: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## License

MIT
