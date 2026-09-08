# Security

## Supported versions

Security fixes are applied to the latest release on `main`.

## What RDP Web stores

RDP Web stores connection metadata: display name, host, port, RDP username/domain, optional RD Gateway, groups, notes and connection preferences. It stores the RDP Web owner password only as an Argon2id hash.

**RDP passwords are not accepted or stored.** When importing legacy RdpSync Android schema-1 JSON, a `password` property may exist. The importer decodes it for compatibility but deliberately discards it before persistence.

## Browser/session protections

- Opaque random session tokens are stored only as SHA-256 hashes server-side.
- Session cookies are HttpOnly and SameSite=Strict.
- State-changing authenticated requests require a per-session CSRF token.
- Requests with an `Origin` header are restricted to the configured public origin / current host.
- CSP, anti-framing, no-sniff and restrictive Permissions-Policy headers are set.
- Device fields reject CR/LF/NUL to prevent `.rdp` property injection.
- API request bodies are size limited.

For public deployments, HTTPS is required in practice. Configure `RDPWEB_PUBLIC_URL=https://...` and `RDPWEB_SECURE_COOKIE=true`.

## Generated RDP files

Downloaded `.rdp` files are intentionally conservative: drive, printer, COM-port and smart-card redirection are disabled unless a future explicit feature safely enables them. The files are unsigned, so modern Windows versions can display security warnings. Do not weaken Windows security policy just to suppress those warnings.

## Reporting vulnerabilities

Do not open a public issue containing credentials, public IPs, tokens or exploit details. Use GitHub's private vulnerability reporting feature when enabled, or contact the repository owner privately.
