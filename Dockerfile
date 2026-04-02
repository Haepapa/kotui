# ─── Stage 1: Build the frontend ────────────────────────────────────────────
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci --quiet

COPY frontend/ ./
RUN npm run build

# ─── Stage 2: Build the Go binary ────────────────────────────────────────────
FROM golang:1.25-alpine AS go-builder

# CGO is not needed for the headless build (Wails excluded via -tags headless,
# modernc.org/sqlite is pure Go). Pure CGO_ENABLED=0 gives a fully static binary.
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Copy source.
COPY . .

# Overwrite frontend/dist with the freshly built assets from Stage 1.
COPY --from=frontend-builder /app/frontend/dist ./frontend/dist

# Build the binary. -tags headless excludes the Wails GUI package entirely,
# producing a pure-Go, CGO-free binary with no GTK/WebKit dependency.
# -w -s strips debug info to reduce image size.
RUN go build -tags headless -ldflags="-w -s" -o /kotui .

# ─── Stage 3: Runtime image ──────────────────────────────────────────────────
FROM alpine:3.21 AS runtime

# Install CA certificates (needed for web_search HTTPS requests).
RUN apk add --no-cache ca-certificates openssh-client tzdata

# Create a non-root user for the process.
RUN addgroup -S kotui && adduser -S kotui -G kotui

# Copy the binary.
COPY --from=go-builder /kotui /usr/local/bin/kotui

# /data is the sole external mount point.
#   - config.toml lives here
#   - SQLite database lives here
#   - Agent sandbox workspace lives here
# Agents can ONLY write inside /data (enforced by the MCP sandbox boundary).
RUN mkdir -p /data && chown kotui:kotui /data
VOLUME ["/data"]

USER kotui

# Expose no ports — headless mode communicates via Dispatcher + relay gateway.
# Phase 12 relay adapters may expose ports; add them to their own service block.

ENTRYPOINT ["/usr/local/bin/kotui", "--headless"]
