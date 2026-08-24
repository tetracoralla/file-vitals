# File Vitals repository contract

Before any review, read `docs/REVIEW_CONTRACT.md`. A plain owner request to
review, audit, 审核, or 复核 invokes that contract end to end; treat it as the
minimum scope and report `tools-dev workspace escalations` without asking the
owner for another checklist.

## Product boundary

File Vitals answers one question: what file facts are available before
another tool acts? It identifies and characterizes a file, records uncertainty
and probe evidence, and derives routing traits. It does not extract document
content, infer dataset schemas, understand images, transform files, or execute
anything found in a file.

The public Agent operations are `file_inspect` for one exact path,
`file_inspect_batch` for 1–16 explicit paths, and `workspace_inventory` for a
bounded directory overview. Use exactly one operation for the current
preflight. CLI, MCP, and library adapters must use the same Go core and result
semantics. The app remains the minimal single-file human surface and invokes
the bundled CLI engine; it must not reimplement inspection semantics in Swift.

## Authority and safety

- Inspection is read-only. Archives are enumerated but never extracted.
- CLI paths are deliberate human paths. MCP paths are relative to the exact
  `UFI_WORKSPACE_ROOT` grant. MCP rejects absolute paths, parent traversal,
  URI-like inputs, non-regular files, and every symlink component.
- The MCP adapter must pass an already-open descriptor into the isolated worker;
  probes must not reopen an Agent-controlled path.
- One deadline, response limit, output limit, archive limit, and memory monitor
  apply to the complete call. A timeout or limit breach terminates the worker.
- A batch or inventory is one call and one worker, not an adapter loop. Batch
  preserves input order and per-item failures. Inventory scans at most 32 files,
  8 levels, and 256 directories without following links.
- Signature evidence outranks the filename extension. Inference and conflicts
  remain explicit; unknown is a valid result.
- Every guard or parser correction requires a negative regression test.

## Acceptance lanes

Report these independently:

- development regression: formatting, vet, tests, build, schemas, plugin checks;
- runtime Agent flow: real MCP initialize/list/call, path rejection, response cap;
- runtime human flow: real CLI inspect, JSON, doctor, errors, and recovery;
  macOS app file selection/drop, mode and hash changes, copy actions, errors,
  and recovery;
- business/experience acceptance: owner judgment of task fit and output utility.

Do not commit or publish unless the owner explicitly asks.
