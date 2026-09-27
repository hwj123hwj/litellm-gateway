#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/bin"

cat > "$TEMP_DIR/bin/curl" <<'CURL_STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$CURL_ARGS_FILE"
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == --config ]]; then
    cp "${args[$((i + 1))]}" "$CURL_AUTH_FILE"
  fi
done
exit 22
CURL_STUB
chmod 0755 "$TEMP_DIR/bin/curl"

cat > "$TEMP_DIR/bin/flock" <<'FLOCK_STUB'
#!/usr/bin/env bash
exit 0
FLOCK_STUB
chmod 0755 "$TEMP_DIR/bin/flock"
printf '%s' 'ghp_testdummy123' > "$TEMP_DIR/github-token"
chmod 0600 "$TEMP_DIR/github-token"

if PATH="$TEMP_DIR/bin:$PATH" \
  CURL_ARGS_FILE="$TEMP_DIR/curl-args" \
  CURL_AUTH_FILE="$TEMP_DIR/curl-auth" \
  GATEWAY_API_BASE="https://example.invalid/test-repository" \
  GATEWAY_GITHUB_TOKEN_FILE="$TEMP_DIR/github-token" \
  GATEWAY_UPDATE_STATE="$TEMP_DIR/state" \
  bash "$ROOT_DIR/scripts/gateway-auto-update.sh" >/dev/null 2>&1; then
  echo "expected the mocked API request to fail" >&2
  exit 1
fi

grep -Fxq 'https://example.invalid/test-repository/commits/main' "$TEMP_DIR/curl-args"
grep -Fxq 'Accept: application/vnd.github+json' "$TEMP_DIR/curl-args"
grep -Fxq 'X-GitHub-Api-Version: 2022-11-28' "$TEMP_DIR/curl-args"
grep -Fxq 'header = "Authorization: Bearer ghp_testdummy123"' "$TEMP_DIR/curl-auth"
if grep -Fq 'ghp_testdummy123' "$TEMP_DIR/curl-args"; then
  echo "GitHub token must not appear in curl command arguments" >&2
  exit 1
fi
echo "Mini-host updater expands its API URL and request headers correctly"
