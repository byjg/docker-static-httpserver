# Static http server

[![Build Status](https://github.com/byjg/docker-static-httpserver/actions/workflows/phpunit.yml/badge.svg?branch=master)](https://github.com/byjg/docker-static-httpserver/actions/workflows/build.yml)
[![Opensource ByJG](https://img.shields.io/badge/opensource-byjg-success.svg)](http://opensource.byjg.com)
[![GitHub source](https://img.shields.io/badge/Github-source-informational?logo=github)](https://github.com/byjg/docker-static-httpserver/)
[![GitHub license](https://img.shields.io/github/license/byjg/docker-static-httpserver.svg)](https://opensource.byjg.com/license/)
[![GitHub release](https://img.shields.io/github/release/byjg/docker-static-httpserver.svg)](https://github.com/byjg/docker-static-httpserver/releases/)

A really minimal HTTP/HTTPS Server image for static files written in Go.

## Why?

* Create a simple HTML website
* Serve static files with HTTP and HTTPS (self-signed certificate generated and reused automatically)
* SPA (Single Page Application) support for frontend frameworks like React, Angular, Vue
* In-memory LRU file cache with configurable limits
* Health check endpoint for Kubernetes probes
* Really small footprint

## How to use the "Parking page"?

The image includes a self-contained parking page (single HTML file, no external dependencies) that can be
customized by setting the environment variables:

* `HTML_TITLE` - Page title (default: "Coming soon")
* `TITLE` - Main heading (default: "soon")
* `MESSAGE` - Body message
* `BG_IMAGE` - Background image URL
* `FACEBOOK` - Facebook page URL
* `TWITTER` - Twitter page URL
* `YOUTUBE` - YouTube page URL

e.g.

```bash
docker run -p 8080:8080 -p 8443:8443 -e TITLE=soon -e "MESSAGE=Keep In Touch" byjg/static-httpserver
```

## Configuration

The server can be configured via CLI flags or environment variables. CLI flags take precedence over environment variables.

| CLI Flag           | Env Variable          | Default      | Description                                                                   |
|--------------------|-----------------------|--------------|-------------------------------------------------------------------------------|
| `--root-dir`       | `ROOT_DIR`            | *(required)* | Root directory for static files                                               |
| `--bind`           | `BIND_ADDRESS`        | `0.0.0.0`    | IP address to listen on                                                       |
| `--only-https`     | `ONLY_HTTPS`          | `false`      | Never start the HTTP listener, even when a port is set                        |
| `--port`           | `PORT`                | *(disabled)* | HTTP listening port. Not set = HTTP disabled                                  |
| `--tls-port`       | `TLS_PORT`            | `8443`       | HTTPS listening port                                                          |
| `--tls-cert-dir`   | `TLS_CERT_DIR`        | *(see below)* | Directory to look for `cert.pem` and `key.pem`, and where the self-signed certificate is saved |
| `--tls-cert-file`  | `TLS_CERT_FILE`       | *(none)*     | TLS certificate file — takes precedence over `--tls-cert-dir`                 |
| `--tls-key-file`   | `TLS_KEY_FILE`        | *(none)*     | TLS private key file — must be used together with `--tls-cert-file`           |
| `--tls-selfsigned-hosts` | `TLS_SELFSIGNED_HOSTS` | *(none)* | Extra hostnames/IPs the self-signed certificate must be valid for (comma-separated) |
| `--spa`            | `SPA_MODE`            | `false`      | Enable SPA routing                                                            |
| `--show-headers`   | `SHOW_HEADERS`        | `false`      | Display request headers on the parking page                                   |
| `--cache-max-size` | `CACHE_MAX_SIZE`      | `50000000`   | Max total cache size in bytes (0 to disable)                                  |
| `--cache-max-file` | `CACHE_MAX_FILE_SIZE` | `5000000`    | Max individual file size to cache in bytes                                    |
| `--proxy`          | `PROXY_ROUTES`        | *(none)*     | Proxy route as `/prefix=http://target`, `/` proxies everything (repeatable flag, comma-separated env) |
| `--proxy-timeout`  | `PROXY_TIMEOUT`       | `30`         | Proxy upstream response timeout in seconds                                    |
| `--proxy-ca`       | `PROXY_CA_FILE`       | *(none)*     | CA certificate file (PEM) for verifying backend TLS — takes precedence over `--proxy-insecure` |
| `--proxy-insecure` | `PROXY_INSECURE`      | `false`      | Skip TLS verification for proxy backends — use only on trusted networks       |
| `--version`        |                       |              | Print version and exit                                                        |

`--tls-cert-dir` defaults to `/certs` when running as root and to `~/.static-httpserver/certs`
otherwise, so an unprivileged process always has somewhere to keep its certificate.

The Docker image sets `--root-dir /static`, `--port 8080` and `--tls-cert-dir /certs` by default.

### CLI Usage

```bash
# Serve current directory over HTTPS (port 8443)
static-httpserver --root-dir .
curl -k https://localhost:8443/

# Add plain HTTP on port 8080
static-httpserver --root-dir /var/www/html --port 8080
curl http://localhost:8080/

# HTTPS only, ignoring any port set through PORT
static-httpserver --root-dir /var/www/html --only-https

# Listen on one address only
static-httpserver --root-dir . --bind 127.0.0.1

# SPA mode
static-httpserver --root-dir ./dist --spa
```

### Install via deb/rpm

```bash
# Debian/Ubuntu
apt install static-httpserver

# RHEL/CentOS
yum install static-httpserver
```

### HTTPS / TLS

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

#### Self-signed certificate (default)

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
curl -k https://localhost:8443/health

# Or trust the generated certificate
curl --cacert ./certs/selfsigned-cert.pem https://localhost:8443/health
```

#### Your own certificates

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

### SPA Mode

When enabled, any request that doesn't match an existing file **and** has no file extension
is served the `index.html` page. This supports client-side routing in frameworks like React, Angular, and Vue.

Requests for missing static assets (e.g., `/missing.css`) still return 404.

```bash
docker run -p 8080:8080 -e SPA_MODE=true byjg/static-httpserver
```

### Reverse Proxy

The server can forward requests matching a path prefix to a backend service. This is useful for:
- Avoiding CORS issues by serving the frontend and API from the same origin
- Hiding backend services from direct client access
- Replacing nginx/caddy as a reverse proxy sidecar in Kubernetes
- Putting HTTPS in front of a backend that only speaks HTTP

```bash
# CLI — multiple routes
static-httpserver --root-dir ./dist --spa \
    --proxy /api=http://backend:3000 \
    --proxy /auth=http://auth-service:4000

# Docker — comma-separated env
docker run -p 8080:8080 \
    -e SPA_MODE=true \
    -e PROXY_ROUTES="/api=http://backend:3000,/auth=http://auth:4000" \
    byjg/static-httpserver
```

The proxy strips the prefix before forwarding: a request to `/api/users` is forwarded as `/users` to the target.

When several routes match, the **most specific prefix wins**, whatever order they were given in —
`/api/admin` is chosen over `/api`, and both over `/`.

The `--proxy-timeout` flag (default 30s) controls how long the server waits for a response from the upstream.

Proxied requests carry `X-Forwarded-Proto` and `X-Forwarded-Host` (alongside the `X-Forwarded-For`
added by Go), so a backend behind the HTTPS listener can tell that the client spoke HTTPS — Express
needs it for `req.protocol`, `secure` cookies and absolute redirects. Values set by an upstream proxy
are preserved.

#### HTTPS for a backend that has none

`/` is the catch-all prefix: it matches every path and strips nothing, so the whole site can be
handed to a backend while static-httpserver terminates TLS with its own certificate. The backend
keeps serving plain HTTP and never deals with certificates.

```bash
# Node (or any HTTP backend) on :3000, reachable over HTTPS on :443
static-httpserver \
    --root-dir /var/www/empty \
    --only-https --tls-port 443 \
    --proxy /=http://127.0.0.1:3000
```

```bash
# Docker: TLS terminated here, plain HTTP to the app container
docker run -p 8443:8443 \
    -e ONLY_HTTPS=true \
    -e PROXY_ROUTES="/=http://app:3000" \
    -v $(pwd)/certs:/certs \
    byjg/static-httpserver
```

`/health` is always answered locally, so it stays usable as a probe even behind a catch-all route.
Everything else goes to the backend — routes are matched before the static file lookup — so point
`--root-dir` at an empty directory when the backend owns the whole site.

#### Proxy backend TLS

When the backend uses HTTPS with a self-signed or private CA certificate, use `--proxy-ca` to provide
the CA cert for verification:

```bash
static-httpserver --root-dir ./dist \
    --proxy /api=https://internal-service:8443 \
    --proxy-ca /path/to/ca.crt
```

For trusted internal networks (e.g. WireGuard mesh) where managing a CA cert is impractical,
`--proxy-insecure` skips verification entirely:

```bash
static-httpserver --root-dir ./dist \
    --proxy /api=https://internal-service:8443 \
    --proxy-insecure
```

> **Note:** If both `--proxy-ca` and `--proxy-insecure` are set, `--proxy-ca` takes precedence
> and a warning is logged. Never use `--proxy-insecure` for backends reachable from untrusted networks.

#### TLS termination with hardened OIDC exposure

A common pattern is to use static-httpserver as a TLS-terminating reverse proxy that exposes
only specific OIDC/OAuth endpoints publicly, while keeping the rest of the API internal:

```bash
static-httpserver \
    --root-dir /var/www/empty \
    --tls-port 443 \
    --tls-cert-file /etc/letsencrypt/live/nimbus.example.com/fullchain.pem \
    --tls-key-file /etc/letsencrypt/live/nimbus.example.com/privkey.pem \
    --proxy /.well-known/openid-configuration=https://10.106.103.1:8443/.well-known/openid-configuration \
    --proxy /keys=https://10.106.103.1:8443/keys \
    --proxy /authorize=https://10.106.103.1:8443/authorize \
    --proxy /oauth/token=https://10.106.103.1:8443/oauth/token \
    --proxy /login=https://10.106.103.1:8443/login \
    --proxy /callback=https://10.106.103.1:8443/callback \
    --proxy /userinfo=https://10.106.103.1:8443/userinfo \
    --proxy-ca /var/lib/nimbus/ca.crt
```

In this setup:
- The browser connects over HTTPS with a trusted public cert (Let's Encrypt)
- Only OIDC endpoints are forwarded — the rest of the API returns 404
- The backend connection is verified using the internal CA cert
- The internal API remains unreachable from the public internet

### Health Check

The server exposes a `/health` endpoint that returns `{"status":"ok"}` with HTTP 200.
This is used by the Helm chart for Kubernetes liveness and readiness probes.

## Using with Helm 3

3.2. Using HELM 3

Minimal configuration

```bash
helm repo add byjg https://opensource.byjg.com/helm
helm repo update
helm upgrade --install mysite byjg/static-httpserver \
    --namespace default \
    --set "ingress.hosts={www.example.org,example.org}" \
    --set parameters.title=Welcome
```

Parameters:

```yaml
ingress:
  hosts: []               # Required
parameters:
  htmlTitle: ""
  title: "soon"
  message: ""
  backgroundImage: ""
  facebook: ""
  twitter: ""
  youtube: ""
  spaMode: ""
  showHeaders: ""
  rootDir: ""
  port: ""
  tlsPort: ""
  tlsCertDir: ""
  cacheMaxSize: ""
  cacheMaxFileSize: ""
```

```tip
This HELM package is setup to work with [EasyHAProxy](https://github.com/byjg/docker-easy-haproxy)
```

## Enabling as Addon on MicroK8s

The Parking addon deploys a static webserver to ‘park’ a domain. This involves all
necessary ingress, service and Pods. This addon adds the proper labels which can be
discovered by EasyHAProxy.

To enable this addon:

```
microk8s enable parking <domainlist>
```

… where domainlist is the comma separated list of domains to be parked.

To disable the addon:

```
microk8s disable parking
```

Follow this discussion: [https://discuss.kubernetes.io/t/addon-parking/23186](https://discuss.kubernetes.io/t/addon-parking/23186)

## Use your own static pages

Mount your own HTML directory to replace the default parking page:

```bash
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver
```

## Create your own image

```dockerfile
FROM byjg/static-httpserver

COPY /path/to/html /static
```

## Using with React / Vue / Angular (SPA)

Use a multi-stage Dockerfile to build your frontend app and serve it with SPA routing:

```dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM byjg/static-httpserver
ENV SPA_MODE=true
COPY --from=builder /app/build /static
```

Note: adjust the build output folder depending on your framework:
- **React (CRA)**: `build`
- **Vite**: `dist`
- **Next.js (static export)**: `out`
- **Angular**: `dist/<project-name>/browser`

Then build and run:

```bash
docker build -t myapp .
docker run -p 8080:8080 myapp
```

----
[Open source ByJG](http://opensource.byjg.com)