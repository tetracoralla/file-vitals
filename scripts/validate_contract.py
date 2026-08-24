#!/usr/bin/env python3
import json
import pathlib
import subprocess
import sys

import jsonschema


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: validate_contract.py BINARY REPO_ROOT")
    binary = pathlib.Path(sys.argv[1]).resolve()
    root = pathlib.Path(sys.argv[2]).resolve()
    input_schema = json.loads((root / "schemas/file-inspect-input.schema.json").read_text())
    output_schema = json.loads((root / "schemas/inspection-result.schema.json").read_text())
    jsonschema.Draft202012Validator.check_schema(input_schema)
    jsonschema.Draft202012Validator.check_schema(output_schema)
    input_validator = jsonschema.Draft202012Validator(input_schema)
    input_validator.validate({"path": "go.mod", "mode": "standard", "hash": "none"})
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
    print("contract schemas and representative result: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
