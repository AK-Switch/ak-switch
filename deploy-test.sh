#!/bin/bash
# Acceptance test for Docker Compose full stack (Alvus + Prometheus + Grafana).
# Runs from WSL2 where Docker is available.
#
# Usage:
#   bash deploy-test.sh
#
# Prerequisites:
#   - Docker & docker compose installed
#   - Ports 3000, 9090, 3001 available
#   - .env file exists at project root

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

PASS=0
FAIL=0

cleanup() {
  echo ""
  echo "=== Cleaning up ==="
  docker compose down -v 2>/dev/null || true
}
trap cleanup EXIT

assert() {
  local desc="$1"
  shift
  if eval "$@"; then
    echo "  ✅ $desc"
    ((PASS++))
  else
    echo "  ❌ $desc"
    ((FAIL++))
  fi
}

wait_for_http() {
  local url="$1"
  local label="$2"
  local max_wait="${3:-30}"
  for i in $(seq 1 "$max_wait"); do
    if curl -sf "$url" > /dev/null 2>&1; then
      echo "  --> $label ready after ${i}s"
      return 0
    fi
    sleep 1
  done
  echo "  --> $label NOT ready after ${max_wait}s"
  return 1
}

echo "=== Docker Compose Deployment Acceptance Tests ==="
echo ""

# ── 1. Config syntax ──────────────────────────────
echo "[1] Configuration validation"
assert "docker compose config succeeds" \
  "docker compose config > /dev/null 2>&1"

# ── 2. Start stack ────────────────────────────────
echo ""
echo "[2] Starting services"
docker compose up -d 2>&1 | sed 's/^/  /'

# ── 3. Service health ─────────────────────────────
echo ""
echo "[3] Service readiness"

echo "  Waiting for Alvus..."
wait_for_http "http://localhost:3000/health" "Alvus" 30 || assert "Alvus /health responds" "false"

echo "  Waiting for Prometheus..."
wait_for_http "http://localhost:9090/-/ready" "Prometheus" 30 || assert "Prometheus ready" "false"

echo "  Waiting for Grafana..."
wait_for_http "http://localhost:3001/api/health" "Grafana" 30 || assert "Grafana ready" "false"

# ── 4. Alvus health check ─────────────────────────
echo ""
echo "[4] Alvus health endpoint"
ALVUS_HEALTH=$(curl -sf http://localhost:3000/health 2>/dev/null || echo "")
assert "Alvus /health returns status ok" \
  "echo '$ALVUS_HEALTH' | grep -q '\"status\":\"ok\"'"
assert "Alvus /health returns keys count" \
  "echo '$ALVUS_HEALTH' | grep -q '\"keys\"'"

# ── 5. Prometheus target scraping ─────────────────
echo ""
echo "[5] Prometheus targets"
PROM_TARGETS=$(curl -sf 'http://localhost:9090/api/v1/targets' 2>/dev/null || echo "")
assert "Prometheus has alvus target" \
  "echo '$PROM_TARGETS' | grep -q 'alvus:3000'"

# ── 6. Prometheus metric query ────────────────────
echo ""
echo "[6] Prometheus metric scraping"
# Wait for at least one scrape cycle
sleep 15
PROM_QUERY=$(curl -sf 'http://localhost:9090/api/v1/query?query=alvus_requests_total' 2>/dev/null || echo "")
assert "Prometheus has alvus_requests_total metric" \
  "echo '$PROM_QUERY' | grep -q 'alvus_requests_total'"

# Check Go runtime metrics (emitted automatically)
PROM_GO=$(curl -sf 'http://localhost:9090/api/v1/query?query=go_info' 2>/dev/null || echo "")
assert "Prometheus has Go runtime metrics" \
  "echo '$PROM_GO' | grep -q 'go_info'"

# Check upstream CB state metric exists
PROM_CB=$(curl -sf 'http://localhost:9090/api/v1/query?query=alvus_upstream_cb_state' 2>/dev/null || echo "")
assert "Prometheus has alvus_upstream_cb_state" \
  "echo '$PROM_CB' | grep -q 'alvus_upstream_cb_state'"

# ── 7. Grafana health ─────────────────────────────
echo ""
echo "[7] Grafana health"
GRAFANA_HEALTH=$(curl -sf http://localhost:3001/api/health 2>/dev/null || echo "")
assert "Grafana reports database ok" \
  "echo '$GRAFANA_HEALTH' | grep -q '\"database\":\"ok\"'"

# ── 8. Grafana datasource provisioning ────────────
echo ""
echo "[8] Grafana datasource provisioning"
# Try with anonymous auth first, fall back to admin default
DS_URL="http://localhost:3001/api/datasources"
DS_RESULT=$(curl -sf "$DS_URL" 2>/dev/null || curl -sf -u "admin:admin" "$DS_URL" 2>/dev/null || echo "")
assert "Grafana has Prometheus datasource" \
  "echo '$DS_RESULT' | grep -q 'Prometheus'"

# ── 9. Grafana dashboard provisioning ─────────────
echo ""
echo "[9] Grafana dashboard provisioning"
SEARCH_URL='http://localhost:3001/api/search?query=Alvus'
DASH_RESULT=$(curl -sf "$SEARCH_URL" 2>/dev/null || curl -sf -u "admin:admin" "$SEARCH_URL" 2>/dev/null || echo "")
assert "Grafana has pre-provisioned 'Alvus Overview' dashboard" \
  "echo '$DASH_RESULT' | grep -q 'Alvus Overview'"

# ── 10. Container health ─────────────────────────
echo ""
echo "[10] Container state"
CONTAINERS=$(docker compose ps --format json 2>/dev/null || echo "")
assert "Alvus container is running" \
  "echo '$CONTAINERS' | grep -q 'alvus' && docker compose ps --services | grep -q '^alvus$'"
assert "Prometheus container is running" \
  "docker compose ps --services | grep -q '^prometheus$'"
assert "Grafana container is running" \
  "docker compose ps --services | grep -q '^grafana$'"

# ── Summary ───────────────────────────────────────
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi