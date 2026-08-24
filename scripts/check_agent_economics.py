#!/usr/bin/env python3
"""Compare repeated single-file MCP calls with the bounded batch operation."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def send(process: subprocess.Popen, value: dict) -> None:
    assert process.stdin is not None
    process.stdin.write(json.dumps(value, separators=(",", ":")) + "\n")
    process.stdin.flush()


def receive(process: subprocess.Popen, wanted_id: int) -> tuple[dict, int]:
    assert process.stdout is not None
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        line = process.stdout.readline()
        if not line:
            break
        message = json.loads(line)
        if message.get("id") == wanted_id:
            return message, len(line.encode())
    raise AssertionError(f"timed out waiting for MCP response {wanted_id}")


def identity_key(result: dict) -> tuple:
    identity = result["identity"]
    return result["status"], result["file"]["size_bytes"], identity["kind"], identity["media_type"], identity["format"]


def main() -> int:
    if len(sys.argv) != 2:
        raise SystemExit("usage: check_agent_economics.py BINARY")
    binary = Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix="file-vitals-economics-") as temporary:
        workspace = Path(temporary)
        fixtures = {
            "01.txt": b"alpha\n",
            "02.json": b'{"value":2}\n',
            "03.csv": b"a,b\n1,2\n",
            "04.svg": b'<svg xmlns="http://www.w3.org/2000/svg"/>',
            "05.wasm": b"\x00asm\x01\x00\x00\x00",
            "06.avro": b"Obj\x01payload",
            "07.npy": b"\x93NUMPY\x01\x00payload",
            "08.bin": b"\x00\x01\x02\x03",
        }
        for name, data in fixtures.items():
            (workspace / name).write_bytes(data)
        environment = os.environ.copy()
        environment["UFI_WORKSPACE_ROOT"] = str(workspace)
        process = subprocess.Popen(
            [str(binary), "mcp"], env=environment, stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1,
        )
        try:
            send(process, {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"economics","version":"1"}}})
            receive(process, 1)
            send(process, {"jsonrpc":"2.0","method":"notifications/initialized"})

            send(process, {"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}})
            catalog_response, catalog_bytes = receive(process, 2)
            tools = catalog_response["result"]["tools"]
            schema_bytes = [
                len(json.dumps(tool["inputSchema"], separators=(",", ":")).encode())
                + len(json.dumps(tool["outputSchema"], separators=(",", ":")).encode())
                for tool in tools
            ]

            single_results = []
            single_bytes = 0
            single_started = time.monotonic()
            for offset, path in enumerate(fixtures, start=10):
                send(process, {"jsonrpc":"2.0","id":offset,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":path,"mode":"quick"}}})
                response, size = receive(process, offset)
                single_results.append(response["result"]["structuredContent"])
                single_bytes += size
            single_ms = round((time.monotonic() - single_started) * 1000, 2)

            batch_started = time.monotonic()
            send(process, {"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"file_inspect_batch","arguments":{"paths":list(fixtures),"mode":"quick"}}})
            batch_response, batch_bytes = receive(process, 30)
            batch_ms = round((time.monotonic() - batch_started) * 1000, 2)
            batch = batch_response["result"]["structuredContent"]
            if batch["status"] != "ok" or len(batch["items"]) != len(single_results):
                raise AssertionError("batch did not return the complete explicit set")
            for index, item in enumerate(batch["items"]):
                if item["index"] != index or item["path"] != list(fixtures)[index]:
                    raise AssertionError("batch lost input correlation")
                if identity_key(item["result"]) != identity_key(single_results[index]):
                    raise AssertionError(f"batch carrier drift for {item['path']}")
            if batch_bytes >= single_bytes:
                raise AssertionError(f"batch response did not reduce carrier bytes: batch={batch_bytes} singles={single_bytes}")
            metrics = {
                "single_tool_calls": len(fixtures),
                "batch_tool_calls": 1,
                "tool_call_reduction_percent": round((1 - 1 / len(fixtures)) * 100, 1),
                "single_response_bytes": single_bytes,
                "batch_response_bytes": batch_bytes,
                "response_byte_reduction_percent": round((1 - batch_bytes / single_bytes) * 100, 1),
                "single_elapsed_ms_observed": single_ms,
                "batch_elapsed_ms_observed": batch_ms,
                "semantic_equivalence": True,
                "tool_catalog_count": len(tools),
                "tool_catalog_response_bytes": catalog_bytes,
                "tool_schema_bytes_total": sum(schema_bytes),
                "largest_tool_schema_bytes": max(schema_bytes),
            }
            print(json.dumps(metrics, separators=(",", ":")))
        finally:
            if process.stdin is not None:
                process.stdin.close()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            if process.returncode != 0:
                stderr = process.stderr.read() if process.stderr else ""
                raise AssertionError(f"MCP economics probe exited {process.returncode}: {stderr}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
