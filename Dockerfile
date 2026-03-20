FROM docker.io/golang:latest AS builder
WORKDIR /app
COPY src/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY html /static
COPY --from=builder /app/server .
CMD ["./server"]

