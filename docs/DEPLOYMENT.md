# Deployment

## Docker Compose

```bash
git clone https://github.com/Shermanxwz/RDP-web.git
cd RDP-web
docker compose up -d --build
```

The default Compose configuration publishes only `127.0.0.1:8080`, uses the deterministic named volume `rdpweb-data`, runs the service as a non-root user, drops Linux capabilities and mounts the container root filesystem read-only.

### First-run provisioning

A fresh database has no owner. If `RDPWEB_SETUP_TOKEN` is not configured, RDP Web generates a cryptographically random setup token and writes it to the service log:

```bash
docker compose logs rdp-web | grep setup_token
```

Open the site once with that token:

```text
http://127.0.0.1:8080/?setup=TOKEN
```

Create the owner account. The database itself enforces a single-owner invariant, so concurrent setup requests cannot create a second owner.

For deterministic provisioning you can set your own strong token before the first boot:

```bash
RDPWEB_SETUP_TOKEN='a-long-random-secret'
docker compose up -d --build
```

The token is only relevant while no owner exists.

## Production HTTPS

Keep the application bound to loopback and put a TLS reverse proxy such as Caddy, nginx or Traefik in front of it.

Example `.env`:

```bash
RDPWEB_BIND_IP=127.0.0.1
RDPWEB_PUBLIC_URL=https://rdp.example.com
RDPWEB_SECURE_COOKIE=auto
RDPWEB_SESSION_TTL=720h
```

`auto` enables the Secure cookie flag whenever the configured public URL is HTTPS. State-changing requests must match the configured public Origin or they are rejected with HTTP 403.

### Caddy example

```caddy
rdp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

After TLS is active, open:

```text
https://rdp.example.com/?setup=TOKEN
```

for the initial owner creation if the instance is still fresh.

## Owner password rotation and recovery

For routine rotation use **修改密码** in the authenticated PWA. The current password is required. On success, RDP Web atomically updates the Argon2id hash, revokes all existing web sessions and returns a fresh session to the browser that performed the rotation.

If the owner password is forgotten, recovery requires control of the host/container data volume. Stop the running service first:

```bash
docker compose stop rdp-web
```

Read the new password without echoing it, pipe it to the one-shot recovery command, and remove the shell variable immediately afterwards:

```bash
read -rsp 'New RDP Web password: ' RDPWEB_NEW_PASSWORD; echo
printf '%s' "$RDPWEB_NEW_PASSWORD" | docker compose run --rm -T rdp-web reset-password --password-stdin
unset RDPWEB_NEW_PASSWORD
```

Then start the normal service again:

```bash
docker compose up -d
```

The reset command does not accept a password as a positional CLI argument, does not print it, and revokes every existing web session. It only changes the RDP Web owner password; it never touches Microsoft/RDP credentials because those credentials are not stored by this application.

## Health and readiness

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

A healthy response is:

```json
{"ok":true}
```

The readiness endpoint performs a SQLite query. If the process is alive but the database cannot be queried, it returns HTTP 503 instead of reporting a false healthy state.

## Portable profile backup

Use **Export** in the authenticated UI for a portable RDP Web JSON backup. It contains groups and connection metadata, never RDP passwords.

Importing an RDP Web schema-1 backup uses the atomic restore path: the complete payload is validated before replacement, all database writes occur in one transaction, and any failure rolls back the operation.

Legacy RdpSync Android JSON is handled separately as an additive migration import. Legacy `password` values are read only for format compatibility and deliberately discarded before persistence.

## Full database backup

A full database-level backup preserves the owner account and all persistent state. Stop the application first so SQLite WAL state is fully settled:

```bash
docker compose stop rdp-web
```

Archive the deterministic named volume:

```bash
docker run --rm \
  -v rdpweb-data:/data:ro \
  -v "$PWD:/backup" \
  alpine:3.24 \
  sh -c 'cd /data && tar czf /backup/rdpweb-data.tgz .'
```

Then restart:

```bash
docker compose start rdp-web
```

Do not copy only `rdpweb.db` while the application is actively writing unless your backup system is explicitly SQLite-WAL-aware.

## Full database restore

Stop the service:

```bash
docker compose stop rdp-web
```

Optionally archive the current volume before replacing it. Then restore:

```bash
docker run --rm \
  -v rdpweb-data:/data \
  -v "$PWD:/backup:ro" \
  alpine:3.24 \
  sh -c 'find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar xzf /backup/rdpweb-data.tgz -C /data'
```

Start the service and verify readiness:

```bash
docker compose start rdp-web
curl -fsS http://127.0.0.1:8080/healthz
```

## Updating

```bash
git pull
docker compose build --pull
docker compose up -d
```

Database migrations are forward-only and run automatically at startup. Take a full backup before significant version upgrades.

## Network exposure

Only the web endpoint needs to be reachable by users. RDP Web does not proxy TCP/3389. After handoff, the user's Microsoft RDP client connects directly to the target host or configured RD Gateway.

Do not expose the default HTTP listener directly to the public internet. Use HTTPS, configure `RDPWEB_PUBLIC_URL`, and leave `RDPWEB_BIND_IP=127.0.0.1` when the reverse proxy is on the same host.
