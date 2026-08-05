FROM golang:1.23-alpine AS builder

WORKDIR /src

# Dep cache layer.
COPY go.mod go.sum ./
RUN go mod download

# Copy sources.
COPY . .

# Build a fully-static, stripped binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/shortener ./cmd/shortener

# ─────────────────────────────────────────

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

WORKDIR /app
COPY --from=builder /out/shortener /app/shortener

EXPOSE 8080 9100 50051
USER nonroot:nonroot

ENTRYPOINT ["/app/shortener"]
