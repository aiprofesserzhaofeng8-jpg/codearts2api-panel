#!/bin/sh
# Liveness includes the first-install wizard. --ready is traffic readiness.
# No secrets, external DNS, auth requests, or upstream calls.
set -eu
base=${CODEARTS_HEALTH_URL:-http://127.0.0.1:10087}
if body=$(http_proxy= https_proxy= HTTP_PROXY= HTTPS_PROXY= wget -q -T 3 -O - "$base/health" 2>/dev/null) &&
   printf '%s' "$body" | grep -Eq '"service"[[:space:]]*:[[:space:]]*"codearts2api-panel"' &&
   printf '%s' "$body" | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'; then
    echo running
    exit 0
fi
if [ "${1:-}" != '--ready' ] &&
   body=$(http_proxy= https_proxy= HTTP_PROXY= HTTPS_PROXY= wget -q -T 3 -O - "$base/setup/status" 2>/dev/null) &&
   printf '%s' "$body" | grep -Eq '"needs_setup"[[:space:]]*:[[:space:]]*true' &&
   printf '%s' "$body" | grep -Eq '"ready"[[:space:]]*:[[:space:]]*false'; then
    echo setup_required
    exit 0
fi
echo unhealthy
exit 1
