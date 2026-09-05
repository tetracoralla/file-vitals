# File Vitals

File Vitals is developed by openAdam.

File Vitals is the deterministic file-preflight layer an Agent can call before
it chooses a format-specific tool. Give it one file, an explicit batch, or a
workspace directory; it reports what the bytes indicate, typed structural
properties, uncertainty, action blockers, routing traits, integrity, and probe
provenance through strict bounded schemas.

**Know the file before you act.**

It is deliberately not a universal reader. It does not extract document text,
infer dataset schemas, interpret image meaning, convert files, execute macros,
or unpack archives.

## What 0.3.3 inspects

| Family | Built-in or optional facts |
| --- | --- |
| Any regular file | size, extension evidence, signature/MIME identity, confidence, conflicts, optional or expected SHA-256 with explicit match predicate |
| Text | BOM, encoding certainty, line endings, exact line count when fully scanned |
| JSON, JSONL, YAML, TOML, CSV, TSV, XML, SVG | bounded syntax validation without data-shape inference |
| PNG, JPEG, GIF, WebP, SVG | dimensions, color model, bit depth or alpha when determinable |
| MP4, MOV, MKV, WebM, MP3, WAV, FLAC, Ogg | container, duration, streams, codecs, rational FPS via optional `ffprobe` |
| ZIP, tar, gzip/tar.gz | bounded enumeration, encryption, unsafe paths, links/devices, honest scanned versus exact totals; never extraction |
| PDF | version, pages/encryption/title/author, and bounded text-layer routing via optional `pdfinfo`/`pdftotext` |
| TTF, OTF, WOFF, WOFF2 | family, style, weight, glyph count and variable axes where available |
| ELF, PE, Mach-O, Java class | executable or bytecode format, architecture, bitness, endianness where applicable |
| OOXML | DOCX/DOCM/XLSX/XLSM/PPTX/PPTM identity, sheet/slide counts, macros, external relationships, and embedded objects after package evidence agrees |
| Data/build envelopes | exact bounded signatures for Parquet, Arrow/Feather, ORC, Avro, NumPy, HDF5, and WebAssembly |
| Indirection | exact Git LFS pointer identity, object ID, and declared target size |

Unknown bytes remain unknown. An extension is evidence, not authority.

## Build and use

Go 1.26.6 or newer is required for development and release builds. Earlier
Go 1.26 patch releases contain standard-library vulnerabilities reachable from
the file and path inspection boundaries. Version 0.3.3 supports Darwin and
Linux, where descriptor inheritance and rooted file opening preserve the MCP
authority boundary. The installed plugin launches its bundled native binary;
it does not require Docker, a background service, or network access.

```bash
go build -o bin/finspect ./cmd/finspect
bin/finspect path/to/file
bin/finspect path/to/file --quick --json
bin/finspect path/to/archive.zip --deep
bin/finspect path/to/file --sha256
bin/finspect path/to/file --expect-sha256 HEX
bin/finspect batch a.json b.parquet missing.bin --quick --json
bin/finspect inventory path/to/workspace --max-depth 4 --json
bin/finspect doctor
```

`quick` performs stat, signatures, identity normalization, and traits.
`standard` adds the applicable family probe. `deep` also returns up to 200
archive entry names. All modes are read-only. Exit codes: `0` ok, partial, or
unsupported; `1` error result; `2` usage error; `3` corrupt file.

The CLI accepts a deliberate human path, including an absolute path. The MCP
surface is narrower: every path must be relative to the exact
`UFI_WORKSPACE_ROOT` grant and every symbolic-link component is rejected.
Inventory also skips symlink entries rather than following them.

Development MCP server:

```bash
UFI_WORKSPACE_ROOT=/absolute/workspace go run ./cmd/finspect mcp
```

The server speaks newline-delimited stdio MCP. It supports the stateless
2026-07-28 `server/discover` and per-request metadata model, while preserving
the legacy `initialize` flow through 2025-11-25. It exposes exactly three
read-only operations: `file_inspect`, `file_inspect_batch`, and
`workspace_inventory`. Each publishes strict input/output schemas and returns
both concise text and structured content.

## macOS app

File Vitals also includes a minimal read-only macOS surface for people who do
not want to use a terminal. Choose or drop one file, select the inspection
depth, optionally request SHA-256, and copy either a concise summary or the full
JSON result. The app bundles and invokes the same `finspect` executable used by
the CLI and plugin; it does not implement a second inspection engine.

