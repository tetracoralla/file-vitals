---
name: file-vitals
description: Inspect a local file deterministically when an Agent needs its real format, typed structural properties, encoding certainty, extension conflicts, integrity, or safe next-tool routing before acting. Use for unknown, extensionless, mislabeled, media, image, document, archive, font, text, data-envelope, or executable files; not for extracting content, understanding images, inferring dataset schemas, or converting files.
---

# File Vitals

Call `file_inspect` once when the next action depends on what a file actually is.
Treat signature identity, confidence, conflicts, diagnostics, and provenance as
the result; do not re-guess the type from its filename.

- Use `standard` by default.
- Use `quick` when identity and routing traits are enough.
- Use `deep` only when bounded archive entry names or expensive metadata changes
  the task.
- Request `sha256` only when content identity is needed; it has a fixed size and
  time budget.

Pass a path relative to the workspace granted by the host. `E_WORKSPACE_REQUIRED`,
path-authority errors, a stable unsupported result, or a limit error is terminal
for that call. Do not bypass the boundary with shell probes or retry with an
absolute path. Ask the user to grant the intended workspace only when that grant
is genuinely missing.

Present the canonical identity, properties that affect the next action,
uncertainty or conflicts, and material diagnostics. `partial` means usable facts
exist but a promised family property is missing; it is not total failure.

Keep the boundary clear: this tool answers what the file envelope is. Use a
document extractor for content, a data inspector for record shape, a visual
model for image meaning, and a transformer for conversion.
