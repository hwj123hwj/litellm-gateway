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

# Exercise the actual reinstall decision under the same Bash running this test.
# Syntax-only checks do not catch unsupported expansions on macOS Bash 3.2.
REINSTALL_DECISION=$(sed -n '/^    if .*REPLY/,/^    fi/p' "${INSTALLER}")
ok() { :; }
for REPLY in n N y Y "" no; do
    NEEDS_DOWNLOAD=true
    eval "$REINSTALL_DECISION"
    case "$REPLY" in
        n|N) EXPECTED=false ;;
        *) EXPECTED=true ;;
    esac
    if [ "$NEEDS_DOWNLOAD" != "$EXPECTED" ]; then
        echo "FAIL: unexpected reinstall decision for reply '$REPLY'" >&2
        exit 1
    fi
done

echo "PASS: Unix installer syntax, latest-release download, and reinstall confirmation regression tests"
