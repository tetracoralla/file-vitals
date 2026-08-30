#!/usr/bin/env python3
"""Verify the staged macOS app without treating signing as complete."""

from __future__ import annotations

import json
from pathlib import Path
import plistlib
import stat
import subprocess
import sys
from typing import NoReturn


ROOT = Path(__file__).resolve().parents[1]


def fail(message: str) -> NoReturn:
    raise SystemExit(f"app bundle check failed: {message}")


def main() -> None:
    if len(sys.argv) != 2:
        fail("usage: check_app_bundle.py APP_BUNDLE")
    supplied = Path(sys.argv[1]).expanduser()
    bundle = supplied.absolute() if supplied.is_absolute() else (Path.cwd() / supplied).absolute()
    contents = bundle / "Contents"
    app_binary = contents / "MacOS" / "FileVitals"
    engine = contents / "Resources" / "runtime" / "finspect"
    licenses = contents / "Resources" / "licenses"

    for path in (bundle, contents, app_binary, engine, licenses):
        if path.is_symlink():
            fail(f"symlink is not allowed: {path}")
    for path in bundle.rglob("*"):
        if path.is_symlink():
            fail(f"bundle contains a symlink: {path.relative_to(bundle)}")

    for executable in (app_binary, engine):
        if not executable.is_file() or not executable.stat().st_mode & stat.S_IXUSR:
            fail(f"executable is missing: {executable}")

    with (contents / "Info.plist").open("rb") as handle:
        info = plistlib.load(handle)
    expected = {
        "CFBundleExecutable": "FileVitals",
        "CFBundleIdentifier": "org.openadam.file-vitals",
        "CFBundleName": "File Vitals",
        "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": "0.3.3",
        "LSMinimumSystemVersion": "14.0",
    }
    for key, value in expected.items():
        if info.get(key) != value:
            fail(f"Info.plist {key} is {info.get(key)!r}, expected {value!r}")

    legal_files = ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"]
    legal_files.extend(
        f"third_party_licenses/{path.name}"
        for path in sorted((ROOT / "third_party_licenses").iterdir())
        if path.is_file()
    )
    for relative in legal_files:
        source = ROOT / relative
        packaged = licenses / relative
        if not packaged.is_file() or packaged.read_bytes() != source.read_bytes():
            fail(f"legal file is missing or changed: {relative}")

    def run_engine(arguments: list[str]) -> subprocess.CompletedProcess[str]:
        try:
            return subprocess.run(
                arguments,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                timeout=15,
            )
        except subprocess.CalledProcessError as error:
            fail(f"bundled engine failed for {arguments[:2]}: {(error.stderr or '').strip()}")
        except subprocess.TimeoutExpired:
            fail(f"bundled engine timed out for {arguments[:2]}")

    doctor = run_engine([str(engine), "doctor"])
    for required_probe in ("internal-signatures: available", "result-schema: available", "worker-self-test: available"):
        if required_probe not in doctor.stdout:
            fail(f"bundled engine doctor is missing {required_probe!r}")
    inspected = run_engine([str(engine), str(ROOT / "README.md"), "--quick", "--json"])
    result = json.loads(inspected.stdout)
    if result.get("status") != "ok" or result.get("file", {}).get("name") != "README.md":
        fail("bundled engine did not complete the representative inspection")

    print("macOS app structure, legal inventory, and bundled engine: ok")


if __name__ == "__main__":
    main()
