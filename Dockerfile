FROM golang:latest
WORKDIR /app
COPY src/go-server.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o go-server .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY html /static
COPY --from=0 /app/go-server .
CMD ["./go-server"]  

