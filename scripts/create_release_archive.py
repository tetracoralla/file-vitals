#!/usr/bin/env python3
"""Write a portable, deterministic archive of a prepared plugin bundle."""

import gzip
import os
from pathlib import Path
import stat
import sys
import tarfile


def create_archive(bundle: Path, destination: Path) -> None:
    if bundle.is_symlink() or not bundle.is_dir():
        raise ValueError("bundle must be a real directory")
    files = []
    for root, directories, names in os.walk(bundle, followlinks=False):
        for name in directories + names:
            path = Path(root) / name
            mode = path.lstat().st_mode
            if stat.S_ISREG(mode):
                files.append(path)
            elif not stat.S_ISDIR(mode):
                raise ValueError(f"bundle contains a link or special entry: {path}")
    # Construct headers explicitly: no host uid, timezone, umask, xattrs or
    # source-path metadata enter the archive. The source bundle is unchanged.
    with destination.open("xb") as output:
        with gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for path in sorted(files, key=lambda item: item.relative_to(bundle.parent).as_posix()):
                    with path.open("rb") as source:
                        metadata = os.fstat(source.fileno())
                        info = tarfile.TarInfo(path.relative_to(bundle.parent).as_posix())
                        info.size = metadata.st_size
                        info.mode = 0o755 if metadata.st_mode & 0o111 else 0o644
                        info.uid = info.gid = 0
                        info.uname, info.gname = "root", "wheel"
                        info.mtime = 946684800  # 2000-01-01 00:00:00 UTC
                        archive.addfile(info, source)


if __name__ == "__main__":
    try:
        if len(sys.argv) != 3:
            raise ValueError("usage: create_release_archive.py BUNDLE DESTINATION")
        create_archive(Path(sys.argv[1]), Path(sys.argv[2]))
    except (OSError, ValueError, tarfile.TarError) as error:
        raise SystemExit(f"release archive error: {error}") from error
