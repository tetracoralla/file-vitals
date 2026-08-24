#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
cd "${repo_root}"

# These validators own the installed Skill and plugin manifest contracts. The
# all-gates entrypoint must fail closed when they are unavailable; silently
# skipping either would turn an incomplete environment into a false PASS.
skill_root="${UFI_SKILL_ROOT:-$HOME/.codex/skills/.system}"
skill_validator="${skill_root}/skill-creator/scripts/quick_validate.py"
plugin_validator="${skill_root}/plugin-creator/scripts/validate_plugin.py"
for validator in "${skill_validator}" "${plugin_validator}"; do
  if [[ ! -f "${validator}" ]]; then
    echo "required Codex validator not found: ${validator}" >&2
    echo "set UFI_SKILL_ROOT to the directory containing skill-creator and plugin-creator" >&2
    exit 1
  fi
done

unformatted="$(gofmt -l cmd internal schemas capabilities)"
if [[ -n "${unformatted}" ]]; then
  echo "Go files require formatting:" >&2
  echo "${unformatted}" >&2
  exit 1
fi

go mod verify
go vet ./...
go test -race ./...
./scripts/check_vulnerabilities.sh
mkdir -p bin
go build -trimpath -o bin/finspect ./cmd/finspect
go build -trimpath -o bin/file-vitals-capability ./cmd/capability-adapter
bin/finspect doctor
python3 scripts/generate_collection_schemas.py --check
python3 scripts/validate_contract.py bin/finspect "${repo_root}"
python3 scripts/check_agent_economics.py bin/finspect
python3 "${skill_validator}" skills/file-vitals
python3 "${plugin_validator}" "${repo_root}"
./scripts/check_build_path_guard.sh
./scripts/check_app_build_path_guard.sh
./scripts/build_plugin.sh --replace
bundle="${repo_root}/dist/plugin/file-vitals-0.3.2-$(go env GOOS)-$(go env GOARCH)"
python3 scripts/check_release_legal.py "${bundle}"
python3 "${plugin_validator}" "${bundle}"
python3 scripts/probe_plugin.py "${bundle}"
(cd "${repo_root}/dist/plugin" && shasum -a 256 -c "$(basename "${bundle}").tar.gz.sha256")
tar -tzf "${bundle}.tar.gz" >/dev/null

# On macOS with a Swift toolchain, also stage and verify the app bundle: this
# exercises the real app binary, the bundled engine, and the legal inventory.
# The full swift test suite (which needs Xcode) runs in CI instead.
if [[ "$(go env GOOS)" == "darwin" ]] && command -v swift >/dev/null 2>&1; then
  ./script/build_and_run.sh build
  cmp "${bundle}/runtime/finspect" "${repo_root}/dist/File Vitals.app/Contents/Resources/runtime/finspect"
  echo "plugin and macOS app bundled engine parity: ok"
fi

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git diff --check
fi

echo "all File Vitals checks passed"
