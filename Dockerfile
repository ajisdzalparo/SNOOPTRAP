# Multi-stage build for SnoopTrap Global WebRTC Relay Service (< 15MB)
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY relay-service/go.mod relay-service/go.sum* ./
RUN go mod download
COPY relay-service/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o relay-service .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/relay-service .
EXPOSE 8080
ENV PORT=8080
CMD ["./relay-service"]

