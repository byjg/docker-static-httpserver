FROM docker.io/golang:1.26 AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:3.23
RUN apk --no-cache add ca-certificates
RUN adduser -D -h /app appuser
WORKDIR /app
COPY --chown=appuser:appuser html /static
COPY --from=builder --chown=appuser:appuser /app/server .
USER appuser
CMD ["./server"]

