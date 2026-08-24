#!/usr/bin/env python3
"""Generate collection schemas from the canonical single-file result schema."""

import argparse
import copy
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "schemas" / "inspection-result.schema.json"
TARGET = ROOT / "schemas" / "file-inspect-batch-result.schema.json"


def build_batch_schema() -> dict:
    inspection = json.loads(SOURCE.read_text())
    inspection_result = {
        key: copy.deepcopy(inspection[key])
        for key in ("type", "additionalProperties", "required", "properties")
    }
    definitions = copy.deepcopy(inspection["$defs"])
    definitions["inspectionResult"] = inspection_result
    definitions["error"] = copy.deepcopy(inspection["$defs"]["error"])
    definitions["diagnostic"] = copy.deepcopy(inspection["$defs"]["diagnostic"])
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "https://file-vitals.local/schema/file-inspect-batch-result-1.0.json",
        "title": "File Vitals batch result",
        "type": "object",
        "additionalProperties": False,
        "required": ["schema_version", "status", "items", "diagnostics", "limits"],
        "properties": {
            "schema_version": {"const": "1.0"},
            "status": {"enum": ["ok", "partial", "error"]},
            "items": {
                "type": "array",
                "maxItems": 16,
                "items": {
                    "type": "object",
                    "additionalProperties": False,
                    "required": ["index", "path", "result"],
                    "properties": {
                        "index": {"type": "integer", "minimum": 0, "maximum": 15},
                        "path": {"type": "string", "maxLength": 4096},
                        "result": {"$ref": "#/$defs/inspectionResult"},
                    },
                },
            },
            "diagnostics": {
                "type": "array",
                "maxItems": 64,
                "items": {"$ref": "#/$defs/diagnostic"},
            },
            "limits": {"$ref": "#/$defs/collectionLimits"},
            "error": {"$ref": "#/$defs/error"},
        },
        "$defs": {
            **definitions,
            "collectionLimits": {
                "type": "object",
                "additionalProperties": False,
                "required": ["item_max", "response_bytes_max", "timeout_ms", "memory_bytes_max"],
                "properties": {
                    "item_max": {"const": 16},
                    "response_bytes_max": {"const": 196608},
                    "timeout_ms": {"type": "integer", "minimum": 1},
                    "memory_bytes_max": {"type": "integer", "minimum": 1},
                    "truncated": {"type": "boolean"},
                },
            },
        },
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    rendered = json.dumps(build_batch_schema(), indent=2, ensure_ascii=False) + "\n"
    if args.check:
        if not TARGET.exists() or TARGET.read_text() != rendered:
            raise SystemExit(f"generated schema is stale: {TARGET}")
        return 0
    TARGET.write_text(rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
