#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/file-vitals-app-path-guard.XXXXXX")"
cleanup() {
  case "${scratch}" in
    "${TMPDIR:-/tmp}"/file-vitals-app-path-guard.*) rm -rf -- "${scratch}" ;;
  esac
}
trap cleanup EXIT

fixture_repo="${scratch}/repo"
outside="${scratch}/outside"
mkdir -p "${fixture_repo}/script" "${outside}"
cp "${repo_root}/script/build_and_run.sh" "${fixture_repo}/script/build_and_run.sh"
printf 'preserve\n' > "${outside}/sentinel"
ln -s "${outside}" "${fixture_repo}/dist"

if "${fixture_repo}/script/build_and_run.sh" build >"${scratch}/build.out" 2>&1; then
  echo "app build accepted a symlinked dist directory" >&2
  exit 1
fi
if ! grep -q "refusing symlinked app build path" "${scratch}/build.out"; then
  echo "app build did not fail at the symlink guard" >&2
  sed -n '1,20p' "${scratch}/build.out" >&2
  exit 1
fi
if [[ ! -f "${outside}/sentinel" ]]; then
  echo "app build path guard modified the symlink target" >&2
  exit 1
fi

mkdir -p "${scratch}/real.app"
ln -s "${scratch}/real.app" "${scratch}/linked.app"
if python3 "${repo_root}/scripts/check_app_bundle.py" "${scratch}/linked.app" >"${scratch}/check.out" 2>&1; then
  echo "app bundle checker accepted a symlinked bundle root" >&2
  exit 1
fi
if ! grep -q "symlink is not allowed" "${scratch}/check.out"; then
  echo "app bundle checker did not report the root symlink" >&2
  sed -n '1,20p' "${scratch}/check.out" >&2
  exit 1
fi

echo "macOS app build path symlink guard: ok"
