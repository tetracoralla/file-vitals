# Third-party notices

File Vitals 0.1.0 is built with the Go toolchain and includes these
direct dependencies in the distributed binary:

- Go 1.26 runtime and standard library — BSD 3-Clause license; see
  `third_party_licenses/Go.txt` and `Go-PATENTS.txt`.
- `github.com/BurntSushi/toml` v1.6.0 — MIT license; see
  `third_party_licenses/BurntSushi-toml.txt`.
- `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3 — Apache-2.0 license;
  see `third_party_licenses/jsonschema-NOTICE.txt`; the Apache-2.0 terms are
  included in the root `LICENSE` file.
- `go.yaml.in/yaml/v3` v3.0.5 — Apache-2.0 and MIT notices; see
  `third_party_licenses/yaml-v3-LICENSE.txt` and `yaml-v3-NOTICE.txt`.
- `github.com/dlclark/regexp2` v1.11.0 (transitive) — MIT license; see
  `third_party_licenses/regexp2-LICENSE.txt`.
- `golang.org/x/text` v0.14.0 (transitive) — BSD 3-Clause license; see
  `third_party_licenses/x-text-LICENSE.txt`.

Optional programs discovered at runtime (`file`, `ffprobe`, `pdfinfo`, and
`fc-scan`) are not bundled.
