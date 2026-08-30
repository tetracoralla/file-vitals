"""Fixed File Vitals dogfood prompts and their deterministic reference specs."""

TASKS = (
    (
        "batch-a",
        "Before changing any code, report deterministic preflight facts for these eight explicit files, "
        "preserving input order: go.mod, README.md, schemas/file-inspect-input.schema.json, "
        "schemas/inspection-result.schema.json, internal/inspector/types.go, app/FileVitals/Package.swift, "
        "LICENSE, dist/plugin/file-vitals-0.3.3-darwin-arm64.tar.gz. For each, give status, size in bytes, "
        "kind, media type, and format. Do not modify anything. Keep the final answer concise.",
    ),
    (
        "batch-b",
        "Before changing any code, report deterministic preflight facts for these eight explicit files, "
        "preserving input order: NOTICE, SECURITY.md, .github/workflows/ci.yml, "
        "schemas/file-inspect-batch-input.schema.json, schemas/workspace-inventory-result.schema.json, "
        "internal/mcp/server.go, app/FileVitals/Resources/Info.plist, "
        "dist/plugin/file-vitals-0.3.0-darwin-arm64.tar.gz. For each, give status, size in bytes, kind, "
        "media type, and format. Do not modify anything. Keep the final answer concise.",
    ),
    (
        "inventory-schemas",
        "Before choosing any file reader, characterize the unknown set of regular files under schemas "
        "through depth 1. Report the bounded inventory status and counts, then list each returned path, "
        "size in bytes, kind, media type, and format. Do not read file contents or modify anything. Keep "
        "the final answer concise.",
    ),
    (
        "inventory-app",
        "Before choosing any file reader, characterize the unknown set of regular files under "
        "app/FileVitals/Sources through depth 3. Report the bounded inventory status and counts, then list "
        "each returned path, size in bytes, kind, media type, and format. Do not read file contents or "
        "modify anything. Keep the final answer concise.",
    ),
    (
        "hash-match",
        "Verify deterministically whether go.mod has expected SHA-256 "
        "4c7af0aca4732c17529aa08b5e391e155562b284c68d32d9bda79e5e5b7778bc. Report the explicit match "
        "predicate, observed digest, status, size, kind, media type, and format. Do not compare digests "
        "mentally and do not modify anything. Keep the final answer concise.",
    ),
    (
        "hash-mismatch",
        "Verify deterministically whether README.md has expected SHA-256 "
        "0000000000000000000000000000000000000000000000000000000000000000. Report the explicit match "
        "predicate, observed digest, status, size, kind, media type, and format. Do not compare digests "
        "mentally and do not modify anything. Keep the final answer concise.",
    ),
    (
        "archive-030",
        "Before extracting dist/plugin/file-vitals-0.3.0-darwin-arm64.tar.gz, report its verified identity, "
        "archive format, parseability, entry count, truncation state, and structural action blockers: "
        "absolute paths, parent traversal paths, links, device entries, encryption, and whether inspection "
        "was complete. Do not extract or modify anything. Keep the final answer concise.",
    ),
    (
        "archive-032",
        "Before extracting dist/plugin/file-vitals-0.3.3-darwin-arm64.tar.gz, report its verified identity, "
        "archive format, parseability, entry count, truncation state, and structural action blockers: "
        "absolute paths, parent traversal paths, links, device entries, encryption, and whether inspection "
        "was complete. Do not extract or modify anything. Keep the final answer concise.",
    ),
)

REFERENCE_SPECS = {
    "batch-a": {
        "operation": "batch",
        "paths": [
            "go.mod",
            "README.md",
            "schemas/file-inspect-input.schema.json",
            "schemas/inspection-result.schema.json",
            "internal/inspector/types.go",
            "app/FileVitals/Package.swift",
            "LICENSE",
            "dist/plugin/file-vitals-0.3.3-darwin-arm64.tar.gz",
        ],
    },
    "batch-b": {
        "operation": "batch",
        "paths": [
            "NOTICE",
            "SECURITY.md",
            ".github/workflows/ci.yml",
            "schemas/file-inspect-batch-input.schema.json",
            "schemas/workspace-inventory-result.schema.json",
            "internal/mcp/server.go",
            "app/FileVitals/Resources/Info.plist",
            "dist/plugin/file-vitals-0.3.0-darwin-arm64.tar.gz",
        ],
    },
    "inventory-schemas": {"operation": "inventory", "root": "schemas", "max_depth": 1},
    "inventory-app": {"operation": "inventory", "root": "app/FileVitals/Sources", "max_depth": 3},
    "hash-match": {
        "operation": "single",
        "path": "go.mod",
        "expected_sha256": "4c7af0aca4732c17529aa08b5e391e155562b284c68d32d9bda79e5e5b7778bc",
    },
    "hash-mismatch": {
        "operation": "single",
        "path": "README.md",
        "expected_sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    },
    "archive-030": {
        "operation": "single",
        "path": "dist/plugin/file-vitals-0.3.0-darwin-arm64.tar.gz",
    },
    "archive-032": {
        "operation": "single",
        "path": "dist/plugin/file-vitals-0.3.3-darwin-arm64.tar.gz",
    },
}
