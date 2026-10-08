---
sidebar_position: 2
sidebar_label: "Reverse Proxy"
---

# Reverse Proxy

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

## HTTPS for a backend that has none

`/` is the catch-all prefix: it matches every path and strips nothing, so the whole site can be
handed to a backend while static-httpserver terminates TLS with its own certificate (see
[HTTPS / TLS](tls.md)). The backend keeps serving plain HTTP and never deals with certificates.

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

The health endpoint (`/_health`, or whatever `--health-path` is set to) is always answered locally,
so it stays usable as a probe even behind a catch-all route, while a backend's own `/health` is
proxied through untouched. Everything else goes to the backend — routes are matched before the
static file lookup — so point `--root-dir` at an empty directory when the backend owns the whole
site.

## Local development: `npm run serve-https`

[`examples/serve-https.sh`](https://github.com/byjg/docker-static-httpserver/blob/master/examples/serve-https.sh)
starts your app and puts static-httpserver in front of it, so a local app gets HTTPS without
touching its code. Copy it into your project as `scripts/serve-https.sh`, then wire it into
`package.json`:

```json
{
  "scripts": {
    "serve": "node server.js",
    "serve-https": "sh ./scripts/serve-https.sh"
  }
}
```

```bash
npm run serve-https
# https://localhost:8443 -> http://127.0.0.1:3000
```

The ports and the command are environment variables, so the same script works for any stack:

```bash
APP_CMD="npm run dev" APP_PORT=5173 npm run serve-https   # Vite
```

| Variable   | Default         | Meaning                                   |
|------------|-----------------|-------------------------------------------|
| `APP_CMD`  | `npm run serve` | Command that starts the app               |
| `APP_PORT` | `3000`          | Port the app listens on                   |
| `TLS_PORT` | `8443`          | Port to serve HTTPS on                    |
| `CERT_DIR` | `.certs`        | Where the self-signed certificate is kept |
| `ROOT_DIR` | `.static`       | Static files, if any                      |

Ctrl-C stops both — the script forwards the signal to the app instead of leaving it orphaned. The
certificate is kept in `CERT_DIR` and reused, so the browser exception you add survives restarts;
add `.certs/` to `.gitignore`. Requests made before the app finishes booting get a 502 until it is
listening.

> For containers, run the app and static-httpserver as two containers (compose or a Kubernetes pod)
> and point `--proxy /=http://app:3000` at the app, rather than starting both from one entrypoint.

## Proxy backend TLS

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

## TLS termination with hardened OIDC exposure

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
