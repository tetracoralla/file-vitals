#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
cd "${repo_root}"

# Pin the scanner for reproducible behavior while allowing its database to
# remain current. The go.mod patch floor supplies the standard library that is
# part of the scan target.
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
