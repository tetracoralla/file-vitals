#!/usr/bin/env python3
"""Run an opt-in paired Codex dogfood comparison with and without File Vitals.

This is intentionally not part of check_all.sh: it invokes a hosted model and
uses temporary minimal Codex homes so it never changes the user's plugin state.
"""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import json
import os
from pathlib import Path
import random
import shutil
import subprocess
import tempfile
import time
from typing import Any

from agent_dogfood_cases import TASKS
from agent_dogfood_support import (
    aggregate,
    build_dogfood_workspace,
    canonical_answer_from_payload,
    exception_record,
    expected_answer,
    first_difference,
    fixture_digest,
    percent_change,
    project_optional_observations,
    write_response_schema,
)


ROOT = Path(__file__).resolve().parent.parent
PLUGIN = "file-vitals@file-vitals-local"
CODEX = Path(shutil.which("codex") or "codex").resolve()
MODEL = "gpt-5.6-terra"
ORDER_SEED = 20260824


def run_command(
    arguments: list[str],
    *,
    timeout: int = 30,
    environment: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        arguments,
        cwd=ROOT,
        env=environment,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def dogfood_environment(codex_home: Path) -> dict[str, str]:
    environment = os.environ.copy()
    environment["CODEX_HOME"] = str(codex_home)
    environment["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin"
    return environment


def plugin_is_enabled(codex_home: Path | None = None) -> bool:
    environment = dogfood_environment(codex_home) if codex_home is not None else None
    result = run_command([str(CODEX), "plugin", "list"], environment=environment)
    if result.returncode != 0:
        raise RuntimeError(f"could not list plugins: {result.stderr.strip()}")
    return any(
        PLUGIN in line and "installed, enabled" in line
        for line in result.stdout.splitlines()
    )


def prepare_codex_home(destination: Path, *, target_enabled: bool) -> None:
    destination.mkdir(parents=True)
    source_codex_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
    source_auth = source_codex_home / "auth.json"
    if not source_auth.is_file():
        raise RuntimeError(f"Codex auth file is missing: {source_auth}")
    destination_auth = destination / "auth.json"
    shutil.copy2(source_auth, destination_auth)
    destination_auth.chmod(0o600)

    environment = dogfood_environment(destination)
    if target_enabled:
        marketplace = run_command(
            [str(CODEX), "plugin", "marketplace", "add", str(ROOT / "dist/plugin"), "--json"],
            environment=environment,
        )
        if marketplace.returncode != 0:
            raise RuntimeError(
                f"could not add temporary marketplace: {marketplace.stderr.strip() or marketplace.stdout.strip()}"
            )
        install = run_command(
            [str(CODEX), "plugin", "add", PLUGIN, "--json"],
            environment=environment,
        )
        if install.returncode != 0:
            raise RuntimeError(
                f"could not install temporary plugin: {install.stderr.strip() or install.stdout.strip()}"
            )
    if plugin_is_enabled(destination) != target_enabled:
        raise RuntimeError(f"temporary plugin state did not become enabled={target_enabled}")


def run_task(
    condition: str,
    task_id: str,
    prompt: str,
    include_final: bool,
    workspace: Path,
    codex_home: Path,
    response_schema: Path,
    expected: dict[str, Any],
) -> dict[str, Any]:
    environment = dogfood_environment(codex_home)
    environment["UFI_WORKSPACE_ROOT"] = str(workspace)
    started = time.monotonic()
    structured_prompt = (
        f"{prompt} Return only the JSON object required by the response schema. "
        f"Set task_id to {task_id!r}; use null for non-applicable fact fields."
    )
    command = [
        str(CODEX),
        "exec",
        "--model",
        MODEL,
        "--ephemeral",
        "--json",
        "--output-schema",
        str(response_schema),
        "-s",
        "read-only",
        "-C",
        str(workspace),
        "--skip-git-repo-check",
        structured_prompt,
    ]
    timed_out = False
    try:
        result = subprocess.run(
            command,
            cwd=workspace,
            env=environment,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=180,
            check=False,
        )
    except subprocess.TimeoutExpired as error:
        timed_out = True
        stdout = error.stdout or ""
        stderr = error.stderr or ""
        if isinstance(stdout, bytes):
            stdout = stdout.decode(errors="replace")
        if isinstance(stderr, bytes):
            stderr = stderr.decode(errors="replace")
        result = subprocess.CompletedProcess(command, 124, stdout, stderr)
    events = []
    parse_errors = 0
    for line in result.stdout.splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            parse_errors += 1

    completed_items = [event["item"] for event in events if event.get("type") == "item.completed"]
    usage_events = [event["usage"] for event in events if event.get("type") == "turn.completed"]
    messages = [item["text"] for item in completed_items if item.get("type") == "agent_message"]
    mcp_calls = [
        item
        for item in completed_items
        if item.get("type") == "mcp_tool_call" and item.get("server") == "file-vitals"
    ]
    commands = [item for item in completed_items if item.get("type") == "command_execution"]
    target_cli_commands = [
        command
        for command in commands
        if any(marker in command.get("command", "") for marker in ("finspect", "file-vitals-capability"))
    ]
    parsed_answer = None
    answer_parse_error = ""
    if messages:
        try:
            parsed_answer = json.loads(messages[-1])
        except json.JSONDecodeError as error:
            answer_parse_error = str(error)
    tool_matches_reference: bool | None = None
    answer_matches_tool_result: bool | None = None
    if condition == "target":
        if len(mcp_calls) != 1:
            reference_difference = f"expected one File Vitals call, observed {len(mcp_calls)}"
        else:
            call_result = mcp_calls[0].get("result") or {}
            payload = call_result.get("structured_content") or call_result.get("structuredContent")
            if not isinstance(payload, dict):
                reference_difference = "File Vitals call did not expose structured content"
            else:
                observed = canonical_answer_from_payload(task_id, payload)
                tool_difference = first_difference(project_optional_observations(observed, expected), expected)
                answer_difference = (
                    first_difference(parsed_answer, observed)
                    if parsed_answer is not None
                    else "final answer was not JSON"
                )
                tool_matches_reference = tool_difference is None
                answer_matches_tool_result = answer_difference is None
                reference_difference = (
                    f"tool vs reference: {tool_difference}"
                    if tool_difference
                    else (f"answer vs tool: {answer_difference}" if answer_difference else None)
                )
    else:
        reference_difference = (
            first_difference(parsed_answer, expected) if parsed_answer is not None else "final answer was not JSON"
        )
    record: dict[str, Any] = {
        "condition": condition,
        "task_id": task_id,
        "completed": result.returncode == 0 and bool(usage_events) and bool(messages),
        "timed_out": timed_out,
        "answer_matches_reference": reference_difference is None,
        "tool_matches_reference": tool_matches_reference,
        "answer_matches_tool_result": answer_matches_tool_result,
        "reference_difference": reference_difference or "",
        "answer_parse_error": answer_parse_error,
        "return_code": result.returncode,
        "elapsed_seconds": round(time.monotonic() - started, 3),
        "parse_errors": parse_errors,
        "input_tokens": usage_events[-1].get("input_tokens") if usage_events else None,
        "cached_input_tokens": usage_events[-1].get("cached_input_tokens") if usage_events else None,
        "output_tokens": usage_events[-1].get("output_tokens") if usage_events else None,
        "reasoning_output_tokens": usage_events[-1].get("reasoning_output_tokens") if usage_events else None,
        "target_tool_calls": len(mcp_calls),
        "target_tools": [call.get("tool") for call in mcp_calls],
        "target_cli_command_calls": len(target_cli_commands),
        "command_calls": len(commands),
        "failed_command_calls": sum(1 for command in commands if command.get("exit_code") not in (0, None)),
        "stderr_tail": result.stderr[-1000:] if result.returncode != 0 else "",
    }
    if include_final:
        record["final_message"] = messages[-1] if messages else ""
        record["commands"] = [command.get("command") for command in commands]
    return record


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Run 16 hosted-model sessions in isolated temporary Codex homes."
    )
    parser.add_argument("--jobs", type=int, default=2, choices=range(1, 5))
    parser.add_argument("--include-final", action="store_true")
    parser.add_argument(
        "--allow-hosted-model-run",
        action="store_true",
        help="required acknowledgement that this invokes 16 hosted-model sessions",
    )
    arguments = parser.parse_args()
    if not arguments.allow_hosted_model_run:
        parser.error("--allow-hosted-model-run is required")
    global_plugin_before = plugin_is_enabled()
    codex_version = run_command([str(CODEX), "--version"]).stdout.strip()
    plugin_manifest = json.loads((ROOT / ".codex-plugin/plugin.json").read_text())

    records: list[dict[str, Any]] = []
    with tempfile.TemporaryDirectory(prefix="file-vitals-agent-dogfood-") as temporary:
        temporary_root = Path(temporary)
        workspace = temporary_root / "workspace"
        workspace.mkdir()
        build_dogfood_workspace(workspace)
        workspace_digest = fixture_digest(workspace)
        response_schema = temporary_root / "response.schema.json"
        write_response_schema(response_schema)
        references = {task_id: expected_answer(task_id, workspace) for task_id, _ in TASKS}
        target_home = temporary_root / "target-codex-home"
        baseline_home = temporary_root / "baseline-codex-home"
        prepare_codex_home(target_home, target_enabled=True)
        prepare_codex_home(baseline_home, target_enabled=False)
        baseline_cache_absent = not (baseline_home / "plugins/cache/file-vitals-local").exists()
        work_items = [
            (condition, task_id, prompt)
            for task_id, prompt in TASKS
            for condition in ("target", "baseline")
        ]
        random.Random(ORDER_SEED).shuffle(work_items)
        with ThreadPoolExecutor(max_workers=arguments.jobs) as executor:
            futures = {}
            for condition, task_id, prompt in work_items:
                codex_home = target_home if condition == "target" else baseline_home
                future = executor.submit(
                    run_task,
                    condition,
                    task_id,
                    prompt,
                    arguments.include_final,
                    workspace,
                    codex_home,
                    response_schema,
                    references[task_id],
                )
                futures[future] = (condition, task_id)
            for future in as_completed(futures):
                condition, task_id = futures[future]
                try:
                    records.append(future.result())
                except Exception as error:
                    records.append(exception_record(condition, task_id, error))

    global_plugin_untouched = plugin_is_enabled() == global_plugin_before

    records.sort(key=lambda record: (record["condition"], record["task_id"]))
    target = aggregate([record for record in records if record["condition"] == "target"])
    baseline = aggregate([record for record in records if record["condition"] == "baseline"])
    target_by_task = {record["task_id"]: record for record in records if record["condition"] == "target"}
    baseline_by_task = {record["task_id"]: record for record in records if record["condition"] == "baseline"}
    qualified_task_ids = [
        task_id
        for task_id, _ in TASKS
        if target_by_task[task_id]["completed"]
        and target_by_task[task_id]["answer_matches_reference"]
        and baseline_by_task[task_id]["completed"]
        and baseline_by_task[task_id]["answer_matches_reference"]
        and baseline_by_task[task_id]["target_tool_calls"] == 0
        and baseline_by_task[task_id]["target_cli_command_calls"] == 0
    ]
    qualified_target = aggregate([target_by_task[task_id] for task_id in qualified_task_ids])
    qualified_baseline = aggregate([baseline_by_task[task_id] for task_id in qualified_task_ids])
    summary = {
        "design": {
            "task_families": 4,
            "fixtures_per_family": 2,
            "runs_per_condition": len(TASKS),
            "paired_runs": len(TASKS),
            "only_intended_difference": "file-vitals plugin availability",
            "neutral_fixture_workspace": True,
            "minimal_temporary_codex_homes": True,
            "baseline_has_no_target_marketplace_or_plugin": True,
            "model_requested": MODEL,
            "codex_executable": str(CODEX),
            "codex_version": codex_version,
            "plugin_version": plugin_manifest["version"],
            "fixture_sha256": workspace_digest,
            "order_seed": ORDER_SEED,
            "sanitized_path": "/usr/bin:/bin:/usr/sbin:/sbin",
            "baseline_installed_cache_absent": baseline_cache_absent,
            "global_plugin_untouched": global_plugin_untouched,
        },
        "target": target,
        "baseline": baseline,
        "quality_qualified_pair_count": len(qualified_task_ids),
        "quality_qualified_task_ids": qualified_task_ids,
        "quality_qualified_target": qualified_target,
        "quality_qualified_baseline": qualified_baseline,
        "quality_qualified_target_vs_baseline_percent": {
            "input_tokens": percent_change(
                qualified_target["input_tokens"], qualified_baseline["input_tokens"]
            ),
            "output_tokens": percent_change(
                qualified_target["output_tokens"], qualified_baseline["output_tokens"]
            ),
            "reasoning_output_tokens": percent_change(
                qualified_target["reasoning_output_tokens"],
                qualified_baseline["reasoning_output_tokens"],
            ),
            "command_calls": percent_change(
                qualified_target["command_calls"], qualified_baseline["command_calls"]
            ),
        },
        "runs": records,
    }
    print(json.dumps(summary, indent=2, ensure_ascii=False))
    target_valid = (
        target["completed_runs"] == len(TASKS)
        and target["reference_match_runs"] == len(TASKS)
        and target["target_adoption_runs"] == len(TASKS)
    )
    baseline_uncontaminated = baseline["target_tool_calls"] == 0 and baseline["target_cli_command_calls"] == 0
    harness_valid = global_plugin_untouched and baseline_cache_absent and baseline_uncontaminated
    return 0 if target_valid and harness_valid else 1


if __name__ == "__main__":
    raise SystemExit(main())
