---
timestamp: 2026-08-24T18:45:00+08:00
segment: File Vitals 0.3.2 public closure
status: in_progress
source: codex
---

# Campaign anchor

## Objective and finish condition

Close the current local File Vitals work through a protected GitHub pull
request: preserve the 0.3.1 runtime hardening, add deterministic macOS drop URL
selection regressions, harden canonical Agent reporting and no-retry behavior,
validate the installed 0.3.2 plugin, and obtain current Linux/macOS CI including
Swift XCTest before merging.

Finish when the current branch is committed and pushed, the PR's required Linux
and macOS checks plus current CodeQL are reviewed, the change is squash-merged
through the repository's protection policy, and installed/source/remote state
is reconciled.

## Current state

- Public repository: `tetracoralla/file-vitals`, Apache-2.0; current account has
  ADMIN permission. Current GitHub API state requires strict Linux/macOS checks
  and linear history; force pushes and deletion are disabled, and only squash
  merge is enabled. No approving-review rule or repository ruleset is active.
- Current branch: `codex/file-vitals-closure` based on local commit `ef1c967`;
  the remote `main` remains at `186d72f` and is not ahead.
- Product/plugin version: 0.3.2. The version bump covers the canonical-value
  reporting and successful-call no-retry Skill contract added after 0.3.1.
- macOS drop handling now routes through `DropSelection.firstFileURL`; negative
  XCTest covers empty, remote, and invalid-first-item cases. The test is a URL
  selection regression, not evidence of Finder-to-App delivery.
- `./scripts/check_all.sh`: PASS for Go tests/race/vet/vulnerability, schemas,
  Capability, package guards, plugin legal/probe/checksum, cancellation and
  recovery, release Swift build, and current app bundle.
- Plugin and macOS app packaging now use identical stripped Go build flags and
  a byte-for-byte parity check. Both bundled `finspect` binaries have SHA-256
  `bf696d667e09805008ec21590e240e8fd1c494a22d440b33255b5e056c4e1950`.
- Installed plugin 0.3.2: direct single/batch/inventory/guard/cancellation/
  recovery probe PASS; installed and bundle runtime SHA-256 both
  `bf696d667e09805008ec21590e240e8fd1c494a22d440b33255b5e056c4e1950`.
- Installed cold Agent SHA mismatch task: exactly one `file_inspect` call and
  canonical `status=ok`, `sha256_matches=false` result.

## Agent dogfood conclusion

The opt-in `scripts/run_agent_dogfood.py` uses a neutral fixture workspace,
minimal temporary Codex homes, `gpt-5.6-terra`, read-only sessions, a sanitized
PATH, a fixed order seed, a 180-second per-session deadline, and Go-core
reference projections. It does not modify the user's global plugin state and
is not part of default CI.

The target condition adopted File Vitals in every full-run task. The final
0.3.2 follow-up closed the only observed repeated hash-mismatch confirmation in
three consecutive one-call runs; the two archive variants separately passed
tool-to-reference and answer-to-tool checks while allowing an extra requested
SHA-256 observation. A clean no-marketplace baseline batch sample made zero
target calls, used nine shell commands and 90.374 seconds, and did not match the
canonical reference.

No exact paired token-cost verdict is claimed. A baseline with the local target
marketplace configured discovered `finspect` and was invalid; a clean baseline
does not share File Vitals' canonical taxonomy, so it is not quality-equivalent.
The observed 30,445-byte three-tool catalog therefore remains a measurement,
not a demonstrated optimization requirement. Do not shrink strict schemas
without a new quality-equivalent paired experiment.

## Remaining lanes and next action

- Local Swift XCTest: setup-blocked because the active Command Line Tools Swift
  runtime has no XCTest module. GitHub CI already defines
  `swift test --package-path app/FileVitals` on `macos-15`; the new PR SHA must
  run it.
- Native Finder-to-App drag: runtime BLOCKED in the current Computer Use host.
  Cross-display drag calls completed but did not deliver a URL to the App.
  Logic XCTest and SwiftUI compilation do not replace this observation.
- Business/experience acceptance: owner-held; prior packaged App selection,
  mode/hash, copy, error, and recovery flows passed and were not redesigned.

Final local diff review, packaged App smoke, dogfood-harness offline self-check,
and the complete source/package gate are PASS. PR #1 ran Linux, macOS, and all
configured CodeQL languages successfully on commit `23e2d41`; macOS executed
seven XCTest cases with zero failures. Next executable action: publish this
governance-fact correction, wait for the new head checks, then squash-merge PR
#1 and reconcile local, installed, and remote state.
