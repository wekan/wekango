#!/usr/bin/env python3
"""Import the pinned FerretDB runtime from Git objects, never working-tree files."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tempfile

REPOSITORY = "https://github.com/wekan/FerretDB"
MANIFEST = "PROVENANCE.json"


def git(source, *args):
    return subprocess.check_output(["git", "-C", str(source), *args])


def selected(name):
    path = PurePosixPath(name)
    return (
        name in {"go.mod", "go.sum", "LICENSE", "NOTICE"}
        or name.startswith(("ferretdb/", "internal/", "build/version/"))
    ) and path.name.lower() not in {"agents.md", "claude.md"}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inventory(destination):
    files = {}
    for path in destination.rglob("*"):
        if path.is_symlink():
            raise ValueError(f"Refusing symlink in existing import: {path}")
        if path.is_file() and path != destination / MANIFEST:
            files[path.relative_to(destination).as_posix()] = digest(path.read_bytes())
        elif not path.is_file() and not path.is_dir():
            raise ValueError(f"Unexpected file type: {path}")
    return files


def verify_existing(destination):
    if destination.is_symlink() or not destination.is_dir():
        raise ValueError("Existing destination must be an imported directory")
    manifest = json.loads((destination / MANIFEST).read_text())
    if (manifest.get("repository") != REPOSITORY
            or not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", manifest.get("commit", ""))
            or not isinstance(manifest.get("files"), dict)):
        raise ValueError("Invalid existing provenance")
    expected_dirs = {parent.as_posix() for name in manifest["files"]
                     for parent in PurePosixPath(name).parents if parent != PurePosixPath(".")}
    actual_dirs = {p.relative_to(destination).as_posix()
                   for p in destination.rglob("*") if p.is_dir()}
    if actual_dirs != expected_dirs or inventory(destination) != manifest["files"]:
        raise ValueError("Existing import contains edited, missing or unknown files")


def sync(source, commit, destination):
    if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", commit):
        raise ValueError("Supply an exact full lowercase Git commit hash")
    resolved = git(source, "rev-parse", "--verify", commit + "^{commit}").decode().strip()
    if resolved != commit:
        raise ValueError("Requested object is not the exact commit")
    entries = git(source, "ls-tree", "-r", "-z", commit).split(b"\0")
    files = {}
    for entry in entries:
        if not entry:
            continue
        metadata, raw_name = entry.split(b"\t", 1)
        name = raw_name.decode("utf-8")
        if not selected(name):
            continue
        mode, kind, oid = metadata.decode().split()
        if kind != "blob" or mode not in {"100644", "100755"}:
            raise ValueError(f"Unsupported selected Git entry: {name}")
        if name == MANIFEST or PurePosixPath(name).is_absolute() or ".." in PurePosixPath(name).parts:
            raise ValueError(f"Unsafe selected path: {name}")
        files[name] = (git(source, "cat-file", "blob", oid), mode)
    if not {"go.mod", "go.sum", "LICENSE"}.issubset(files):
        raise ValueError("Source commit lacks required module/license files")
    destination = Path(destination)
    if destination.exists() or destination.is_symlink():
        verify_existing(destination)
    temporary = os.environ.get("TMPDIR")
    if not temporary or not Path(temporary).is_dir():
        raise ValueError("TMPDIR must name an existing task temporary directory")
    manifest = {"repository": REPOSITORY, "commit": resolved,
                "files": {name: digest(data) for name, (data, _) in sorted(files.items())}}
    # All source reads and validation finish before the existing import changes.
    with tempfile.TemporaryDirectory(prefix="sync-ferretdb-", dir=temporary) as workspace:
        staged = Path(workspace) / "runtime"
        staged.mkdir()
        for name, (data, mode) in files.items():
            target = staged / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            target.chmod(0o755 if mode == "100755" else 0o644)
        (staged / MANIFEST).write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        destination.parent.mkdir(parents=True, exist_ok=True)
        backup = Path(workspace) / "previous"
        if destination.exists():
            verify_existing(destination)
            destination.rename(backup)
        try:
            staged.rename(destination)
        except BaseException:
            if backup.exists():
                backup.rename(destination)
            raise
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path, nargs="?")
    parser.add_argument("commit", nargs="?")
    parser.add_argument("--verify", action="store_true",
                        help="Verify the existing import offline, without a source checkout")
    parser.add_argument("--destination", type=Path,
                        default=Path(__file__).resolve().parents[1] / "internal/compat/ferretdb")
    args = parser.parse_args()
    if args.verify:
        if args.source is not None or args.commit is not None:
            parser.error("--verify does not accept a source or commit")
        verify_existing(args.destination)
        print(f"Verified {args.destination / MANIFEST}")
        return
    if args.source is None or args.commit is None:
        parser.error("source checkout and full commit hash are required")
    manifest = sync(args.source, args.commit, args.destination)
    print(f"Imported {len(manifest['files'])} files from {manifest['commit']}")


if __name__ == "__main__":
    main()
