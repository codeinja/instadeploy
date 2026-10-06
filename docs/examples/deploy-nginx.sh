#!/usr/bin/env bash
# Deploys nginx through the Insta Deploy API and waits for its public URL.
# This does exactly what the dashboard's Deploy button does.
#
# Usage: EMAIL=you@example.com PASSWORD=secret ./deploy-nginx.sh
# Needs: curl, jq, and at least one machine connected in the dashboard.
set -euo pipefail

DASHBOARD=${DASHBOARD:-http://localhost:3000}   # Better Auth (sign-in)
API=${API:-http://localhost:8080/api}           # Insta Deploy API

# Sign in with Better Auth; the returned token works as a Bearer token.
TOKEN=$(curl -sf "$DASHBOARD/api/auth/sign-in/email" -H 'content-type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" | jq -r .token)
auth=(-H "authorization: Bearer $TOKEN")

AGENT_ID=$(curl -sf "$API/agents" "${auth[@]}" | jq -r 'map(select(.status=="ONLINE"))[0].id // empty')
if [ -z "$AGENT_ID" ]; then
  echo "No online machine. Add one in the dashboard and run its docker command first." >&2
  exit 1
fi

ID=$(jq --arg agent "$AGENT_ID" '.agent_id = $agent' "$(dirname "$0")/nginx.json" |
  curl -sf "$API/deployments" "${auth[@]}" -H 'content-type: application/json' -d @- | jq -r .id)
echo "Deployment $ID created"

while true; do
  D=$(curl -sf "$API/deployments/$ID" "${auth[@]}")
  STATUS=$(jq -r .status <<<"$D")
  ROUTE=$(jq -r '.services[0].routes[0] // {} | .status // "PENDING"' <<<"$D")
  echo "status=$STATUS url=$ROUTE"
  case "$STATUS/$ROUTE" in
    FAILED/*)        echo "Failed: $(jq -r .error <<<"$D")"; exit 1 ;;
    RUNNING/READY)   echo "Public URL: $(jq -r '.services[0].routes[0].url' <<<"$D")"; exit 0 ;;
    RUNNING/FAILED)  echo "Running, but the public URL failed: $(jq -r '.services[0].routes[0].error' <<<"$D")"; exit 1 ;;
    RUNNING/DISABLED) echo "Running. Pangolin isn't configured, so there's no public URL."; exit 0 ;;
  esac
  sleep 2
done
