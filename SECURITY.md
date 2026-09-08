# Security

## Supported versions

Security fixes are applied to the latest release on `main`.

## What RDP Web stores

RDP Web stores connection metadata: display name, host, port, RDP username/domain, optional RD Gateway, groups, notes and connection preferences. It stores the RDP Web owner password only as an Argon2id hash.

**RDP passwords are not accepted or stored.** When importing legacy RdpSync Android schema-1 JSON, a `password` property may exist. The importer decodes it for compatibility but deliberately discards it before persistence. RDP Web exports and generated handoff payloads never contain an RDP password.

## Owner provisioning

- There is exactly one RDP Web owner. SQLite enforces that invariant at the database layer, not only in HTTP application logic.
- First-run setup requires a provisioning token.
- If `RDPWEB_SETUP_TOKEN` is omitted on a fresh database, a cryptographically random token is generated and written to the service log for first-run use.
- Concurrent first-run requests are tested; only one owner can be committed.
- There is no public registration path after setup.

Treat the first-run token like a temporary secret and avoid copying it into issue reports, screenshots or public logs.

## Browser/session protections

- Owner passwords use Argon2id with per-password random salt.
- Opaque random session tokens are stored only as SHA-256 hashes server-side.
- Session cookies are HttpOnly and SameSite=Strict.
- Secure cookies automatically follow an HTTPS `RDPWEB_PUBLIC_URL` unless explicitly overridden.
- State-changing authenticated requests require a per-session CSRF token.
- Requests with an `Origin` header are restricted to the configured public origin / current host.
- Login failures are persistently counted by a hashed username/client-address key and temporarily throttled after repeated failures.
- API request bodies are strictly size limited, including over-limit detection rather than silent truncation.
- CSP, anti-framing, no-sniff, COOP/CORP, restrictive Permissions-Policy and cross-domain policy headers are set.
- HSTS is emitted when secure-cookie/HTTPS mode is active.
- Device fields reject CR/LF/NUL and invalid host forms to prevent `.rdp` property injection.

For public deployments, HTTPS is required in practice. Configure `RDPWEB_PUBLIC_URL=https://...`, leave `RDPWEB_SECURE_COOKIE=auto`, and keep the application listener behind the reverse proxy.

## Data integrity and restore

- SQLite runs with foreign keys, busy timeout and WAL mode.
- `/healthz` performs an actual database query and returns HTTP 503 when the database is unavailable.
- RDP Web backup restore validates the complete payload before mutation and replaces groups/devices inside one transaction.
- Invalid restore data leaves the prior dataset intact.
- Full filesystem backups should be taken with the service stopped unless the backup system is explicitly SQLite-WAL-aware.

## Container hardening

The default Compose deployment:

- runs the application as a non-root user;
- uses a read-only root filesystem;
- drops all Linux capabilities;
- sets `no-new-privileges`;
- provides only a small tmpfs for `/tmp`;
- stores writable state only in the named `rdpweb-data` volume;
- binds the HTTP port to loopback by default.

CI launches the real hardened container, creates persistent data, destroys it, starts a second container on the same volume and verifies that authentication and connection data survive.

## Generated RDP files

Downloaded `.rdp` files are intentionally conservative: drive, printer, COM-port and smart-card redirection are disabled. The files never contain a password. They are unsigned, so modern Windows versions can display security warnings. Do not weaken Windows security policy merely to suppress those warnings.

The mobile/macOS `rdp://` handoff likewise never transports a password.

## Verification gates

Every pull request must pass:

- Go race tests, vet and production build;
- RDP payload/security regression tests;
- HTTP integration tests covering setup concurrency, login throttling, restart persistence, atomic restore and legacy-secret stripping;
- npm high-severity audit for the browser E2E dependency set;
- a real Docker lifecycle/persistence smoke test;
- a real Chromium product E2E.

The final Microsoft proprietary-client launch on Windows, Android, iOS/iPadOS and macOS is an external platform boundary. It must be verified on the corresponding real platform; Linux CI must not be presented as evidence that an OS protocol handler is installed.

## Reporting vulnerabilities

Do not open a public issue containing credentials, public IPs, setup/session tokens or exploit details. Use GitHub's private vulnerability reporting feature when enabled, or contact the repository owner privately.
