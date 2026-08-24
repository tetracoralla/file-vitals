# File Vitals product model

## Outcome

Give File Vitals one local file and receive a bounded, typed statement
of what it is, which structural properties are available, how certain each
identity claim is, which routing traits follow, and which probes supplied the
evidence.

The dominant Agent request must take one `file_inspect` call. Human surfaces
are the `finspect` CLI and a minimal macOS app. Both call the same bounded Go
core: the CLI provides concise and JSON output, while the app presents one
selected file's useful facts and copy actions without exposing protocol detail.

## Product identity

**File Vitals** is the public brand. `file-vitals` is the repository, plugin,
Skill, MCP server, Provider, and release-bundle slug. The stable task-oriented
contracts remain descriptive: the CLI is `finspect`, the public Agent operation
is `file_inspect`, the portable Capability is `org.openadam.file.inspect`, and
the existing `UFI_*` environment variables remain unchanged.

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

The result is not a reader, converter, extractor, virus scanner, validator of
business content, or data profiler. `file_inspect` describes the file envelope;
a data inspector describes records inside that envelope.

## Shared model

The Go core owns:

1. bounded header inspection and signature evidence;
2. identity normalization and extension-conflict reporting;
3. family probes for text, structured text, images, media, PDF, archives,
   fonts, and executable binaries;
4. routing-trait derivation;
5. stable statuses, diagnostics, provenance, and response fitting.

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

Quick mode performs stat, signature, MIME normalization, and routing traits.
Standard mode adds the applicable family probe. Deep mode adds bounded archive
entry names and more expensive optional metadata; it never extracts content.

The result schema is version `1.0`. Status is one of `ok`, `partial`,
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
- external probe output: 1 MiB stdout and 16 KiB stderr;
- text/structured parse window: 8 MiB;
- optional SHA-256 input: 1 GiB;
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

Routing traits describe safe next-step affordances such as `previewable`,
`text_extractable`, `enumerable`, `extractable`, `transcodable`,
`page_addressable`, or `executable`. They do not claim that a downstream tool is
installed and they never authorize the action.

## Release inventory

A release includes the Go library/core, `finspect` CLI, stdio MCP server, strict
input/output schemas, product-local Codex Skill, plugin manifest, self-contained
platform binary, project and third-party license notices, local marketplace
bundle, checksums, and rerunnable checks. The Darwin release may also include
the minimal macOS app with the same native `finspect` binary bundled inside it.
No network access is required at runtime.

Version 0.1.0 targets Darwin and Linux. Windows packaging remains unsupported
until it can preserve the same already-open-handle authority and worker
isolation semantics; compilation alone is not treated as runtime support.
