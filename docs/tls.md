---
sidebar_position: 1
sidebar_label: "HTTPS / TLS"
---

# HTTPS / TLS

HTTPS is **always enabled** (default port 8443), so the minimal way to serve it is to just start
the server:

```bash
# CLI: https://localhost:8443
static-httpserver --root-dir ./html

# Docker: publish the HTTPS port and keep the certificate in a volume
docker run -p 8443:8443 \
    -v $(pwd)/html:/static:ro \
    -v $(pwd)/certs:/certs \
    byjg/static-httpserver
```

HTTP is **optional** — it only starts when `--port` or `PORT` is set. The Docker image sets
`PORT=8080`, so it serves both by default. `--only-https` (or `ONLY_HTTPS=true`) turns the HTTP
listener off even when a port is set, which is the way to get an HTTPS-only container:

```bash
static-httpserver --root-dir ./html --port 8080          # HTTP + HTTPS

docker run -p 8443:8443 -e ONLY_HTTPS=true byjg/static-httpserver   # HTTPS only
```

Both listeners use `--bind` (default `0.0.0.0`, all interfaces). Use it to restrict the server to a
single address, e.g. `--bind 127.0.0.1` for local-only access.

## Self-signed certificate (default)

When `--tls-cert-dir` has no `cert.pem`/`key.pem`, the server generates a self-signed certificate
valid for one year and saves it as `selfsigned-cert.pem` / `selfsigned-key.pem` **inside that same
directory** — `/certs` as root, `~/.static-httpserver/certs` otherwise, created with `0700`. On the
next start the saved certificate is reused, and it is only regenerated when it is expired (or within
30 days of expiring) or when it does not cover all the requested hostnames.

The Docker image runs as a non-root user (uid 1000) but pins `TLS_CERT_DIR=/certs` and ships that
directory owned by it, so a named volume on `/certs` inherits the ownership and keeps the same
certificate across container restarts. A **bind** mount takes its ownership from the host instead,
so the host directory has to be writable by uid 1000. When the directory cannot be written at all
(a read-only mount, or a path the process may not create), the certificate is simply kept in memory:
the server still serves HTTPS, it just gets a new certificate on every start.

The certificate covers `localhost`, `127.0.0.1`, `::1`, the machine hostname and the address the
server listens on — when `--bind` is a specific IP, that IP; when it is the `0.0.0.0` default, every
routable address of the machine, so `https://<lan-ip>:8443` verifies too. Note that a machine whose
addresses change (a new Docker bridge, a new DHCP lease) no longer matches the saved certificate,
which is then regenerated on the next start; `--bind <ip>` avoids that.

`--tls-selfsigned-hosts` is optional and only needed for names the server cannot discover, such as a
public DNS name:

```bash
static-httpserver --root-dir ./html --tls-selfsigned-hosts www.example.org,192.168.1.10
```

Because the certificate carries proper `SubjectAltName` entries, clients can be told to trust it
instead of skipping verification:

```bash
# Quick and dirty: skip verification
curl -k https://localhost:8443/_health

# Or trust the generated certificate
curl --cacert ./certs/selfsigned-cert.pem https://localhost:8443/_health
```

## Your own certificates

Provide a directory containing `cert.pem` and `key.pem`:

```bash
# CLI
static-httpserver --root-dir ./html --tls-cert-dir /path/to/certs

# Docker
docker run -p 8080:8080 -p 8443:8443 \
    -v /path/to/certs:/certs:ro \
    byjg/static-httpserver
```

When both files are present they always take precedence over the self-signed certificate.

If your files are not named `cert.pem` and `key.pem` — Let's Encrypt, for instance, writes
`fullchain.pem` and `privkey.pem` — point at them directly:

```bash
static-httpserver --root-dir ./html \
    --tls-port 443 \
    --tls-cert-file /etc/letsencrypt/live/example.org/fullchain.pem \
    --tls-key-file /etc/letsencrypt/live/example.org/privkey.pem
```

Both flags must be given together. Unlike `--tls-cert-dir`, an unreadable file here is a fatal
error: the server will **not** silently fall back to a self-signed certificate.

## TLS in front of a backend

To terminate TLS here and forward to an application that only speaks HTTP, or to verify a backend
that speaks HTTPS with a private CA, see [Reverse Proxy](reverse-proxy.md).
