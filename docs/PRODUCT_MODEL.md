# File Vitals product model

## Outcome

Give File Vitals one local file, an explicit set of files, or one workspace
directory and receive a bounded, typed statement of what is present, which
structural properties are available, what blocks a proposed next action, how
certain each identity claim is, and which probes supplied the evidence.

The dominant Agent request must take exactly one preflight operation:
`file_inspect` for one known path, `file_inspect_batch` for 1–16 known paths,
or `workspace_inventory` when paths are not yet known. Human surfaces are the
`finspect` CLI and a minimal single-file macOS app. All adapters call the same
bounded Go core; the app does not grow a workspace-management surface.

## Product identity

**File Vitals** is the public brand. `file-vitals` is the repository, plugin,
Skill, MCP server, Provider, and release-bundle slug. The stable task-oriented
contracts remain descriptive: the CLI is `finspect`; public Agent operations
are `file_inspect`, `file_inspect_batch`, and `workspace_inventory`; the
portable single-file Capability remains `org.openadam.file.inspect`; and the
existing `UFI_*` environment variables remain unchanged.

The public descriptor is **deterministic file preflight for Agents**. The short
promise is **Know the file before you act.** Neither statement expands the
implemented boundary into content reading, transformation, execution, or
downstream authorization.

## Users and tasks

- A coding, design, or operations Agent needs to choose the next file tool
  without guessing from a name or parsing ad-hoc command output.
- A developer or operator needs a fast, scriptable characterization report and
  an honest explanation when a probe is unavailable or evidence conflicts.
- A macOS user needs the same result through file selection or drag and drop,
  without learning command-line syntax.

The result is not a reader, converter, extractor, virus scanner, content search,
validator of business content, or data profiler. File Vitals describes file
envelopes and bounded workspace composition; a data inspector describes records
inside an envelope.

## Shared model

The Go core owns:

1. bounded header inspection and signature evidence;
2. identity normalization and extension-conflict reporting;
3. family probes for text, structured text, images, media, PDF, archives,
   fonts, and executable binaries;
4. routing-trait and explicit action-constraint derivation;
5. stable statuses, diagnostics, provenance, and response fitting.

It additionally recognizes exact Git LFS pointers and bounded signatures for
Parquet, Arrow/Feather, ORC, Avro, NumPy, HDF5, and WebAssembly. Package facts
cover unsafe archive paths and special entries; OOXML facts cover macro state,
sheet/slide counts, external relationships, and embedded objects; SVG facts
cover scripts and external references; PDF routing distinguishes a present,
absent, or unknown text layer from a bounded page sample.

The CLI, macOS app, and MCP server are adapters. The app invokes the bundled
`finspect` executable and decodes the published result schema instead of
reimplementing inspection rules. Optional system programs (`file`,
`ffprobe`, `pdfinfo`, and `fc-scan`) add evidence or family properties; their
absence or inability to read a recognized container produces a partial result
when the missing probe prevents a promised standard inspection. Probe failure
alone is not evidence that a file is corrupt.

Font `weight` uses the OpenType/CSS 1..1000 scale. Fontconfig probe values are
normalized to that scale before they enter the shared result.

## Public operation

`file_inspect` accepts:

- `path`: a non-empty relative file path inside the MCP workspace grant;
- `mode`: `quick`, `standard` (default), or `deep`;
- `hash`: `none` (default) or `sha256`.
- `expected_sha256`: an optional 64-hex digest that forces bounded SHA-256 and
  returns the explicit `sha256_matches` predicate.

`file_inspect_batch` accepts 1–16 unique relative paths plus the same mode and
hash controls. It preserves input order and returns a full schema-valid result
or stable error for every path. One cumulative hash-input budget, deadline,
worker, memory limit, and response limit covers the whole batch.

`workspace_inventory` accepts a relative directory (default `.`) and a maximum
depth from 0 to 8 (default 4). It deterministically scans up to 32 regular files
and 256 directories, never follows symlinks, and returns compact per-file facts
plus format aggregates and explicit truncation.

Quick mode performs stat, signature, MIME normalization, and routing traits.
Standard mode adds the applicable family probe. Deep mode adds bounded archive
entry names and more expensive optional metadata; it never extracts content.

The single-file result schema is version `1.1`; the batch result schema is
version `1.0`; and the workspace inventory result schema is version `1.1`.
Single-file status is one of `ok`, `partial`,
`unsupported`, `corrupt`, or `error`. Probable text encoding is never presented
as exact; exact encoding requires a byte-order mark or another deterministic
signature. `structured.parseable` is omitted when a bounded validation stops at
an internal limit, so incomplete validation is not misreported as invalidity.

## Resource and cost budgets

- whole call, including queue admission and file opening: 10 seconds in CLI,
  5 seconds in MCP;
- aggregate worker process-group resident memory: 384 MiB hard monitor,
  256 MiB Go soft limit;
- complete worker JSON: 256 KiB;
- complete batch or inventory worker JSON: 192 KiB, within the 256 KiB MCP
  envelope;
- external probe output: 1 MiB stdout and 16 KiB stderr;
- text/structured parse window: 8 MiB;
- optional SHA-256 input: 1 GiB;
- batch: 16 paths and one cumulative 1 GiB SHA-256 input budget;
- inventory: 32 files, 8 levels, 256 directories, and 4,096 directory entries;
- archive scan: 10,000 headers, 64 MiB decompressed scan, at most 200 returned
  names in deep mode;
- every returned filename, diagnostic, and external scalar is length-bounded.

These are product limits, not caller hints. A limit breach is explicit and does
not silently become a confident answer.

The stdio adapter supports both protocol eras: stateless MCP 2026-07-28 with
`server/discover` and required per-request metadata, and the legacy
`initialize` flow through 2025-11-25. Modern and legacy successful response
shapes remain separate.

## Capability boundary

The portable `org.openadam.file.inspect@0.1.0` projection is intentionally
narrower than the product result. In particular, File Vitals archive details
remain product-owned until a later Capability version explicitly defines their
portable meaning; `deep` is still a valid inspection mode at the v0.1 boundary.

Routing traits describe possible next-step affordances such as `previewable`,
`text_extractable`, `enumerable`, `extractable`, `transcodable`,
`page_addressable`, or `executable`. They do not claim that a downstream tool is
installed and they never authorize the action. `constraints` reports observed
blockers such as an integrity mismatch, indirection, encryption, unsafe archive
paths, links/devices, active content, external references, or embedded objects.
These are typed observations, not malware or policy verdicts.

## Release inventory

A release includes the Go library/core, `finspect` CLI, stdio MCP server, strict
input/output schemas, product-local Codex Skill, plugin manifest, self-contained
platform binary, project and third-party license notices, local marketplace
bundle, checksums, and rerunnable checks. The Darwin release may also include
the minimal macOS app with the same native `finspect` binary bundled inside it.
No network access is required at runtime.

Version 0.3.3 targets Darwin and Linux. Windows packaging remains unsupported
until it can preserve the same already-open-handle authority and worker
isolation semantics; compilation alone is not treated as runtime support.
