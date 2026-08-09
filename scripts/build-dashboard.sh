#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WEB_DIR="${REPO_ROOT}/web"
EMBED_DIR="${REPO_ROOT}/go-gateway/internal/dashboard/static"

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to build the Dashboard" >&2
  exit 1
fi

cd "${WEB_DIR}"
npm ci
npm run build

rm -rf "${EMBED_DIR}"
mkdir -p "${EMBED_DIR}"
cp -R "${WEB_DIR}/dist/renderer/." "${EMBED_DIR}/"

echo "Dashboard assets embedded at ${EMBED_DIR}"
