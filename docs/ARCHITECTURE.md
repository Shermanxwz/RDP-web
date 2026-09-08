# Architecture

## Goal

RDP Web is a connection-profile control plane, not an RDP data plane. It solves profile portability while delegating the actual desktop session to Microsoft clients.

## Components

1. **Embedded PWA** — static HTML/CSS/JS served from the Go binary. There is no Node runtime and no CDN dependency in production.
2. **Go HTTP application** — first-run provisioning, authentication, JSON APIs, validation, backup/restore, launch-material generation and static serving.
3. **SQLite** — owner, opaque sessions, login-failure counters, groups and devices. WAL mode, foreign keys and a single writer connection are used for predictable self-hosted operation.
4. **Microsoft client handoff** — `rdp://` on Android/iOS/macOS; `.rdp` file on Windows.
5. **Verification layer** — Go integration/race tests, real-container lifecycle smoke and Chromium product E2E are permanent CI gates.

## Request/data flow

```text
Browser / installed PWA
        |
        | HTTPS + same-origin JSON
        v
   Go HTTP service
        |
        +---- auth/session/CSRF/throttle
        |
        +---- validation + groups/devices
        |
        +---- export / transactional restore
        |
        v
      SQLite

Connect action:
  Android/iOS/macOS ---> rdp:// URI ---> Microsoft client
  Windows -------------> .rdp file ---> mstsc.exe / supported Microsoft client
```

RDP traffic itself never passes through RDP Web.

## First-run invariant

A fresh instance requires a setup token to create its owner. The HTTP layer uses an atomic initial-owner operation and SQLite has a database-level unique invariant that prevents a second owner even if setup requests race.

After an owner exists, setup cannot create another account. RDP Web is intentionally a single-owner self-hosted product, not a multi-tenant account service.

## Authentication boundary

The browser never receives the owner's password hash. The session token is random and HttpOnly; only its SHA-256 hash is stored server-side. JavaScript receives a separate per-session CSRF token for state-changing requests.

Login failures are stored using a hash of normalized username plus client address and are used only for throttling. Successful login clears failures for that key.

## RDP credential boundary

The server never receives an RDP password through supported product APIs. Connection metadata such as host, port, username, domain and gateway is stored because it must be rendered to the authenticated owner.

Legacy RdpSync Android migration payloads may contain a historical `password` field. Compatibility parsing can observe the field, but it is deliberately excluded from the new `Device` model and therefore cannot be persisted or exported.

## Backup boundary

Two intentionally different paths exist:

- **Legacy import** is additive and tolerant. Its job is migration from historical RdpSync JSON.
- **RDP Web restore** is replacement-oriented and strict. The complete backup is validated first, then groups/devices are replaced inside one SQLite transaction. Any error rolls back the replacement.

For a full owner/session/database backup, the persistent SQLite volume is backed up while the service is stopped.

## Container boundary

The production container runs as a dedicated non-root user. The default Compose configuration uses a read-only root filesystem, drops all Linux capabilities, enables `no-new-privileges`, provides only a small `/tmp` tmpfs and writes persistent data exclusively to the named `rdpweb-data` volume.

The host HTTP bind defaults to loopback. Public traffic is expected to arrive through a TLS reverse proxy.

## Why no server-side RDP

Proxying RDP through the web service would reintroduce graphics codecs, input semantics, clipboard, NLA/CredSSP, certificate and transport responsibilities that this project specifically avoids. It would also turn the service into a high-value live desktop data-plane and credential target.

The product deliberately makes Microsoft responsible for the RDP session experience and keeps RDP Web focused on synchronized connection management.

## Why SQLite

The project is optimized for personal/self-hosted instances. SQLite avoids a second database service, makes disaster recovery straightforward and easily handles the expected profile-management workload.

The current store is intentionally single-writer (`SetMaxOpenConns(1)`), which also makes first-run and restore transaction semantics easier to reason about. A future multi-tenant hosted product would be a materially different architecture rather than an invisible extension of this sealed self-hosted design.

## Verification boundary

Linux CI can prove server behavior, browser behavior, generated `rdp://` strings and generated `.rdp` bytes. It cannot honestly prove that a proprietary Microsoft client is installed and registered as a protocol/file handler on a particular Windows, Android, iOS/iPadOS or macOS device.

That last OS/client boundary is kept as an explicit real-platform acceptance gate rather than being faked by a unit test.
