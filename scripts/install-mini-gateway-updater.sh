#!/usr/bin/env bash
# One-time, unprivileged bootstrap for the q user on the Linux mini host.
set -Eeuo pipefail

REPOSITORY=hwj123hwj/litellm-gateway
API="https://api.github.com/repos/$REPOSITORY"
RAW="https://raw.githubusercontent.com/$REPOSITORY"

for command in curl python3 systemctl; do
  command -v "$command" >/dev/null || { echo "Missing required command: $command" >&2; exit 1; }
done
systemctl --user show-environment >/dev/null || { echo 'systemd user manager is unavailable' >&2; exit 1; }

REVISION=$(curl --fail --silent --show-error --location --retry 2 --connect-timeout 8 --max-time 45 \
  -H 'Accept: application/vnd.github+json' "$API/commits/main" |
  python3 -c 'import json,re,sys; s=json.load(sys.stdin)["sha"]; assert re.fullmatch(r"[0-9a-f]{40}",s); print(s)')
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
for path in scripts/gateway-auto-update.sh deploy/systemd/gateway-auto-update.service deploy/systemd/gateway-auto-update.timer; do
  mkdir -p "$TEMP_DIR/$(dirname "$path")"
  curl --fail --silent --show-error --location --retry 2 --connect-timeout 8 --max-time 45 \
    "$RAW/$REVISION/$path" -o "$TEMP_DIR/$path"
done

mkdir -p "$HOME/.local/bin" "$HOME/.config/systemd/user" "$HOME/.config/litellm-gateway" "$HOME/.local/state/litellm-gateway-update"
install -m 0755 "$TEMP_DIR/scripts/gateway-auto-update.sh" "$HOME/.local/bin/gateway-auto-update"
install -m 0644 "$TEMP_DIR/deploy/systemd/gateway-auto-update.service" "$HOME/.config/systemd/user/gateway-auto-update.service"
install -m 0644 "$TEMP_DIR/deploy/systemd/gateway-auto-update.timer" "$HOME/.config/systemd/user/gateway-auto-update.timer"

CONFIG="$HOME/.config/litellm-gateway/update.env"
if [[ ! -e "$CONFIG" ]]; then
  umask 077
  cat > "$CONFIG" <<CONFIG_EOF
GATEWAY_API_BASE=https://api.github.com/repos/hwj123hwj/litellm-gateway
GATEWAY_BINARY=$HOME/.llm-gateway/bin/gateway
GATEWAY_SERVICE=llm-gateway.service
GATEWAY_HEALTH_URL=http://127.0.0.1:4001/health
GATEWAY_UPDATE_STATE=$HOME/.local/state/litellm-gateway-update
CONFIG_EOF
fi

systemctl --user daemon-reload
systemctl --user enable --now gateway-auto-update.timer
printf 'Installed gateway updater for %s\n' "$REVISION"
printf 'Checking for the latest passing main build now...\n'
systemctl --user start gateway-auto-update.service
