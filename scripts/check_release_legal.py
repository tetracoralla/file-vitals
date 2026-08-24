#!/usr/bin/env python3
"""Fail closed when source, plugin, and archive legal inventories diverge."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile


ROOT = Path(__file__).resolve().parents[1]
APACHE_LICENSE_SHA256 = "e0df01efd2e41f704e05eb4f8ecb87e7e5230804ca9814d2f91b6af69815e892"
REQUIRED_LEGAL_FILES = ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md")


def fail(message: str) -> None:
    raise SystemExit(f"release legal check failed: {message}")


def require_apache_manifest(raw: bytes, source: str) -> None:
    try:
        manifest = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        fail(f"invalid plugin manifest in {source}: {exc}")
    if manifest.get("license") != "Apache-2.0":
        fail(f"{source} does not declare Apache-2.0")


def source_legal_files() -> tuple[str, ...]:
    third_party_dir = ROOT / "third_party_licenses"
    if not third_party_dir.is_dir():
        fail("third_party_licenses directory is missing")
    third_party = tuple(
        f"third_party_licenses/{path.name}"
        for path in sorted(third_party_dir.iterdir())
        if path.is_file()
    )
    if not third_party:
        fail("third_party_licenses directory is empty")
    return REQUIRED_LEGAL_FILES + third_party


def main() -> None:
    if len(sys.argv) != 2:
        fail("usage: check_release_legal.py BUNDLE_DIRECTORY")

    bundle = Path(sys.argv[1]).resolve()
    if not bundle.is_dir():
        fail(f"bundle directory is missing: {bundle}")

    license_bytes = (ROOT / "LICENSE").read_bytes()
    if hashlib.sha256(license_bytes).hexdigest() != APACHE_LICENSE_SHA256:
        fail("root LICENSE is not the approved Apache-2.0 text")

    legal_files = source_legal_files()
    for relative in legal_files:
        source = (ROOT / relative).read_bytes()
        packaged_path = bundle / relative
        if not packaged_path.is_file():
            fail(f"bundle is missing {relative}")
        if packaged_path.read_bytes() != source:
            fail(f"bundle copy differs from source: {relative}")

    require_apache_manifest(
        (ROOT / ".codex-plugin/plugin.json").read_bytes(), "source plugin manifest"
    )
    require_apache_manifest(
        (bundle / ".codex-plugin/plugin.json").read_bytes(), "bundled plugin manifest"
    )

    archive = Path(f"{bundle}.tar.gz")
    if not archive.is_file():
        fail(f"archive is missing: {archive}")

    prefix = f"{bundle.name}/"
    expected_archive_files = legal_files + (".codex-plugin/plugin.json",)
    with tarfile.open(archive, "r:gz") as handle:
        members = {member.name: member for member in handle.getmembers()}
        for member in members.values():
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts:
                fail(f"unsafe archive path: {member.name}")
            if member.issym() or member.islnk():
                fail(f"archive contains a link: {member.name}")

        for relative in expected_archive_files:
            name = f"{prefix}{relative}"
            member = members.get(name)
            if member is None or not member.isfile():
                fail(f"archive is missing {relative}")
            extracted = handle.extractfile(member)
            if extracted is None:
                fail(f"archive member is unreadable: {relative}")
            data = extracted.read()
            if relative == ".codex-plugin/plugin.json":
                require_apache_manifest(data, "archived plugin manifest")
            elif data != (ROOT / relative).read_bytes():
                fail(f"archive copy differs from source: {relative}")

    print("source, plugin bundle, and archive legal inventories: ok")


if __name__ == "__main__":
    main()
