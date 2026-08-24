#!/usr/bin/env python3
import json
import hashlib
import pathlib
import subprocess
import sys

import jsonschema


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: validate_contract.py BINARY REPO_ROOT")
    binary = pathlib.Path(sys.argv[1]).resolve()
    root = pathlib.Path(sys.argv[2]).resolve()
    documents = {
        "input": json.loads((root / "schemas/file-inspect-input.schema.json").read_text()),
        "output": json.loads((root / "schemas/inspection-result.schema.json").read_text()),
        "batch_input": json.loads((root / "schemas/file-inspect-batch-input.schema.json").read_text()),
        "batch_output": json.loads((root / "schemas/file-inspect-batch-result.schema.json").read_text()),
        "inventory_input": json.loads((root / "schemas/workspace-inventory-input.schema.json").read_text()),
        "inventory_output": json.loads((root / "schemas/workspace-inventory-result.schema.json").read_text()),
    }
    for schema in documents.values():
        jsonschema.Draft202012Validator.check_schema(schema)
    input_schema = documents["input"]
    output_schema = documents["output"]
    input_validator = jsonschema.Draft202012Validator(input_schema)
    input_validator.validate({"path": "go.mod", "mode": "standard", "hash": "none"})
    jsonschema.Draft202012Validator(documents["batch_input"]).validate({"paths": ["go.mod", "README.md"], "mode": "quick"})
    jsonschema.Draft202012Validator(documents["inventory_input"]).validate({"path": "schemas", "max_depth": 0})
    completed = subprocess.run(
        [str(binary), str(root / "go.mod"), "--quick", "--json"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=15,
    )
    result = json.loads(completed.stdout)
    jsonschema.Draft202012Validator(output_schema).validate(result)
    if len(json.dumps(result, separators=(",", ":")).encode()) > 256 * 1024:
        raise AssertionError("serialized result exceeds byte budget")
    expected = hashlib.sha256((root / "go.mod").read_bytes()).hexdigest()
    verified = subprocess.run(
        [str(binary), str(root / "go.mod"), "--quick", "--expect-sha256", expected.upper(), "--json"],
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=15,
    )
    verified_result = json.loads(verified.stdout)
    jsonschema.Draft202012Validator(output_schema).validate(verified_result)
    if verified_result.get("integrity", {}).get("sha256_matches") is not True:
        raise AssertionError("expected SHA-256 was not verified explicitly")
    missing = subprocess.run(
        [str(binary), str(root / "does-not-exist.bin"), "--json"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=15,
    )
    if missing.returncode != 1:
        raise AssertionError(f"missing-file exit code is {missing.returncode}")
    missing_result = json.loads(missing.stdout)
    jsonschema.Draft202012Validator(output_schema).validate(missing_result)
    if missing_result.get("error", {}).get("code") != "E_FILE_NOT_FOUND":
        raise AssertionError("missing-file JSON did not preserve the stable error contract")
    batch = subprocess.run(
        [str(binary), "batch", str(root / "go.mod"), str(root / "does-not-exist.bin"), "--quick", "--json"],
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=15,
    )
    batch_result = json.loads(batch.stdout)
    jsonschema.Draft202012Validator(documents["batch_output"]).validate(batch_result)
    if batch_result.get("status") != "partial" or [item.get("index") for item in batch_result.get("items", [])] != [0, 1]:
        raise AssertionError("batch output lost partial status or item correlation")
    if len(json.dumps(batch_result, separators=(",", ":")).encode()) > 192 * 1024:
        raise AssertionError("serialized batch result exceeds byte budget")
    inventory = subprocess.run(
        [str(binary), "inventory", str(root / "schemas"), "--max-depth", "0", "--json"],
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=15,
    )
    inventory_result = json.loads(inventory.stdout)
    jsonschema.Draft202012Validator(documents["inventory_output"]).validate(inventory_result)
    if inventory_result.get("files_scanned", 0) < 6:
        raise AssertionError("inventory did not inspect the published schemas")
    if len(json.dumps(inventory_result, separators=(",", ":")).encode()) > 192 * 1024:
        raise AssertionError("serialized inventory result exceeds byte budget")
    print("single, batch, and inventory schemas and representative results: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
