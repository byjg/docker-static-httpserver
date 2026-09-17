#!/bin/sh
# Serve a local app over HTTPS.
#
# Starts your app, then puts static-httpserver in front of it as a TLS
# terminator: the app keeps speaking plain HTTP and never deals with
# certificates. Both stop together on Ctrl-C.
#
# Copy it into your project, then:
#
#   ./scripts/serve-https.sh
#   APP_CMD="npm run dev" APP_PORT=5173 ./scripts/serve-https.sh
#
# In package.json:
#
#   "scripts": {
#     "serve": "node server.js",
#     "serve-https": "sh ./scripts/serve-https.sh"
#   }
#
# static-httpserver has to be on the PATH.

APP_CMD="${APP_CMD:-npm run serve}"      # command that starts the app
APP_PORT="${APP_PORT:-3000}"             # port the app listens on
TLS_PORT="${TLS_PORT:-8443}"             # port to serve HTTPS on
CERT_DIR="${CERT_DIR:-.certs}"           # where the self-signed cert is kept
ROOT_DIR="${ROOT_DIR:-.static}"          # static files, if any

mkdir -p "$ROOT_DIR"

$APP_CMD &
APP=$!

static-httpserver \
    --root-dir "$ROOT_DIR" \
    --only-https \
    --tls-port "$TLS_PORT" \
    --tls-cert-dir "$CERT_DIR" \
    --proxy "/=http://127.0.0.1:$APP_PORT" &
SRV=$!

# Ctrl-C (or a stop signal) has to reach both, not just this script.
trap 'kill -TERM $APP $SRV 2>/dev/null' TERM INT

echo "https://localhost:$TLS_PORT -> http://127.0.0.1:$APP_PORT"

wait $SRV
kill -TERM $APP 2>/dev/null
wait $APP 2>/dev/null
