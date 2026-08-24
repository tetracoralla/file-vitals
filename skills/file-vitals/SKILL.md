---
name: file-vitals
description: Inspect one or more local files deterministically, verify an expected SHA-256, or inventory a workspace when an Agent needs real formats, typed structural properties, action blockers, uncertainty, integrity, or safe next-tool routing before acting. Use for unknown, extensionless, mislabeled, media, image, document, archive, font, text, data-envelope, build, or executable files; not for extracting content, understanding images, inferring dataset schemas, searching content, or converting files.
---

# File Vitals

Choose exactly one operation for the current preflight:

- Call `file_inspect` for one known path, including expected SHA-256 verification.
- Call `file_inspect_batch` for 1–16 already-known paths. Do not loop over
  `file_inspect`; preserve each returned index and path.
- Call `workspace_inventory` when the relevant paths are not yet known. It
  returns a bounded overview of at most 32 regular files, 256 directories, and
  4,096 directory entries and never follows symlinks. Inspect a selected file
  afterward only when deeper facts are needed.

Treat signature identity, confidence, conflicts, constraints, diagnostics, and
provenance as the result; do not re-guess the type from its filename.

- Use `standard` by default.
- Use `quick` when identity and routing traits are enough.
- Use `deep` only when bounded archive entry names or expensive metadata changes
  the task.
- Request `sha256` only when content identity is needed; it has a fixed size and
  time budget.
- Supply `expected_sha256` when a known digest must be verified. Use the explicit
  `sha256_matches` predicate; do not compare digests mentally.

Pass a path relative to the workspace granted by the host. `E_WORKSPACE_REQUIRED`,
path-authority errors, a stable unsupported result, or a limit error is terminal
for that call. Do not bypass the boundary with shell probes or retry with an
absolute path. Ask the user to grant the intended workspace only when that grant
is genuinely missing.

Present the canonical identity, properties that affect the next action,
constraints, uncertainty or conflicts, and material diagnostics. Package path
facts, active content, external references, encryption, embedded objects, and
indirection are routing blockers—not malware or safety verdicts. `partial`
means usable facts exist but a promised family property is missing; it is not
total failure.

Keep the boundary clear: this tool answers what the file envelope is. Use a
document extractor for content, a data inspector for record shape, a visual
model for image meaning, a search tool for content lookup, and a transformer
for conversion.
