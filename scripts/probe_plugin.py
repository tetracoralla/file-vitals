#!/usr/bin/env python3
import json
import os
import pathlib
import queue
import subprocess
import sys
import tempfile
import threading
import time


MAX_ENVELOPE = 256 * 1024


def main() -> int:
    if len(sys.argv) != 2:
        raise SystemExit("usage: probe_plugin.py PLUGIN_DIR")
    plugin = pathlib.Path(sys.argv[1]).resolve()
    config = json.loads((plugin / ".mcp.json").read_text())["mcpServers"]["file-vitals"]
    command = (plugin / config["command"]).resolve()
    if not command.is_file():
        raise AssertionError(f"bundled command is missing: {command}")

    with tempfile.TemporaryDirectory(prefix="ufi-probe-") as temporary:
        workspace = pathlib.Path(temporary)
        (workspace / "sample.json").write_text('{"value":1}\n', encoding="utf-8")
        (workspace / "unknown.jpg").write_bytes(b"\x00\x01\x02\x03")
        os.symlink(workspace / "sample.json", workspace / "sample-link")
        with (workspace / "large.bin").open("wb") as handle:
            handle.truncate(1024 * 1024 * 1024)

        environment = os.environ.copy()
        environment["UFI_WORKSPACE_ROOT"] = str(workspace)
        process = subprocess.Popen(
            [str(command), *config.get("args", [])],
            cwd=plugin,
            env=environment,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        messages: queue.Queue[tuple[dict, int]] = queue.Queue()

        def read_messages() -> None:
            assert process.stdout is not None
            for line in process.stdout:
                size = len(line.encode())
                messages.put((json.loads(line), size))

        threading.Thread(target=read_messages, daemon=True).start()

        def send(value: dict) -> None:
            assert process.stdin is not None
            process.stdin.write(json.dumps(value, separators=(",", ":")) + "\n")
            process.stdin.flush()

        def receive(expected_id: int, timeout: float = 8.0) -> dict:
            deadline = time.monotonic() + timeout
            deferred: list[tuple[dict, int]] = []
            while time.monotonic() < deadline:
                try:
                    message, size = messages.get(timeout=max(0.01, deadline - time.monotonic()))
                except queue.Empty:
                    break
                if size > MAX_ENVELOPE:
                    raise AssertionError(f"MCP envelope exceeds limit: {size}")
                if message.get("id") == expected_id:
                    for item in deferred:
                        messages.put(item)
                    return message
                deferred.append((message, size))
            raise AssertionError(f"timed out waiting for response {expected_id}")

        send({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}})
        initialized = receive(1)
        assert initialized["result"]["protocolVersion"] == "2025-11-25"
        send({"jsonrpc":"2.0","method":"notifications/initialized"})
        send({"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}})
        tools = receive(2)["result"]["tools"]
        assert [tool["name"] for tool in tools] == ["file_inspect"]
        assert tools[0]["inputSchema"]["additionalProperties"] is False
        assert tools[0]["annotations"] == {"title":"Inspect file","readOnlyHint":True,"destructiveHint":False,"idempotentHint":True,"openWorldHint":False}

        send({"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"sample.json"}}})
        valid = receive(3)["result"]
        assert valid["isError"] is False
        assert valid["structuredContent"]["identity"]["media_type"] == "application/json"

        send({"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"sample.json","mdoe":"quick"}}})
        assert receive(4)["result"]["structuredContent"]["error"]["code"] == "E_INVALID_INPUT"
        send({"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"sample-link"}}})
        assert receive(5)["result"]["structuredContent"]["error"]["code"] == "E_PATH_SYMLINK"
        send({"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":str(workspace / "sample.json")}}})
        assert receive(6)["result"]["structuredContent"]["error"]["code"] == "E_PATH_ABSOLUTE"

        send({"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"large.bin","hash":"sha256","mode":"quick"}}})
        time.sleep(0.05)
        send({"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"runtime cancellation probe"}})
        send({"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"sample.json","mode":"quick"}}})
        cancelled = receive(7, timeout=10)["result"]["structuredContent"]
        recovered = receive(8, timeout=10)["result"]["structuredContent"]
        assert cancelled["error"]["code"] == "E_CANCELLED", cancelled
        assert recovered["status"] == "ok", recovered

        modern_meta = {
            "io.modelcontextprotocol/protocolVersion": "2026-07-28",
            "io.modelcontextprotocol/clientInfo": {"name": "probe", "version": "1"},
            "io.modelcontextprotocol/clientCapabilities": {},
        }
        send({"jsonrpc":"2.0","id":9,"method":"server/discover","params":{"_meta":modern_meta}})
        discovered = receive(9)["result"]
        assert discovered["resultType"] == "complete"
        assert discovered["supportedVersions"] == ["2026-07-28"]
        assert discovered["_meta"]["io.modelcontextprotocol/serverInfo"]["name"] == "file-vitals"
        send({"jsonrpc":"2.0","id":10,"method":"tools/list","params":{"_meta":modern_meta}})
        modern_tools = receive(10)["result"]
        assert modern_tools["resultType"] == "complete"
        assert [tool["name"] for tool in modern_tools["tools"]] == ["file_inspect"]
        send({"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"sample.json","mode":"quick"},"_meta":modern_meta}})
        modern_call = receive(11)["result"]
        assert modern_call["resultType"] == "complete"
        assert modern_call["structuredContent"]["status"] == "ok"

        assert process.stdin is not None
        process.stdin.close()
        process.wait(timeout=5)
        if process.returncode != 0:
            stderr = process.stderr.read() if process.stderr else ""
            raise AssertionError(f"MCP server exited {process.returncode}: {stderr}")

    print("bundled CLI/MCP legacy and modern activation, guards, cancellation, and recovery: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
