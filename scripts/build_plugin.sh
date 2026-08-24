#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
version="0.3.1"
goos="$(go env GOOS)"
case "${goos}" in
  darwin|linux) ;;
  *) echo "0.3.1 runtime packaging supports Darwin and Linux" >&2; exit 1 ;;
esac
platform="${goos}-$(go env GOARCH)"
dist_root="${repo_root}/dist/plugin"
bundle_name="file-vitals-${version}-${platform}"
target="${dist_root}/${bundle_name}"
marketplace_root="${dist_root}/.agents"
marketplace_plugins="${marketplace_root}/plugins"
marketplace_file="${marketplace_plugins}/marketplace.json"
replace="false"

if [[ "${1:-}" == "--replace" && $# -eq 1 ]]; then
  replace="true"
elif [[ $# -ne 0 ]]; then
  echo "usage: build_plugin.sh [--replace]" >&2
  exit 2
fi

case "${target}" in
  "${repo_root}/dist/plugin/file-vitals-"*) ;;
  *) echo "refusing unexpected build target: ${target}" >&2; exit 1 ;;
esac

for candidate in "${repo_root}/dist" "${dist_root}" "${target}" "${marketplace_root}" "${marketplace_plugins}" "${marketplace_file}"; do
  if [[ -L "${candidate}" ]]; then
    echo "refusing symlinked build path: ${candidate}" >&2
    exit 1
  fi
done

if [[ -e "${target}" ]]; then
  if [[ "${replace}" != "true" || ! -d "${target}" ]]; then
    echo "bundle already exists; rerun with --replace for this exact version" >&2
    exit 1
  fi
  rm -rf -- "${target}"
fi

mkdir -p "${dist_root}"
build_tmp="$(mktemp -d "${TMPDIR:-/tmp}/ufi-build.XXXXXX")"
cleanup() {
  case "${build_tmp:-}" in
    "${TMPDIR:-/tmp}/ufi-build."*)
      if [[ -d "${build_tmp}" && ! -L "${build_tmp}" ]]; then rm -rf -- "${build_tmp}"; fi
      ;;
  esac
}
trap cleanup EXIT

stage="${build_tmp}/${bundle_name}"
mkdir -p "${stage}/runtime" "${stage}/.codex-plugin" "${stage}/skills" "${stage}/schemas" "${stage}/capabilities/schemas" "${stage}/docs" "${stage}/third_party_licenses"
go build -trimpath -ldflags="-s -w" -o "${stage}/runtime/finspect" ./cmd/finspect
go build -trimpath -ldflags="-s -w" -o "${stage}/runtime/file-vitals-capability" ./cmd/capability-adapter
cp "${repo_root}/.codex-plugin/plugin.json" "${stage}/.codex-plugin/plugin.json"
cp -R "${repo_root}/skills/file-vitals" "${stage}/skills/file-vitals"
cp "${repo_root}/schemas/"*.json "${stage}/schemas/"
cp "${repo_root}/capabilities/provider.json" "${stage}/capabilities/provider.json"
cp "${repo_root}/capabilities/schemas/"*.json "${stage}/capabilities/schemas/"
cp "${repo_root}/docs/PRODUCT_MODEL.md" "${repo_root}/docs/REVIEW_CONTRACT.md" "${stage}/docs/"
cp "${repo_root}/LICENSE" "${repo_root}/NOTICE" "${repo_root}/README.md" "${repo_root}/THIRD_PARTY_NOTICES.md" "${stage}/"
cp "${repo_root}/third_party_licenses/"* "${stage}/third_party_licenses/"

cat > "${stage}/.mcp.json" <<'JSON'
{
  "mcpServers": {
    "file-vitals": {
      "command": "./runtime/finspect",
      "args": ["mcp"],
      "cwd": ".",
      "env_vars": ["UFI_WORKSPACE_ROOT"]
    }
  }
}
JSON

mv "${stage}" "${target}"
mkdir -p "${marketplace_plugins}"
cat > "${marketplace_file}" <<JSON
{
  "name": "file-vitals-local",
  "interface": {"displayName": "File Vitals Local"},
  "plugins": [
    {
      "name": "file-vitals",
      "source": {"source": "local", "path": "./${bundle_name}"},
      "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
      "category": "Productivity"
    }
  ]
}
JSON

archive="${dist_root}/${bundle_name}.tar.gz"
checksum="${archive}.sha256"
if [[ -L "${archive}" || -L "${checksum}" ]]; then
  echo "refusing symlinked archive target" >&2
  exit 1
fi
rm -f -- "${archive}" "${checksum}"
tar -C "${dist_root}" -czf "${archive}" "${bundle_name}"
(cd "${dist_root}" && shasum -a 256 "${bundle_name}.tar.gz" > "${bundle_name}.tar.gz.sha256")
echo "${target}"
