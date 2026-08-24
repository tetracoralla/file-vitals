---
timestamp: 2026-08-24T14:00:00+08:00
segment: agent file-preflight expansion
status: complete
completed_at: 2026-08-24T14:57:02+08:00
source: codex
---

# Campaign anchor

## Objective and finish condition

Expand File Vitals from one-file preflight into a compact Agent file-preflight
provider that also covers bounded explicit-file batches, bounded workspace
inventory, expected SHA-256 verification, archive/package action blockers,
document-routing facts, indirection files, and common deterministic data/build
format identities.

The campaign is complete when current source and real CLI/MCP runtime expose the
new behavior through strict bounded schemas, the product Skill and plugin agree
with the runtime, the portable single-file Capability remains conformant, and
the development, cancellation/recovery, response-economy, package, installed
Agent, and existing human single-file flows have been rerun and repaired.

Commit, publish, signing/notarization, and owner business/experience acceptance
are outside this implementation authorization.

## Product and route model

- Human user: developer/operator inspecting one deliberate file in the CLI or
  existing macOS app. The app remains the minimum single-file surface unless a
  current human-flow requirement appears.
- Agent tasks: inspect one file; inspect an explicit set of candidate files;
  characterize a bounded workspace before choosing readers; verify a supplied
  SHA-256 without model comparison; observe structural blockers before preview,
  extraction, OCR, parsing, or execution.
- Existing core: `internal/inspector`; adapters: `finspect`, MCP, macOS app,
  JSONL Capability adapter; isolation: supervisor/worker and rooted MCP opens.
- Capability boundary: `org.openadam.file.inspect` remains the portable
  single-file Capability. Batch and workspace inventory are provider product
  operations unless an independently owned portable profile already exists.
- Procedure boundary: none. These are independent deterministic operations,
  not a professional stage graph.
- Skill boundary: ordinary-language routing, mode choice, ambiguity and result
  presentation only; no duplicated schemas or algorithms.
- Dominant route budget: one direct tool call for single-file, explicit batch,
  or workspace inventory; no discovery call; one stable error for invalid input.
- Weakest intended Agent: a low-cost general Agent able to select among three
  semantically distinct file-preflight tools from their names/descriptions.

## Semantic boundaries

In scope:

- Explicit-path batch results preserve input order and correlation, use
  per-item outcomes, and share one cumulative item, deadline, queue, and
  complete-response budget.
- Workspace inventory is read-only, rooted, non-symlink-following, depth/count
  bounded, content-free, and format-aware through the same inspection core.
- Structural blocker fields report observations and uncertainty; they never
  claim a file is safe or authorize a downstream action.
- PDF/OOXML facts remain structural and bounded; no document text is returned.
- New formats receive exact identity only from deterministic signatures.

Out of scope:

- document/data content extraction, summaries, dataset schema inference,
  conversion, malware verdicts, model-specific token estimates, generic grep or
  content search, downstream-tool installation/readiness, and directory writes.

## Current state

- Baseline: `main` at `186d72f` (`feat: release File Vitals 0.1.0`), clean at
  campaign start and tracking `origin/main`.
- Current public Agent operations: `file_inspect`, `file_inspect_batch`, and
  `workspace_inventory`, each with a strict embedded input/output schema.
- Current single-file result schema: `1.1`; batch schema: `1.0`; inventory
  schema: `1.1`; current product/plugin version: `0.3.1`.
- Existing single-file development and release checks are defined by
  `scripts/check_all.sh` and `docs/REVIEW_CONTRACT.md`.
- Implemented semantics: expected SHA-256 predicate; exact Git LFS indirection
  including extension pointers; archive unsafe-path/link/device facts; bounded
  OOXML macro/count/external/embedded facts; SVG active/external facts; PDF
  text-layer present/absent/unknown routing; exact Parquet, Arrow/Feather, ORC,
  Avro, NumPy, HDF5, and WebAssembly identity; batch and inventory cores.
- Implemented carriers: one-worker CLI and MCP batch/inventory, generated batch
  schema, inventory schema, Skill/plugin/docs/version/package alignment, narrow
  Capability projection, and current macOS single-file fact decoding/display.
- Validation completed: targeted Go tests and full `go test ./...`; generated
  schema check; representative single/batch/inventory schema validation; Agent
  economics comparison (8 calls to 1, 87.5% call reduction, semantic
  equivalence); `./scripts/check_all.sh` PASS including race, vulnerability,
  plugin packaging/probe, cancellation/recovery, archive checksum, and macOS
  release build/bundle verification. `swift build -c release` PASS; `swift
  test` is environment-blocked because this Command Line Tools runtime has no
  XCTest module.
- Installed route completed: local marketplace installation is enabled at
  `0.3.1`; the installed MCP passed single/batch/inventory, authority guard,
  cancellation, and recovery probes; the installed runtime SHA-256 matches the
  final bundle (`602916e83c3bd70c3247d0356c5b6a5fdc7926fadab5847f2f214927ba56a2ce`).
