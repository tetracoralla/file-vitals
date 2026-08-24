#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/ufi-path-guard.XXXXXX")"
cleanup() {
  case "${temporary:-}" in
    "${TMPDIR:-/tmp}/ufi-path-guard."*)
      if [[ -d "${temporary}" && ! -L "${temporary}" ]]; then rm -rf -- "${temporary}"; fi
      ;;
  esac
}
trap cleanup EXIT

fixture_repo="${temporary}/repo"
outside="${temporary}/outside"
mkdir -p "${fixture_repo}/scripts" "${fixture_repo}/dist/plugin" "${outside}"
cp "${script_dir}/build_plugin.sh" "${fixture_repo}/scripts/build_plugin.sh"
ln -s "${outside}" "${fixture_repo}/dist/plugin/.agents"

if output="$("${fixture_repo}/scripts/build_plugin.sh" --replace 2>&1)"; then
  echo "build path guard accepted a symlinked marketplace path" >&2
  exit 1
fi
if [[ "${output}" != *"refusing symlinked build path"* ]]; then
  echo "build path guard failed for an unexpected reason: ${output}" >&2
  exit 1
fi

echo "build path symlink guard: ok"
