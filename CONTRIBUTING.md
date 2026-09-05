# Contributing to File Vitals

File Vitals identifies and characterizes one file envelope without
extracting content, transforming data, or executing anything from the file.
Keep the Go core authoritative across the CLI, macOS app, MCP, and Capability
adapters. The app may decode and present the published result, but must not
create a second Swift inspection engine.

Run the complete current check before proposing a change:

```sh
python3 -m pip install -r scripts/requirements-check.txt
./scripts/check_all.sh
```

The check requires Go 1.26.6 or newer and network access for the pinned
`govulncheck` scanner and its current vulnerability database.
Use a Python virtual environment for the check dependencies. Plugin packaging
uses Python's standard archive libraries on both Darwin and Linux, plus
`jsonschema` to validate the source Provider Manifest before rendering and
`PyYAML` for the Codex Skill validator used by `check_all.sh`. The
vendored `capabilities/provider-manifest.schema.v0.3.json` is an unchanged copy
of Capability Contracts' same-version schema; update it from that owner when
adopting a new manifest version. Reproducibility comparisons require the same
target, source, Go/Python toolchains and compression library. Different target
binaries are expected to differ.

Every parser, path, bound, or carrier correction needs a focused negative
regression. Do not include credentials, private files, generated bundles, or
local machine paths.

Unless you explicitly state otherwise, any contribution intentionally
submitted for inclusion in File Vitals is licensed under Apache-2.0.
