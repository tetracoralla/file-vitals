#!/usr/bin/env bash
set -euo pipefail

mode="${1:-run}"
app_name="File Vitals"
process_name="FileVitals"
bundle_id="org.openadam.file-vitals"
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
package_dir="${root_dir}/app/FileVitals"
dist_dir="${root_dir}/dist"
app_bundle="${dist_dir}/${app_name}.app"

case "${mode}" in
  run|--build|build|--debug|debug|--logs|logs|--telemetry|telemetry|--verify|verify) ;;
  *)
    echo "usage: $0 [run|--build|--debug|--logs|--telemetry|--verify]" >&2
    exit 2
    ;;
esac

case "${app_bundle}" in
  "${root_dir}/dist/File Vitals.app") ;;
  *) echo "refusing unexpected app bundle path: ${app_bundle}" >&2; exit 1 ;;
esac

if [[ -L "${dist_dir}" || -L "${app_bundle}" ]]; then
  echo "refusing symlinked app build path: ${app_bundle}" >&2
  exit 1
fi

cd "${root_dir}"
swift build -c release --package-path "${package_dir}"
swift_bin_dir="$(swift build -c release --package-path "${package_dir}" --show-bin-path)"
swift_binary="${swift_bin_dir}/${process_name}"

mkdir -p "${dist_dir}"
if [[ -L "${dist_dir}" || -L "${app_bundle}" ]]; then
  echo "refusing symlinked app build path: ${app_bundle}" >&2
  exit 1
fi

stage_root="$(mktemp -d "${dist_dir}/.file-vitals-app.XXXXXX")"
cleanup() {
  case "${stage_root}" in
    "${dist_dir}"/.file-vitals-app.*)
      if [[ -d "${stage_root}" && ! -L "${stage_root}" ]]; then
        rm -rf -- "${stage_root}"
      fi
      ;;
  esac
}
trap cleanup EXIT

stage_bundle="${stage_root}/${app_name}.app"
app_contents="${stage_bundle}/Contents"
app_macos="${app_contents}/MacOS"
app_resources="${app_contents}/Resources"
app_binary="${app_macos}/${process_name}"
mkdir -p "${app_macos}" "${app_resources}/runtime" "${app_resources}/licenses/third_party_licenses"
cp "${swift_binary}" "${app_binary}"
cp "${package_dir}/Resources/Info.plist" "${app_contents}/Info.plist"
# Match the native plugin build so every shipped adapter carries the exact same
# stripped Go engine, not merely another build from the same source tree.
go build -trimpath -ldflags="-s -w" -o "${app_resources}/runtime/finspect" ./cmd/finspect
cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "${app_resources}/licenses/"
cp third_party_licenses/* "${app_resources}/licenses/third_party_licenses/"
chmod +x "${app_binary}" "${app_resources}/runtime/finspect"
python3 scripts/check_app_bundle.py "${stage_bundle}"

# Keep the last known-good bundle in place until the complete replacement has
# built and passed its structural, legal, and representative runtime checks.
if [[ -L "${dist_dir}" || -L "${app_bundle}" ]]; then
  echo "refusing symlinked app build path: ${app_bundle}" >&2
  exit 1
fi
if [[ -e "${app_bundle}" ]]; then
  rm -rf -- "${app_bundle}"
fi
mv "${stage_bundle}" "${app_bundle}"
rmdir "${stage_root}"
stage_root=""
trap - EXIT

open_app() {
  /usr/bin/open -n "${app_bundle}"
}

case "${mode}" in
  run)
    pkill -x "${process_name}" >/dev/null 2>&1 || true
    open_app
    ;;
  --build|build)
    echo "${app_bundle}"
    ;;
  --debug|debug)
    pkill -x "${process_name}" >/dev/null 2>&1 || true
    lldb -- "${app_bundle}/Contents/MacOS/${process_name}"
    ;;
  --logs|logs)
    pkill -x "${process_name}" >/dev/null 2>&1 || true
    open_app
    /usr/bin/log stream --info --style compact --predicate "process == \"${process_name}\""
    ;;
  --telemetry|telemetry)
    pkill -x "${process_name}" >/dev/null 2>&1 || true
    open_app
    /usr/bin/log stream --info --style compact --predicate "subsystem == \"${bundle_id}\""
    ;;
  --verify|verify)
    pkill -x "${process_name}" >/dev/null 2>&1 || true
    open_app
    for _ in 1 2 3 4 5; do
      if pgrep -x "${process_name}" >/dev/null; then
        exit 0
      fi
      sleep 1
    done
    echo "${app_name} did not remain running" >&2
    exit 1
    ;;
esac
