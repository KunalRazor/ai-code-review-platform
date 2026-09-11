#!/usr/bin/env bash
#
# Feed the platform some pull requests (offline demo).
#
# Posts signed `pull_request` webhook events to the receiver, exactly like
# GitHub would. The receiver enqueues them, the worker reviews each one (using
# the built-in sample diff in OFFLINE mode), stores PR insights + the review in
# Postgres, and the dashboard fills up.
#
# Usage:
#   ./scripts/send-sample-prs.sh
#
# Env:
#   RECEIVER               receiver base URL      (default http://localhost:8080)
#   GITHUB_WEBHOOK_SECRET  webhook secret         (default empty = OFFLINE mode)
set -euo pipefail

RECEIVER="${RECEIVER:-http://localhost:8080}"
SECRET="${GITHUB_WEBHOOK_SECRET:-}"

titles=(
  "Add pagination to orders API"
  "Fix nil pointer in auth"
  "Refactor config loader"
  "Bugfix: token validation edge case"
  "Add retry/backoff to payment client"
  "Improve logging in worker"
)
authors=(alice bob carol dave erin frank)

for i in "${!titles[@]}"; do
  n=$((i + 1))
  body=$(printf '{"action":"opened","number":%d,"pull_request":{"title":"%s","head":{"sha":"c0ffee%02d"},"base":{"sha":"base000"},"user":{"login":"%s"}},"repository":{"name":"demo-repo","owner":{"login":"acme"}}}' \
    "$n" "${titles[$i]}" "$n" "${authors[$i]}")
  sig="sha256=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $NF}')"
  printf '  -> PR #%d  %s\n' "$n" "${titles[$i]}"
  curl -sS -X POST "$RECEIVER/webhook" \
    -H "X-GitHub-Event: pull_request" \
    -H "X-Hub-Signature-256: $sig" \
    --data-binary "$body"
  echo
done

echo
echo "Sent ${#titles[@]} PRs. Give the worker a few seconds, then open the dashboard:"
echo "  http://localhost:5173"
echo "Watch the worker do the reviews with:  docker-compose logs -f worker"
