# Deployment

## Docker Compose

```bash
git clone https://github.com/Shermanxwz/RDP-web.git
cd RDP-web
mkdir -p data
docker compose up -d --build
```

Browse to `http://SERVER:8080` and complete first-run setup.

## Production HTTPS

Put the service behind a TLS reverse proxy such as Caddy, nginx or Traefik. Then configure:

```yaml
environment:
  RDPWEB_PUBLIC_URL: "https://rdp.example.com"
  RDPWEB_SECURE_COOKIE: "true"
```

The public URL is also the accepted browser Origin for state-changing requests. A mismatch will produce HTTP 403 by design.

## Backup

Stop the container for a filesystem-level backup and copy the `data/rdpweb.db` file. Alternatively use the authenticated JSON export in the UI for portable profile backup.

Do not rely on copying only `rdpweb.db` while the service is writing unless your backup system understands SQLite WAL files.

## Restore

Stop the service, replace `/data/rdpweb.db` with the backed-up database, ensure the container user can read/write the file, then start the service.

## Updating

```bash
git pull
docker compose build --pull
docker compose up -d
```

Database migrations are forward-only and run automatically at startup.

## Reverse proxy example (Caddy)

```caddy
rdp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

## Network exposure

Only the web port must be reachable by users. RDP Web does not proxy TCP/3389. The user's Microsoft RDP client connects directly to the target host or configured RD Gateway after handoff.

## First-run provisioning

When no owner exists and `RDPWEB_SETUP_TOKEN` is not set, the service generates a random first-run setup token and writes it to the container log. Open the site once with `?setup=TOKEN` to create the unique owner. After setup, the token is no longer used. You may instead set `RDPWEB_SETUP_TOKEN` explicitly.

The default Compose file binds HTTP to `127.0.0.1:8080`; put Caddy/Nginx in front for HTTPS. Set `RDPWEB_PUBLIC_URL=https://rdp.example.com` and `RDPWEB_SECURE_COOKIE=true`. The default persistent storage is the named volume `rdpweb-data`, avoiding host UID permission failures.
