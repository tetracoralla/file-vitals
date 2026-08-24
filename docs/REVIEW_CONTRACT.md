# Review contract

Review current source and runtime; prior reports and generated bundles are only
indexes. At minimum rerun `./scripts/check_all.sh` and the following negative
sequences against the built binary.

1. An extension-mismatched file preserves signature identity and emits
   `EXTENSION_MISMATCH`.
2. An unknown binary remains `unsupported`; it is not guessed from its suffix.
   WebM is not called Matroska, Java class magic is not called Mach-O, and an
   Office suffix does not promote a generic or spoofed ZIP.
3. UTF-8 without a BOM is `probable`, while BOM-backed Unicode is `exact`.
4. A malformed recognized archive or structured file becomes `corrupt` or
   `partial` with stable diagnostics rather than leaking parser text.
5. MCP rejects absolute paths, `..`, URI-like paths, directories, FIFO/device
   inputs, and symlinks in any path component.
6. MCP accepts a normal relative file under `UFI_WORKSPACE_ROOT` and returns the
   same typed result as CLI/library inspection of the same bytes.
7. Archive counts, decompressed scanning, probe stdout/stderr, hashing, time,
   aggregate worker-tree memory, queue admission, file opening, and the
   complete response envelope remain bounded by one call budget.
8. Cancellation kills the worker and any active probe; a later request still
   succeeds.
9. `tools/list` exposes exactly `file_inspect`, `file_inspect_batch`, and
   `workspace_inventory`, each with strict schemas and accurate read-only,
   non-destructive, idempotent, closed-world annotations.
10. The installed plugin's manifest, Skill, MCP command, binary version, and
    live public tool agree.
11. MCP 2026-07-28 `server/discover`, stateless list/call, required request
    metadata, `resultType`, server identity, and unsupported-version error all
    pass, while a 2025-11-25 client retains its legacy response shape.
12. External probe scalars cannot escape the result schema; Fontconfig weight
    is normalized to the OpenType/CSS scale, and a probe that cannot read a
    recognized WOFF/WOFF2 returns `partial` rather than claiming corruption.
13. Binary data containers such as SQLite do not receive text metadata or the
    `text_extractable` routing trait merely because their broad kind is `data`.
14. Human CLI output shows explicitly requested SHA-256 and deep archive entry
    names; those facts cannot exist only in JSON.
15. The staged macOS app contains the current `finspect` engine and legal
    inventory, opens a selected or dropped file, preserves mode/hash choices,
    presents errors without stale results, and copies both summary and JSON.
16. The Capability adapter survives an oversized request line with one error
    response and serves the next request; concurrency is bounded with queue
    admission sharing each call's deadline, executing plus queued requests have
    a fixed admission cap, responses correlate by envelope id, and a deep
    archive inspection does not leak product-only archive facts into
    `file.inspect@0.1.0`.
17. Cancelling or replacing a macOS app inspection terminates the running
    engine process, a pending task never re-reads the newer selection, and
    release app builds resolve only the bundled engine.
18. Expected SHA-256 returns an explicit match predicate; a mismatch is a typed
    constraint, and invalid digests never start the worker.
19. Batch preserves index/path correlation and per-item authority errors, uses
    one worker and one cumulative deadline/hash/response budget, and survives a
    limit breach with a schema-valid result.
20. Workspace inventory rejects authority escapes and symlink roots, skips
    symlink/special entries, is deterministic, bounds directory enumeration,
    and reports depth/file/directory/entry truncation without implying a
    complete workspace.
21. Archive path/link/device blockers, exact Git LFS indirection, modern binary
    data signatures, OOXML macro/external/embedded facts, SVG active/external
    facts, and PDF text-layer present/absent/unknown states each have negative
    regressions and remain within the published schema.
