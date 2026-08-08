# =============================================================================
# Stage 1: Builder — compile the Go binary
# =============================================================================
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache ca-certificates
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /akswitch ./cmd/akswitch/

# =============================================================================
# Stage 2: Runtime — Alpine with busybox (built-in) for HEALTHCHECK
# =============================================================================
FROM alpine:3.24

# Create non-root user for runtime
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy CA certificates (needed for outbound HTTPS to upstream APIs)
COPY --from=builder /etc/ssl/certs/ /etc/ssl/certs/
# Copy the Go binary
COPY --from=builder /akswitch /akswitch
RUN chown appuser:appgroup /akswitch

EXPOSE 3000
USER appuser
ENTRYPOINT ["/akswitch"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O - http://localhost:3000/health || exit 1
