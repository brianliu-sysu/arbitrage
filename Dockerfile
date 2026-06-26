# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /arbitrage ./cmd/arbitrage/

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /arbitrage /usr/local/bin/arbitrage
EXPOSE 8080
ENTRYPOINT ["arbitrage"]
CMD ["-config", "/etc/arbitrage/config.yaml"]
