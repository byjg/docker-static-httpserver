FROM docker.io/golang:1.26 AS builder
RUN apt-get update && apt-get install -y make && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY go.mod main.go Makefile ./
RUN make build

FROM alpine:3.23
RUN apk --no-cache add ca-certificates
RUN adduser -D -h /app appuser
WORKDIR /app
COPY --chown=appuser:appuser html /static
COPY --from=builder --chown=appuser:appuser /app/bin/static-httpserver .
USER appuser
CMD ["./static-httpserver"]

