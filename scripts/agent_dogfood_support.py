"""Deterministic fixtures, references, and scoring for agent dogfood runs."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import shutil
import statistics
import subprocess
from typing import Any

from agent_dogfood_cases import REFERENCE_SPECS


ROOT = Path(__file__).resolve().parent.parent


def build_dogfood_workspace(destination: Path) -> None:
    files = (
        "go.mod",
        "README.md",
        "LICENSE",
        "NOTICE",
        "SECURITY.md",
        ".github/workflows/ci.yml",
        "internal/inspector/types.go",
        "internal/mcp/server.go",
        "app/FileVitals/Package.swift",
        "app/FileVitals/Resources/Info.plist",
        "dist/plugin/file-vitals-0.3.0-darwin-arm64.tar.gz",
        "dist/plugin/file-vitals-0.3.2-darwin-arm64.tar.gz",
    )
    for relative in files:
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / relative, target)
    shutil.copytree(ROOT / "schemas", destination / "schemas")
    shutil.copytree(ROOT / "app/FileVitals/Sources", destination / "app/FileVitals/Sources")


def fixture_digest(workspace: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(candidate for candidate in workspace.rglob("*") if candidate.is_file()):
        relative = path.relative_to(workspace).as_posix()
        digest.update(relative.encode())
        digest.update(b"\0")
        digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def write_response_schema(destination: Path) -> None:
    nullable_string = {"type": ["string", "null"]}
    nullable_integer = {"type": ["integer", "null"]}
    nullable_boolean = {"type": ["boolean", "null"]}
    fact_properties: dict[str, Any] = {
        "path": {"type": "string"},
        "status": {
            "type": "string",
            "description": "Exact canonical status from this File Vitals item; never an assessment label.",
        },
        "size_bytes": {"type": "integer"},
        "kind": {"type": "string", "description": "Exact canonical identity.kind value."},
        "media_type": {"type": "string", "description": "Exact canonical identity.media_type value."},
        "format": {"type": "string", "description": "Exact canonical identity.format value."},
        "sha256": nullable_string,
        "sha256_matches": nullable_boolean,
        "archive_format": {
            **nullable_string,
            "description": "Exact canonical archive.format value, or null when absent.",
        },
        "archive_parseable": nullable_boolean,
        "archive_entry_count": nullable_integer,
        "archive_truncated": nullable_boolean,
        "absolute_paths": nullable_integer,
        "parent_paths": nullable_integer,
        "link_entries": nullable_integer,
        "device_entries": nullable_integer,
        "encrypted": nullable_boolean,
        "inspection_complete": nullable_boolean,
    }
    inventory_properties = {
        "file_count": {"type": "integer"},
        "directory_count": {"type": "integer"},
        "entry_count": {"type": "integer"},
        "total_size_bytes": {"type": "integer"},
        "truncated": {"type": "boolean"},
    }
    schema = {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "additionalProperties": False,
        "required": ["task_id", "status", "facts", "inventory"],
        "properties": {
            "task_id": {"type": "string"},
            "status": {
                "type": "string",
                "description": "Exact top-level canonical File Vitals status; never completed, verified, or another assessment label.",
            },
            "facts": {
                "type": "array",
                "items": {
                    "type": "object",
                    "additionalProperties": False,
                    "required": list(fact_properties),
                    "properties": fact_properties,
                },
            },
            "inventory": {
                "anyOf": [
                    {"type": "null"},
                    {
                        "type": "object",
                        "additionalProperties": False,
                        "required": list(inventory_properties),
                        "properties": inventory_properties,
                    },
                ]
            },
        },
    }
    destination.write_text(json.dumps(schema, separators=(",", ":")) + "\n")


def run_reference(arguments: list[str], workspace: Path) -> dict[str, Any]:
    result = subprocess.run(
        [str(ROOT / "bin/finspect"), *arguments, "--json"],
        cwd=workspace,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=30,
        check=False,
    )
    if result.returncode not in (0, 1):
        raise RuntimeError(f"reference command failed: {result.stderr.strip() or result.stdout.strip()}")
    return json.loads(result.stdout)


def canonical_fact(path: str, result: dict[str, Any]) -> dict[str, Any]:
    identity = result["identity"]
    integrity = result.get("integrity", {})
    archive = result.get("archive") or {}
    path_facts = archive.get("path_facts") or {}
    return {
        "path": path,
        "status": result["status"],
        "size_bytes": result["file"]["size_bytes"],
        "kind": identity["kind"],
        "media_type": identity["media_type"],
        "format": identity["format"],
        "sha256": integrity.get("sha256"),
        "sha256_matches": integrity.get("sha256_matches"),
        "archive_format": archive.get("format"),
        "archive_parseable": integrity.get("parseable") if archive else None,
        "archive_entry_count": archive.get("entry_count"),
        "archive_truncated": (
            bool(archive.get("entries_truncated") or archive.get("scan_truncated")) if archive else None
        ),
        "absolute_paths": path_facts.get("absolute_paths"),
        "parent_paths": path_facts.get("parent_paths"),
        "link_entries": path_facts.get("link_entries"),
        "device_entries": path_facts.get("device_entries"),
        "encrypted": archive.get("encrypted"),
        "inspection_complete": path_facts.get("inspection_complete"),
    }


def canonical_answer_from_payload(task_id: str, result: dict[str, Any]) -> dict[str, Any]:
    spec = REFERENCE_SPECS[task_id]
    operation = spec["operation"]
    inventory = None
    if operation == "batch":
        facts = [canonical_fact(item["path"], item["result"]) for item in result["items"]]
    elif operation == "inventory":
        facts = []
        for item in result["items"]:
            path = item["path"]
            if not path.startswith(f"{spec['root']}/"):
                path = f"{spec['root']}/{path}"
            facts.append(
                {
                    "path": path,
                    "status": item["status"],
                    "size_bytes": item["size_bytes"],
                    "kind": item["identity"]["kind"],
                    "media_type": item["identity"]["media_type"],
                    "format": item["identity"]["format"],
                    "sha256": None,
                    "sha256_matches": None,
                    "archive_format": None,
                    "archive_parseable": None,
                    "archive_entry_count": None,
                    "archive_truncated": None,
                    "absolute_paths": None,
                    "parent_paths": None,
                    "link_entries": None,
                    "device_entries": None,
                    "encrypted": None,
                    "inspection_complete": None,
                }
            )
        inventory = {
            "file_count": result["files_scanned"],
            "directory_count": result["directories_scanned"],
            "entry_count": result["entries_scanned"],
            "total_size_bytes": result["total_size_bytes"],
            "truncated": any(
                diagnostic.get("code", "").endswith("LIMIT_REACHED")
                for diagnostic in result.get("diagnostics", [])
            ),
        }
    else:
        facts = [canonical_fact(spec["path"], result)]
    return {"task_id": task_id, "status": result["status"], "facts": facts, "inventory": inventory}


def expected_answer(task_id: str, workspace: Path) -> dict[str, Any]:
    spec = REFERENCE_SPECS[task_id]
    operation = spec["operation"]
    if operation == "batch":
        result = run_reference(["batch", *spec["paths"], "--standard"], workspace)
    elif operation == "inventory":
        result = run_reference(
            ["inventory", spec["root"], "--max-depth", str(spec["max_depth"])],
            workspace,
        )
    else:
        arguments = [spec["path"], "--standard"]
        if "expected_sha256" in spec:
            arguments.extend(["--expect-sha256", spec["expected_sha256"]])
        result = run_reference(arguments, workspace)
    return canonical_answer_from_payload(task_id, result)


def project_optional_observations(actual: dict[str, Any], expected: dict[str, Any]) -> dict[str, Any]:
    projected = json.loads(json.dumps(actual))
    for actual_fact, expected_fact in zip(projected.get("facts", []), expected.get("facts", [])):
        for field in ("sha256", "sha256_matches"):
            if expected_fact.get(field) is None:
                actual_fact[field] = None
    return projected


def first_difference(actual: Any, expected: Any, path: str = "$") -> str | None:
    if type(actual) is not type(expected):
        return f"{path}: type {type(actual).__name__} != {type(expected).__name__}"
    if isinstance(expected, dict):
        if actual.keys() != expected.keys():
            return f"{path}: keys {sorted(actual)} != {sorted(expected)}"
        for key in expected:
            difference = first_difference(actual[key], expected[key], f"{path}.{key}")
            if difference:
                return difference
        return None
    if isinstance(expected, list):
        if len(actual) != len(expected):
            return f"{path}: length {len(actual)} != {len(expected)}"
        for index, value in enumerate(expected):
            difference = first_difference(actual[index], value, f"{path}[{index}]")
            if difference:
                return difference
        return None
    if actual != expected:
        return f"{path}: {actual!r} != {expected!r}"
    return None


def aggregate(records: list[dict[str, Any]]) -> dict[str, Any]:
    completed = [record for record in records if record["completed"]]

    def total(key: str) -> int:
        return sum(int(record[key]) for record in completed if record[key] is not None)

    def median(key: str) -> float | None:
        values = [float(record[key]) for record in completed if record[key] is not None]
        return round(statistics.median(values), 3) if values else None

    return {
        "runs": len(records),
        "completed_runs": len(completed),
        "timed_out_runs": sum(record["timed_out"] for record in records),
        "reference_match_runs": sum(record["answer_matches_reference"] for record in records),
        "tool_reference_match_runs": sum(record["tool_matches_reference"] is True for record in records),
        "answer_tool_match_runs": sum(record["answer_matches_tool_result"] is True for record in records),
        "target_adoption_runs": sum(record["target_tool_calls"] > 0 for record in completed),
        "target_tool_calls": total("target_tool_calls"),
        "target_cli_command_calls": total("target_cli_command_calls"),
        "command_calls": total("command_calls"),
        "input_tokens": total("input_tokens"),
        "cached_input_tokens": total("cached_input_tokens"),
        "output_tokens": total("output_tokens"),
        "reasoning_output_tokens": total("reasoning_output_tokens"),
        "median_elapsed_seconds": median("elapsed_seconds"),
    }


def exception_record(condition: str, task_id: str, error: Exception) -> dict[str, Any]:
    return {
        "condition": condition,
        "task_id": task_id,
        "completed": False,
        "timed_out": False,
        "answer_matches_reference": False,
        "tool_matches_reference": None,
        "answer_matches_tool_result": None,
        "reference_difference": "worker exception",
        "answer_parse_error": "",
        "return_code": 125,
        "elapsed_seconds": 0.0,
        "parse_errors": 0,
        "input_tokens": None,
        "cached_input_tokens": None,
        "output_tokens": None,
        "reasoning_output_tokens": None,
        "target_tool_calls": 0,
        "target_tools": [],
        "target_cli_command_calls": 0,
        "command_calls": 0,
        "failed_command_calls": 0,
        "stderr_tail": f"{type(error).__name__}: {error}"[-1000:],
    }


def percent_change(target: int, baseline: int) -> float | None:
    if baseline == 0:
        return None
    return round((target - baseline) / baseline * 100, 1)
