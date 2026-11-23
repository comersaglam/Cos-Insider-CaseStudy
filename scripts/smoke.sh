#!/bin/sh
# Smoke test of a running API: create a notification, read it back, and check the answers.
# Usage: scripts/smoke.sh [base URL], http://localhost:8080 by default.
set -eu

base="${1:-http://localhost:8080}"
key="smoke-$(date +%s)"

created=$(curl -sS --fail-with-body -X POST "$base/v1/notifications" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $key" \
  -d '{"to":"+905551234567","channel":"sms","content":"Smoke test"}')
id=$(printf '%s' "$created" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[ -n "$id" ] || { echo "create: no id in $created" >&2; exit 1; }
echo "created $id"

read=$(curl -sS --fail-with-body "$base/v1/notifications/$id")
printf '%s' "$read" | grep -q '"status":"queued"' || { echo "get: not queued: $read" >&2; exit 1; }
echo "read it back: queued"

again=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$base/v1/notifications" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $key" \
  -d '{"to":"+905551234567","channel":"sms","content":"Smoke test"}')
[ "$again" = 202 ] || { echo "repeat: got $again" >&2; exit 1; }
echo "repeated with the same key: 202"
echo "smoke test passed"
