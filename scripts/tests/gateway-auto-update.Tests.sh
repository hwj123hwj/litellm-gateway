#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/bin"

cat > "$TEMP_DIR/bin/curl" <<'CURL_STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$CURL_ARGS_FILE"
exit 22
CURL_STUB
chmod 0755 "$TEMP_DIR/bin/curl"

cat > "$TEMP_DIR/bin/flock" <<'FLOCK_STUB'
#!/usr/bin/env bash
exit 0
FLOCK_STUB
chmod 0755 "$TEMP_DIR/bin/flock"

if PATH="$TEMP_DIR/bin:$PATH" \
  CURL_ARGS_FILE="$TEMP_DIR/curl-args" \
  GATEWAY_API_BASE="https://example.invalid/test-repository" \
  GATEWAY_UPDATE_STATE="$TEMP_DIR/state" \
  bash "$ROOT_DIR/scripts/gateway-auto-update.sh" >/dev/null 2>&1; then
  echo "expected the mocked API request to fail" >&2
  exit 1
fi

grep -Fxq 'https://example.invalid/test-repository/commits/main' "$TEMP_DIR/curl-args"
grep -Fxq 'Accept: application/vnd.github+json' "$TEMP_DIR/curl-args"
grep -Fxq 'X-GitHub-Api-Version: 2022-11-28' "$TEMP_DIR/curl-args"
echo "Mini-host updater expands its API URL and request headers correctly"
