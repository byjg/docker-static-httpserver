# Static http server

[![Build Status](https://github.com/byjg/docker-static-httpserver/actions/workflows/phpunit.yml/badge.svg?branch=master)](https://github.com/byjg/docker-static-httpserver/actions/workflows/build.yml)
[![Opensource ByJG](https://img.shields.io/badge/opensource-byjg-success.svg)](http://opensource.byjg.com)
[![GitHub source](https://img.shields.io/badge/Github-source-informational?logo=github)](https://github.com/byjg/docker-static-httpserver/)
[![GitHub license](https://img.shields.io/github/license/byjg/docker-static-httpserver.svg)](https://opensource.byjg.com/opensource/licensing.html)
[![GitHub release](https://img.shields.io/github/release/byjg/docker-static-httpserver.svg)](https://github.com/byjg/docker-static-httpserver/releases/)

A really minimal HTTP/HTTPS Server image for static files written in Go.

## Why?

* Create a simple HTML website
* Serve static files with HTTP and HTTPS (self-signed certificate by default)
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
docker run -p 8080:8080 -e TITLE=soon -e "MESSAGE=Keep In Touch" byjg/static-httpserver
```

## Configuration

The server can be configured via CLI flags or environment variables. CLI flags take precedence over environment variables.

| CLI Flag | Env Variable | Default | Description |
|---|---|---|---|
| `--root-dir` | `ROOT_DIR` | *(required)* | Root directory for static files |
| `--port` | `PORT` | *(disabled)* | HTTP listening port. Not set = HTTP disabled |
| `--tls-port` | `TLS_PORT` | `8443` | HTTPS listening port |
| `--tls-cert-dir` | `TLS_CERT_DIR` | `/certs` | Directory to look for `cert.pem` and `key.pem` |
| `--spa` | `SPA_MODE` | `false` | Enable SPA routing |
| `--show-headers` | `SHOW_HEADERS` | `false` | Display request headers on the parking page |
| `--cache-max-size` | `CACHE_MAX_SIZE` | `50000000` | Max total cache size in bytes (0 to disable) |
| `--cache-max-file` | `CACHE_MAX_FILE_SIZE` | `5000000` | Max individual file size to cache in bytes |
| `--version` | | | Print version and exit |

The Docker image sets `--root-dir /static` and `--port 8080` by default.

### CLI Usage

```bash
# Serve current directory on HTTPS only (port 8443)
static-httpserver --root-dir .

# Serve with both HTTP and HTTPS
static-httpserver --root-dir /var/www/html --port 8080

# SPA mode
static-httpserver --root-dir ./dist --port 3000 --spa
```

### Install via deb/rpm

```bash
# Debian/Ubuntu
apt install static-httpserver

# RHEL/CentOS
yum install static-httpserver
```

### HTTPS / TLS

HTTPS is always enabled (default port 8443). By default, a **self-signed certificate** is generated
in memory at startup.

To use your own certificates, provide a directory with `cert.pem` and `key.pem`:

```bash
# CLI
static-httpserver --root-dir ./html --tls-cert-dir /path/to/certs

# Docker
docker run -p 8080:8080 -p 8443:8443 \
    -v /path/to/certs:/certs:ro \
    byjg/static-httpserver
```

HTTP is **optional** — only started when `--port` or `PORT` is set.

### SPA Mode

When enabled, any request that doesn't match an existing file **and** has no file extension
is served the `index.html` page. This supports client-side routing in frameworks like React, Angular, and Vue.

Requests for missing static assets (e.g., `/missing.css`) still return 404.

```bash
docker run -p 8080:8080 -e SPA_MODE=true byjg/static-httpserver
```

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
