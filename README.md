# RDP Web

A self-hosted, cloud-synced connection list for Microsoft's Remote Desktop clients.

RDP Web does **not** implement RDP. It stores connection profiles and hands sessions to Microsoft's clients:

- Android / iOS / macOS: launches the documented `rdp://` URI scheme.
- Windows: downloads a generated `.rdp` file for Remote Desktop Connection (`mstsc.exe`).
- Other platforms: profile management and `.rdp` export remain available.

This split keeps the cloud-list problem in RDP Web and leaves rendering, input, codecs, NLA, clipboard and the RDP transport to Microsoft.

## Features

- Single-binary Go service with embedded responsive PWA.
- SQLite persistence with WAL mode and foreign-key enforcement.
- First-run owner setup; no public registration endpoint afterwards.
- Argon2id password hashing, server-side opaque sessions, HttpOnly/SameSite cookies and CSRF tokens.
- Device groups, favorites, search, notes and connection options.
- Microsoft-compatible `rdp://` generation for Android/iOS/macOS.
- UTF-16LE `.rdp` generation for Windows Remote Desktop Connection.
- **No RDP passwords are stored.** Credentials stay with the Microsoft client / OS credential store.
- Import of RdpSync Android schema-1 JSON; legacy `password` fields are deliberately discarded.
- Portable JSON backup/export.
- Docker/Compose deployment, health endpoint, CI, tests and release builds.

## Quick start

```bash
mkdir -p data
docker compose up -d --build
```

Open `http://localhost:8080`. On first launch, create the owner account.

For internet-facing deployments put RDP Web behind HTTPS and set:

```bash
RDPWEB_PUBLIC_URL=https://rdp.example.com
RDPWEB_SECURE_COOKIE=true
```

See [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

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
| `RDPWEB_SECURE_COOKIE` | auto from HTTPS public URL | Force Secure session cookie |
| `RDPWEB_SESSION_TTL` | `720h` | Login session lifetime |

## Security model

RDP Web stores metadata such as host, port and username. It intentionally does not accept or persist RDP passwords. Imported RdpSync Android JSON may contain a `password` property; it is ignored during import and never returned by exports.

The generated `.rdp` files are unsigned. Current Windows versions can show security warnings for downloaded RDP files. That is expected behavior and should not be bypassed. Organizations that distribute RDP files at scale should use Microsoft's documented signing and policy controls.

Read [`SECURITY.md`](SECURITY.md) before exposing an instance to the internet.

## Development

```bash
make check
make run
```

CI verifies formatting, tests, vet, build and Docker build.

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
