#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
INSTALLER="${SCRIPT_DIR}/../install.sh"

bash -n "${INSTALLER}"

if grep -q 'api.github.com/repos/.*/releases/latest' "${INSTALLER}"; then
    echo "FAIL: Unix installer should not depend on the rate-limited GitHub API" >&2
    exit 1
fi

if ! grep -q 'releases/latest/download/${BINARY_ARTIFACT}' "${INSTALLER}"; then
    echo "FAIL: Unix installer should use the latest-release asset redirect" >&2
    exit 1
fi

echo "PASS: Unix installer syntax and latest-release download regression tests"
