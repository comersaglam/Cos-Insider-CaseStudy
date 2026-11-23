#!/bin/sh
# Smoke test of the running service: create a notification, read it back, repeat it with its idempotency key, and
# wait for the worker to send it.
# Usage: scripts/smoke.sh [base URL], http://localhost:8080 by default.
set -eu

base="${1:-http://localhost:8080}"
key="smoke-$(date +%s)"
body='{"to":"+905551234567","channel":"sms","content":"Smoke test"}'

created=$(curl -sS --fail-with-body -X POST "$base/v1/notifications" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d "$body")
id=$(printf '%s' "$created" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[ -n "$id" ] || { echo "create: no id in $created" >&2; exit 1; }
echo "created $id"

again=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$base/v1/notifications" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d "$body")
[ "$again" = 202 ] || { echo "repeat: got $again" >&2; exit 1; }
echo "repeated with the same key: 202"

i=0
while [ $i -lt 30 ]; do
  status=$(curl -sS --fail-with-body "$base/v1/notifications/$id" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
  if [ "$status" = sent ]; then
    echo "sent by the worker"
    echo "smoke test passed"
    exit 0
  fi
  i=$((i + 1))
  sleep 1
done
echo "not sent after 30 s; last status: $status" >&2
exit 1
