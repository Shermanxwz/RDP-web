# Seal v1 acceptance criteria

A release is **sealed** only when every automated gate below is green on the final `main` commit and the external Microsoft-client matrix is recorded separately.

## Automated gates

### Code and runtime

- Go formatting, Linux race tests, vet and production build.
- Native Windows runner: `go test`, `go vet`, native build.
- Native macOS runner: `go test`, `go vet`, native build.
- GitHub Actions/CI dependencies use current supported major lines and execute successfully on hosted runners.
- Docker Compose configuration validates successfully.
- Docker image builds from the current supported Alpine runtime line and the hardened container actually starts.
- Database-backed `/healthz` readiness succeeds.
- First-run owner creation is atomic under concurrent requests and the database admits only one owner.
- First-run provisioning token is required when configured.
- Login failure throttling is enforced.
- API request body limits reject oversized input.
- Cryptographic ID/token generation errors are never silently ignored.

### Owner account lifecycle

- Authenticated password rotation requires the current password.
- A successful password rotation atomically changes the password hash and revokes all pre-existing sessions.
- The browser performing the rotation receives a fresh session/CSRF pair and remains authenticated.
- The old password can no longer log in and the new password can.
- An incorrect current password does not rotate credentials or invalidate the active session.
- Local forgotten-password recovery is available only through host/data-volume access using `reset-password --password-stdin`.
- Local recovery never echoes the new password, revokes existing sessions, and is covered by CLI/store tests.

### Data lifecycle

- Full HTTP lifecycle: setup, authenticated CRUD, export, RDP URI/file generation, process restart, login, persistence, delete, transactional restore and semantic recovery.
- Invalid restore payloads leave the pre-existing dataset intact.
- Legacy RdpSync Android migration accepts historical fields/numeric IDs while discarding legacy RDP passwords.
- SQLite data survives complete container replacement when the named `/data` volume is reused.

### RDP payload safety

- `rdp://` and `.rdp` payloads never contain a password.
- `.rdp` output is UTF-16LE and preserves conservative redirection defaults.
- IPv6 address formatting is covered.
- RDP property injection attempts are rejected.

### Browser/product

- E2E dependency installation is reproducible from the lockfile.
- npm high-severity audit passes.
- A real Chromium instance completes first-run setup.
- PWA service worker registration reaches ready state.
- Group/device creation, search and re-login persistence work through the actual UI.
- Owner password rotation is exercised through the actual UI; old-password failure and new-password success are verified.
- Windows-mode connect downloads a valid UTF-16LE `.rdp` file.
- JSON export contains the created profile.

### Release

- Tagged releases re-run Go race/vet and npm high-severity audit before binaries are produced.
- Linux, Windows and macOS binaries are produced for amd64/arm64.
- Every published binary has a SHA-256 checksum and the release workflow verifies checksums before publication.
- Release artifact/upload/download actions are on current supported major lines and are exercised by the release workflow contract.

## External platform gates

CI cannot honestly prove that a proprietary OS client is installed and registered for a protocol/file type. The following therefore require real platform evidence:

- Windows: generated `.rdp` opens with the supported Microsoft client and reaches a test RDP host.
- Android: `rdp://` opens Microsoft Windows App and carries connection parameters.
- iOS/iPadOS: `rdp://` opens Microsoft Windows App and carries connection parameters.
- macOS: `rdp://` opens Microsoft Windows App and carries connection parameters.

These external gates are intentionally separated from automated server/web verification. The project must not claim they passed until they are actually exercised on the corresponding platform.
