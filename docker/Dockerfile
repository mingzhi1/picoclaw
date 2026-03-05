# ============================================================
# Stage 1: Build the metaclaw binary
# ============================================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go generate ./... && \
    CGO_ENABLED=0 go build -tags stdjson \
      -ldflags "-s -w" \
      -o /metaclaw ./cmd/picoclaw

# ============================================================
# Stage 2a: Minimal runtime (default)
# ============================================================
FROM alpine:3.21 AS minimal

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /metaclaw /usr/local/bin/metaclaw

RUN addgroup -g 1000 metaclaw && \
    adduser -D -u 1000 -G metaclaw metaclaw

USER metaclaw

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:18790/health || exit 1

COPY docker/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["gateway"]

# ============================================================
# Stage 2b: Full runtime with MCP support (Node.js + Python)
#   docker build --target full -f docker/Dockerfile .
# ============================================================
FROM node:24-alpine3.21 AS full

RUN apk add --no-cache ca-certificates curl git python3 py3-pip && \
    curl -LsSf https://astral.sh/uv/install.sh | sh && \
    ln -s /root/.local/bin/uv /usr/local/bin/uv && \
    ln -s /root/.local/bin/uvx /usr/local/bin/uvx

COPY --from=builder /metaclaw /usr/local/bin/metaclaw

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:18790/health || exit 1

COPY docker/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["gateway"]