```bash
./script/build_and_run.sh
swift test --package-path app/FileVitals  # requires a full Xcode installation
```

The reusable unsigned development app bundle is staged at
`dist/File Vitals.app`, including the native engine and required project and
third-party license notices. The app currently targets macOS 14 or newer;
Linux remains supported through the CLI and Agent plugin. Public app delivery
still requires the separate signing and notarization workflow.

## Build the Codex plugin

```bash
./scripts/build_plugin.sh
UFI_SKILL_ROOT="${UFI_SKILL_ROOT:-$HOME/.codex/skills/.system}"
python3 "$UFI_SKILL_ROOT/plugin-creator/scripts/validate_plugin.py" \
  dist/plugin/file-vitals-0.3.3-$(go env GOOS)-$(go env GOARCH)
codex plugin marketplace add dist/plugin
codex plugin add file-vitals@file-vitals-local
```

The bundle contains the platform binary, Skill, schemas, documentation,
project and third-party license notices, a local marketplace, a `.tar.gz`, and
a SHA-256 checksum. The archive contains only sorted regular files with
normalized metadata, and `check_all.sh` rebuilds it twice to require identical
bytes from unchanged source.
Use `--replace` only when intentionally replacing the same generated version.

## Portable Capability provider

`capabilities/provider.json` binds the product to
`org.openadam.file.inspect@0.1.0`. The bounded JSONL adapter at
`cmd/capability-adapter` preserves the same already-open-file authority and
isolated worker boundary as MCP, then projects the richer product result into
the portable Capability output. `OPENADAM_CAPABILITY_WORKSPACE_ROOT` may grant
an explicit conformance workspace; otherwise the conformance runner's provider
root is used.

The v0.3 Provider Manifest pins the complete resolved Profile digest and
declares the canonical `inspect` adapter target
separately from the public `file_inspect` MCP target. Its executable transport
schema probe reads the live embedded MCP schemas, so canonical adapter
conformance and live transport conformance remain two independent results.

Release bundles also contain standalone `runtime/file-vitals-capability` and
`runtime/file-vitals-transport-schema-probe` executables. Their packaged
Provider Manifest points only to those bundled executables; it does not retain
the source checkout's `go run` launch commands. A Procedure may therefore
invoke the provider and run its live transport-schema probe without the Go
source checkout. Canonical conformance also needs the standard's suite and
its input fixtures (including `capabilities/fixtures/users.json`); those test
inputs are not shipped in the runtime bundle. `adapterBindings[].target` is
a semantic operation identifier, not an executable filesystem path.
`finspect` and its MCP route remain independently available.

## Safety and truth model

- One isolated worker owns the whole call—including a batch or inventory. The parent enforces one deadline
  from file-open and queue admission through final response, a 384 MiB
  aggregate worker-process-group RSS ceiling, bounded stdout/stderr, and
  worker-tree termination.
- Agent-controlled paths are opened through a rooted filesystem capability and
  passed to the worker and external probes as an already-open descriptor.
- Archives are only enumerated. Compressed scanning, header counts, names, and
  response bytes are capped.
- Batch is capped at 16 explicit files and 192 KiB total response; inventory is
  capped at 32 files, 8 levels, 256 directories, 4,096 directory entries, and
  192 KiB total response.
- Signature evidence wins over extensions. Conflicts remain visible.
- Encoding without deterministic evidence is `probable` or `unknown`, never
  promoted to exact.
- Optional probe absence or inability to read a recognized container produces
  `partial` when a promised family fact cannot otherwise be supplied; probe
  failure alone does not prove corruption.

The exact limits and product boundary live in
[`docs/PRODUCT_MODEL.md`](docs/PRODUCT_MODEL.md). Reviewers should use
[`docs/REVIEW_CONTRACT.md`](docs/REVIEW_CONTRACT.md).

## Verify

```bash
./scripts/check_all.sh
```

This runs formatting, dependency verification, vet, race-enabled tests, builds,
JSON Schema checks, a semantic-equivalence and Agent-call-economics comparison
(including observed tool-catalog/schema bytes), Skill/plugin validation,
self-contained packaging, and real CLI/MCP probes
including cancellation and post-cancellation recovery. The
GitHub Actions workflow repeats the native core, plugin packaging, and MCP
runtime probes on both Ubuntu and macOS, and builds the macOS app on macOS.

## License

File Vitals source and generated plugin bundles are licensed under the
Apache License 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE). Bundled
third-party components retain their own terms, listed in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
