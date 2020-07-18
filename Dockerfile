FROM golang:latest as builder
WORKDIR /app
COPY src/server.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY html /static
COPY --from=builder /app/server .
CMD ["./server"]

