# Static http server

[![Build Status](https://github.com/byjg/docker-static-httpserver/actions/workflows/build.yml/badge.svg?branch=master)](https://github.com/byjg/docker-static-httpserver/actions/workflows/build.yml)
[![Opensource ByJG](https://img.shields.io/badge/opensource-byjg-success.svg)](http://opensource.byjg.com)
[![Install MCP Server](https://img.shields.io/badge/Install-MCP_Server-8A2BE2?logo=modelcontextprotocol&logoColor=white)](https://opensource.byjg.com/docs/ai/mcpserver-byjg-docs/)
[![GitHub source](https://img.shields.io/badge/Github-source-informational?logo=github)](https://github.com/byjg/docker-static-httpserver/)
[![GitHub license](https://img.shields.io/github/license/byjg/docker-static-httpserver.svg)](https://opensource.byjg.com/license/)
[![GitHub release](https://img.shields.io/github/release/byjg/docker-static-httpserver.svg)](https://github.com/byjg/docker-static-httpserver/releases/)

A really minimal HTTP/HTTPS Server image for static files written in Go.

## Features

* Create a simple HTML website
* Serve static files with HTTP and HTTPS (self-signed certificate generated and reused automatically)
* SPA (Single Page Application) support for frontend frameworks like React, Angular, Vue
* Reverse proxy by path prefix, including TLS termination for a backend that only speaks HTTP
* In-memory LRU file cache with configurable limits
* Health check endpoint for Kubernetes probes
* Really small footprint

## Quick start

```bash
# The built-in parking page, on http://localhost:8080 and https://localhost:8443
docker run -p 8080:8080 -p 8443:8443 byjg/static-httpserver

# Your own static pages
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver

# Without Docker: serve the current directory over HTTPS (port 8443)
static-httpserver --root-dir .
```

## Install

### Docker

```bash
docker pull byjg/static-httpserver
```

### deb/rpm

Add the [ByJG package repository](https://opensource.byjg.com/docs/packages), then:

```bash
# Debian/Ubuntu
apt install static-httpserver

# RHEL/CentOS
yum install static-httpserver
```

### Homebrew

On macOS (or Linux) with [Homebrew](https://brew.sh):

```bash
brew install byjg/tap/static-httpserver
```

Homebrew builds it from source, installing Go only for the build.

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

## Use your own static pages

Mount your own HTML directory to replace the default parking page:

```bash
docker run -p 8080:8080 -v /path/to/local/html:/static byjg/static-httpserver
```

Or create your own image:

```dockerfile
FROM byjg/static-httpserver

COPY /path/to/html /static
```

## SPA Mode (React / Vue / Angular)

When enabled, any request that doesn't match an existing file **and** has no file extension
is served the `index.html` page. This supports client-side routing in frameworks like React, Angular, and Vue.

Requests for missing static assets (e.g., `/missing.css`) still return 404.

```bash
docker run -p 8080:8080 -e SPA_MODE=true byjg/static-httpserver
```

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
| `--health-path`    | `HEALTH_PATH`         | `/_health`   | Path of the health endpoint                                                   |
| `--headers-path`   | `HEADERS_PATH`        | `/_headers`  | Path of the request headers endpoint (needs `--show-headers`)                 |
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

## HTTPS / TLS

HTTPS is **always enabled** (default port 8443). With no certificate provided, the server generates
a self-signed one, saves it in `--tls-cert-dir` and reuses it on the next start. HTTP is
**optional** — it only starts when `--port` or `PORT` is set, and `--only-https` turns it off even
then.

```bash
# Docker: publish the HTTPS port and keep the certificate in a volume
docker run -p 8443:8443 \
    -v $(pwd)/html:/static:ro \
    -v $(pwd)/certs:/certs \
    byjg/static-httpserver
```

To use your own certificate, put `cert.pem` and `key.pem` in `--tls-cert-dir`, or point at the
files with `--tls-cert-file` and `--tls-key-file`.

See [HTTPS / TLS](docs/tls.md) for what the self-signed certificate covers, when it is regenerated,
how to make clients trust it and the details of using your own certificates.

## Reverse Proxy

The server can forward requests matching a path prefix to a backend service, stripping the prefix
before forwarding. When several routes match, the most specific prefix wins.

```bash
# Static frontend plus an API on the same origin
static-httpserver --root-dir ./dist --spa \
    --proxy /api=http://backend:3000 \
    --proxy /auth=http://auth-service:4000

# HTTPS in front of a backend that only speaks HTTP
static-httpserver \
    --root-dir /var/www/empty \
    --only-https --tls-port 443 \
    --proxy /=http://127.0.0.1:3000
```

See [Reverse Proxy](docs/reverse-proxy.md) for the forwarded headers, the catch-all `/` route, a
`npm run serve-https` script for local development, backend TLS verification and a hardened OIDC
example.

## Health Check

The server exposes a `/_health` endpoint that returns `{"status":"ok"}` with HTTP 200.
This is used by the Helm chart for Kubernetes liveness and readiness probes.

The underscore keeps it out of the way of an application's own `/health`, which matters when the
whole site is proxied to a backend. `--health-path` (env `HEALTH_PATH`) moves it; the Helm chart
exposes it as `parameters.healthPath` and points the probes at whatever it is set to.

> **Upgrading:** the endpoint used to be `/health`. Kubernetes probes defined outside this chart,
> uptime monitors and load balancer checks have to be pointed at `/_health`, or the old path
> restored with `--health-path /health`.

## Using with Helm 3

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
  healthPath: ""        # defaults to /_health
  rootDir: ""
  port: ""
  tlsPort: ""
  tlsCertDir: ""
  cacheMaxSize: ""
  cacheMaxFileSize: ""
```

> **Tip:** this HELM package is setup to work with [EasyHAProxy](https://github.com/byjg/docker-easy-haproxy)

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

----
[Open source ByJG](http://opensource.byjg.com)
