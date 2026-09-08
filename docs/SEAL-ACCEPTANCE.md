# Seal v1 acceptance criteria

A release is **sealed** only when every automated gate below is green on the final `main` commit and the external Microsoft-client matrix is recorded separately.

## Automated gates

- Go formatting, race tests, vet, and production build.
- Docker image build and actual container startup.
- Database-backed `/healthz` readiness.
- First-run owner creation is atomic under concurrent requests.
- Login failure throttling is enforced.
- Full HTTP lifecycle: setup, authenticated CRUD, export, RDP URI/file generation, process restart, login, persistence, delete, transactional restore, semantic recovery.
- SQLite data survives container replacement when `/data` is persisted.
- RDP URI and `.rdp` payloads never contain a password and preserve safe defaults.
- RDP property injection attempts are rejected.

## External platform gates

CI cannot honestly prove that a proprietary OS client is installed and registered for a protocol. The following therefore require real platform evidence:

- Windows: generated `.rdp` opens with the supported Microsoft client and reaches a test RDP host.
- Android: `rdp://` opens Microsoft Windows App and carries connection parameters.
- iOS/iPadOS: `rdp://` opens Microsoft Windows App and carries connection parameters.
- macOS: `rdp://` opens Microsoft Windows App and carries connection parameters.

The project must not claim those external gates passed until they are actually exercised on the corresponding platform.