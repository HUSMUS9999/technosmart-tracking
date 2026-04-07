FROM golang:alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o fiber-tracker main.go

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/fiber-tracker /app/fiber-tracker
COPY --from=builder /app/config.json /app/config.json
# Create necessary directories
RUN mkdir -p /app/internal/whatsapp && \
    mkdir -p /app/internal/excel && \
    mkdir -p /app/internal/models && \
    mkdir -p /app/internal/scheduler && \
    mkdir -p /app/internal/watcher && \
    mkdir -p /app/internal/config && \
    mkdir -p /app/web/static

# We also need the static files! Oh, Go doesn't embed them by default unless we use go:embed.
# Let's see if the project uses go:embed for static files.
# I will copy the web folder just in case.
COPY --from=builder /app/web/static /app/web/static

EXPOSE 8080

CMD ["./fiber-tracker"]
