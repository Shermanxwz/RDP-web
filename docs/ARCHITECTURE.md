# Architecture

## Goal

RDP Web is a connection-profile control plane, not an RDP data plane. It solves profile portability while delegating the session itself to Microsoft clients.

## Components

1. **Embedded PWA** — static HTML/CSS/JS served from the Go binary. No Node runtime and no CDN dependency.
2. **Go HTTP application** — authentication, JSON APIs, validation, launch material generation and static serving.
3. **SQLite** — users, opaque sessions, groups and devices. WAL mode and a single writer connection are used for predictable small-instance operation.
4. **Microsoft client handoff** — `rdp://` on Android/iOS/macOS; `.rdp` file on Windows.

## Trust boundaries

The browser never receives the owner's password hash or session token (the latter is HttpOnly). The server never receives an RDP password through supported APIs. Connection metadata is not treated as secret because it must be rendered to the authenticated browser, but instances should still be protected by HTTPS and strong owner credentials.

## Why no server-side RDP

Proxying RDP through the web service would reintroduce codec, input, clipboard, NLA, certificate and graphics responsibilities that this project is specifically designed to avoid. It would also turn the service into a high-value credential/data-plane target.

## Why SQLite

The project is intentionally optimized for personal/self-hosted instances. SQLite keeps backup and recovery to one data file, avoids a second service, and easily handles the expected profile-management workload. The store package is isolated enough to replace later if a multi-tenant hosted service requires PostgreSQL.
