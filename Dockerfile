FROM golang:1.25-alpine AS builder

# No GCC needed — pure Go build (SQLite removed, using Postgres for everything)
RUN apk add --no-cache tzdata ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o fiber-tracker main.go

# Use the same Alpine version as the builder to avoid APK repo drift/TLS issues.
# tzdata and ca-certificates are copied directly from the builder stage —
# no APK network call needed at runtime.
FROM alpine:3.21

WORKDIR /app

# Copy timezone data and CA certs from the builder (no apk run needed)
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/fiber-tracker /app/fiber-tracker
RUN mkdir -p /app/config
COPY --from=builder /app/config.json /app/config/config.json

EXPOSE 9510

CMD ["./fiber-tracker"]
