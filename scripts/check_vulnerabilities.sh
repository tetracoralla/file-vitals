#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
cd "${repo_root}"

# Prefer an already-installed scanner so offline and sandboxed check runs do
# not depend on the module proxy; fall back to the pinned module version so a
# machine without the binary still fails closed instead of silently skipping.
# The vulnerability database itself may still require network access. The
# go.mod patch floor supplies the standard library that is part of the scan
# target.
if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./...
else
  go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
fi
