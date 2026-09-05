#!/usr/bin/env python3
"""Render or verify the self-contained release Provider Manifest."""

from __future__ import annotations

import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator, FormatChecker

SCHEMA_PATH = Path(__file__).resolve().parents[1] / "capabilities/provider-manifest.schema.v0.3.json"


def fail(message: str) -> None:
    raise SystemExit(f"release Provider Manifest error: {message}")


def reject_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    value: dict[str, object] = {}
    for key, item in pairs:
        if key in value:
            fail(f"duplicate JSON key: {key}")
        value[key] = item
    return value


def load(path: Path) -> dict[str, object]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=reject_duplicate_keys)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        fail(f"cannot read {path}: {exc}")
    if not isinstance(value, dict):
        fail("manifest root must be an object")
    return value


def rendered(source: dict[str, object]) -> dict[str, object]:
    formats = FormatChecker()
    if "uri" not in formats.checkers:
        fail("URI validation is unavailable; install scripts/requirements-check.txt")
    validator = Draft202012Validator(load(SCHEMA_PATH), format_checker=formats)
    errors = list(validator.iter_errors(source))
    if errors:
        fail(f"source does not satisfy Provider Manifest v0.3: {errors[0].message}")
    if source.get("schemaVersion") != "openadam.provider-manifest.v0.3":
        fail("source schemaVersion must be openadam.provider-manifest.v0.3")
    implementations = source.get("implementations")
    if not isinstance(implementations, list) or len(implementations) != 1:
        fail("File Vitals must declare exactly one Capability implementation")
    implementation = implementations[0]
    if not isinstance(implementation, dict):
        fail("Capability implementation must be an object")
    adapter = implementation.get("adapter")
    probe = implementation.get("transportSchemaProbe")
    if not isinstance(adapter, dict) or not isinstance(probe, dict):
        fail("source adapter and transport schema probe must both be declared")

    # Round-trip through JSON to retain every semantic field while replacing
    # only development launch mechanics with self-contained release paths.
    result = json.loads(json.dumps(source))
    release_implementation = result["implementations"][0]
    release_implementation["adapter"] = {
        "protocol": "openadam.capability-jsonl.v0.1",
        "command": "./runtime/file-vitals-capability",
        "args": [],
        "cwd": ".",
    }
    release_implementation["transportSchemaProbe"] = {
        "protocol": "openadam.transport-schema-jsonl.v0.1",
        "command": "./runtime/file-vitals-transport-schema-probe",
        "args": [],
        "cwd": ".",
    }
    return result


def encoded(value: dict[str, object]) -> str:
    return json.dumps(value, indent=2, ensure_ascii=False) + "\n"


def main() -> None:
    arguments = sys.argv[1:]
    check = arguments[:1] == ["--check"]
    if check:
        arguments = arguments[1:]
    if len(arguments) != 2:
        fail("usage: render_release_provider_manifest.py [--check] SOURCE TARGET")
    source_path, target_path = map(Path, arguments)
    expected = encoded(rendered(load(source_path)))
    if check:
        try:
            actual = target_path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            fail(f"cannot read {target_path}: {exc}")
        if actual != expected:
            fail(f"{target_path} differs from the deterministic release manifest")
        implementation = load(target_path)["implementations"][0]
        for field in ("adapter", "transportSchemaProbe"):
            command = implementation[field]["command"]
            bundle = target_path.parents[1]
            executable = bundle / command
            for entry in (executable, *executable.parents):
                if entry.is_symlink():
                    fail(f"{field} command traverses a symlink: {command}")
                if entry == bundle:
                    break
            if not executable.is_file() or not executable.stat().st_mode & 0o111:
                fail(f"{field} command is not a bundled executable: {command}")
        print("release Provider Manifest launchers and bytes: ok")
        return
    try:
        target_path.write_text(expected, encoding="utf-8")
    except OSError as exc:
        fail(f"cannot write {target_path}: {exc}")


if __name__ == "__main__":
    main()
