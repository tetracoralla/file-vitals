import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest

from create_release_archive import create_archive
from render_release_provider_manifest import encoded, load, rendered

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/render_release_provider_manifest.py"


class ReleasePackagingTests(unittest.TestCase):
    def test_archive_ignores_timezone_source_times_and_permission_noise(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            bundle = root / "bundle"
            bundle.mkdir()
            (bundle / "runtime").mkdir()
            executable = bundle / "runtime/run"
            executable.write_bytes(b"#!/bin/sh\nexit 0\n")
            document = bundle / "a name\nwith newline.txt"
            document.write_bytes(b"content")
            hashes = []
            for index, zone in enumerate(("UTC", "Asia/Shanghai", "America/Los_Angeles")):
                executable.chmod((0o700, 0o755, 0o775)[index])
                document.chmod((0o600, 0o644, 0o664)[index])
                os.utime(document, (1700000000 + index, 1700000000 + index))
                archive = root / f"{index}.tar.gz"
                subprocess.run(
                    [sys.executable, str(ROOT / "scripts/create_release_archive.py"), str(bundle), str(archive)],
                    env={**os.environ, "TZ": zone}, check=True,
                )
                hashes.append(archive.read_bytes())
            self.assertEqual(hashes[0], hashes[1])
            self.assertEqual(hashes[1], hashes[2])
            self.assertEqual(document.stat().st_mtime, 1700000002)
            with tarfile.open(root / "0.tar.gz") as archive:
                members = archive.getmembers()
                self.assertEqual([m.name for m in members], sorted(m.name for m in members))
                for member in members:
                    self.assertTrue(member.isfile())
                    self.assertEqual((member.uid, member.gid, member.mtime), (0, 0, 946684800))
                self.assertEqual(archive.getmember("bundle/runtime/run").mode, 0o755)
                self.assertEqual(archive.getmember("bundle/a name\nwith newline.txt").mode, 0o644)

    def test_archive_rejects_file_and_directory_links_and_special_files(self):
        for kind in ("file", "directory", "fifo"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                bundle = root / "bundle"
                bundle.mkdir()
                entry = bundle / "entry"
                if kind == "fifo":
                    os.mkfifo(entry)
                else:
                    outside = root / "outside"
                    outside.mkdir() if kind == "directory" else outside.write_text("outside")
                    entry.symlink_to(outside)
                with self.assertRaisesRegex(ValueError, "link or special"):
                    create_archive(bundle, root / "archive.tar.gz")
                self.assertFalse((root / "archive.tar.gz").exists())

    def test_manifest_preserves_semantics_and_rejects_missing_or_invalid_fields(self):
        source = load(ROOT / "capabilities/provider.json")
        result = rendered(source)
        restored = copy.deepcopy(result)
        for key in ("adapter", "transportSchemaProbe"):
            restored["implementations"][0][key] = source["implementations"][0][key]
        self.assertEqual(source, restored)
        for field in ("version", "profileDigest", "annotations", "unknown", "protocol", "homepage"):
            with self.subTest(field=field):
                value = copy.deepcopy(source)
                impl = value["implementations"][0]
                if field == "version":
                    del value["provider"]["version"]
                elif field == "profileDigest":
                    del impl[field]
                elif field == "annotations":
                    impl["bindings"][0][field]["readOnlyHint"] = "true"
                elif field == "unknown":
                    value[field] = True
                elif field == "homepage":
                    value["provider"][field] = "not a URI"
                else:
                    impl["adapter"]["protocol"] = "invalid"
                with self.assertRaisesRegex(SystemExit, "does not satisfy"):
                    rendered(value)

    def test_manifest_check_rejects_symlinked_launcher_or_runtime_directory(self):
        for link_directory in (False, True):
            with self.subTest(link_directory=link_directory), tempfile.TemporaryDirectory() as temporary:
                bundle = Path(temporary) / "bundle"
                (bundle / "capabilities").mkdir(parents=True)
                outside = Path(temporary) / "outside"
                outside.mkdir()
                runtime = bundle / "runtime"
                runtime.symlink_to(outside) if link_directory else runtime.mkdir()
                for name in ("file-vitals-capability", "file-vitals-transport-schema-probe"):
                    binary = outside / name
                    binary.write_text("#!/bin/sh\nexit 0\n")
                    binary.chmod(0o755)
                    if not link_directory:
                        (runtime / name).symlink_to(binary)
                target = bundle / "capabilities/provider.json"
                target.write_text(encoded(rendered(load(ROOT / "capabilities/provider.json"))))
                result = subprocess.run(
                    [sys.executable, str(SCRIPT), "--check", str(ROOT / "capabilities/provider.json"), str(target)],
                    capture_output=True, text=True,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("symlink", result.stderr)


if __name__ == "__main__":
    unittest.main()