- Cold ordinary-language routing completed without shell/file reads: the Agent
  chose `file_inspect_batch` for three explicit files, `workspace_inventory`
  for an unknown set under `schemas`, and `file_inspect` for expected SHA-256,
  one matching call per task. The results preserved order, counted eight files,
  and returned the explicit `sha256_matches=false` predicate.
- Real macOS UI exercise completed against the packaged app: file selection,
  Deep mode, SHA-256 recomputation, explicit re-inspection, both copy actions,
  visible missing-file error, and recovery to a successful README inspection
  all behaved correctly. The temporary recovery fixture was removed afterward.

## Source of record and validation ladder

- Product boundary and budgets: `docs/PRODUCT_MODEL.md`.
- Minimum adversarial/runtime checks: `docs/REVIEW_CONTRACT.md`.
- Exact public schemas: `schemas/` and live MCP `tools/list`.
- Shared semantics: `internal/inspector/`.
- Agent carrier: `internal/mcp/`, `skills/file-vitals/`, `.codex-plugin/`.
- Human carrier: `cmd/finspect/`, `app/FileVitals/`.
- Portable Capability: `capabilities/` and `cmd/capability-adapter/`.

Rerunnable ladder:

1. targeted Go and Swift tests for each changed semantic;
2. `gofmt`, schema validation, `go vet`, race tests, and builds;
3. real CLI single/batch/inventory happy, invalid, limit, and recovery paths;
4. real legacy and modern MCP list/call, authority rejection, response cap,
   cancellation, batch order/partial failure, and post-failure recovery;
5. `./scripts/check_all.sh`, plugin packaging/validation, portable Capability
   conformance, installed tool activation and cold ordinary-language routing;
6. current macOS single-file flow and owner-held business/experience judgment.

## Continuation state

- Current segment: 0.3.1 independent re-review, repair, and controller-owned
  validation complete.
- Next executable action: no further correctness implementation is required in
  this review. Owner business/experience acceptance and any commit,
  publication, signing, or notarization remain separately authorized actions.
- Blockers: Swift XCTest execution remains setup-blocked because the active
  Command Line Tools Swift runtime does not provide the `XCTest` module; the
  production Swift build and packaged app checks pass. Current UI exercise
  passed selection, mode/hash changes, explicit reinspection, both copy
  actions, visible error, and recovery. A current cross-application file-drop
  run remains unproved because the Computer Use host rejected cross-window
  dragging before delivery; the drop handler is present in current source.
- Update cadence: after shared semantics, after carrier integration, before
  broad validation, and at closeout.

## 0.3.1 independent re-review follow-up

Stable review target was clean `main` at
`c4a3563ed648e3d92c68030e876b74d00dbd76ea`. The review found and repaired six
derivative defects instead of inheriting the earlier completion claim:

- the installed 0.3.0 Skill pointed at current source while its cached runtime
  binary was stale;
- MCP and Capability readers admitted an unbounded goroutine wait queue behind
  their four-worker semaphores;
- workspace inventory used `ReadDir(-1)` in the carrier process, allowing an
  Agent-selected directory to allocate for every name outside the isolated
  worker memory monitor;
- batch duplicate checks accepted lexically equivalent paths such as `a` and
  `./a`;
- the authority guard rejected only selected URI spellings and accepted other
  RFC 3986 scheme forms such as `data:` and `urn:`;
- the inventory result did not expose the new cumulative entry-enumeration
  limit.

The repaired state uses a fixed 16-call admitted-work cap with four executing
calls in both adapters, a cumulative 4,096-entry inventory limit, inventory
schema 1.1 with explicit entry/directory limits, normalized duplicate checks,
generic URI-scheme rejection, and product/plugin version 0.3.1. Each guard has
a negative regression.

Latest rerunnable evidence:

- targeted authority/inspector/MCP/supervisor/worker/CLI/Capability tests:
  PASS;
- `./scripts/check_all.sh`: PASS, including race, vet, vulnerability, schemas,
  Capability, package, CLI/MCP cancellation and recovery, checksums, and macOS
  release build/bundle checks;
- installed 0.3.1 bundle/source/Skill/schema parity and
  `scripts/probe_plugin.py` against the installed cache: PASS;
- cold ordinary-language Agent task: selected one `file_inspect_batch` call for
  three explicit paths and returned three ordered 0.3.1 results;
- Agent economics: eight calls reduced to one (87.5%), response bytes reduced
  from 7,808 to 7,146 (8.5%), with semantic equivalence; the three-tool catalog
  was 30,445 bytes, including 28,698 schema bytes;
- current packaged App selection, Deep, SHA-256, explicit reinspection, both
  copy actions, missing-file error, and recovery: PASS.

Current dirty state is the uncommitted 0.3.1 repair across authority,
inventory/result contracts, MCP/Capability admission, tests, packaging,
documentation, and economics measurement. External state changed only by
updating the enabled local File Vitals plugin installation to 0.3.1. No commit,
push, publication, signing, or notarization was performed.

Optional next campaign, not part of this completed correctness review: first
measure whether the observed 30,445-byte tool catalog materially harms routing
or cold-start economics in real dogfood. Only then optimize self-contained
schemas or carrier exposure without weakening strict output contracts. Do not
expand into content extraction, dataset schema inference, image understanding,
conversion, search, or execution.
