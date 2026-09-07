#!/usr/bin/env python3
"""Small Git fixtures exercise pinned imports without copying the real runtime."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

# Keep importlib bytecode out of the source tree; temporary files use TMPDIR.
sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location("sync_ferretdb", Path(__file__).with_name("sync-ferretdb.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class SyncTests(unittest.TestCase):
    def setUp(self):
        if not os.environ.get("TMPDIR"):
            self.fail("Set TMPDIR to the repository task temporary directory")
        self.temp = tempfile.TemporaryDirectory(dir=os.environ["TMPDIR"])
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        self.destination = self.root / "import"
        self.git("init", "-q")
        self.git("config", "user.name", "Test Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.tracked = {"go.mod": "module fixture\n", "go.sum": "", "LICENSE": "license\n", "NOTICE": "copyright notice\n",
                        "ferretdb/main.go": "package ferretdb\n", "internal/data/embed.txt": "embedded\n",
                        "internal/data/data_test.go": "package data\n", "build/version/version.txt": "1\n"}
        for name, content in {**self.tracked, "README.md": "excluded", ".github/workflows/build.yml": "excluded",
                              "internal/AGENTS.md": "excluded", "ferretdb/CLAUDE.md": "excluded"}.items():
            p = self.source / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(content)
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        self.commit = self.git("rev-parse", "HEAD").strip()

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.source), *args], text=True)

    def sync(self, commit=None):
        return module.sync(self.source, commit or self.commit, self.destination)

    def test_selection_dirty_checkout_and_repeat(self):
        (self.source / "go.mod").write_text("DIRTY")
        (self.source / "internal/untracked.txt").write_text("UNTRACKED")
        manifest = self.sync()
        self.assertEqual(set(manifest["files"]), set(self.tracked))
        self.assertEqual(manifest["commit"], self.commit)
        self.assertEqual(manifest["repository"], module.REPOSITORY)
        for name, content in self.tracked.items():
            self.assertEqual((self.destination / name).read_text(), content)
            self.assertEqual(manifest["files"][name], module.digest(content.encode()))
        self.assertEqual(self.sync(), manifest)

    def test_new_commit_replaces_verified_import(self):
        self.sync()
        (self.source / "internal/new.txt").write_text("new")
        self.git("add", ".")
        self.git("commit", "-qm", "next")
        commit = self.git("rev-parse", "HEAD").strip()
        self.assertEqual(self.sync(commit)["commit"], commit)
        self.assertEqual((self.destination / "internal/new.txt").read_text(), "new")

    def test_tampering_unknown_files_and_directories_rejected(self):
        for name, kind in [("go.mod", "edit"), ("unknown.txt", "edit"),
                           ("internal/PROVENANCE.json", "edit"), ("empty", "directory")]:
            with self.subTest(name=name):
                self.sync()
                path = self.destination / name
                previous = path.read_bytes() if path.exists() else None
                if kind == "directory":
                    path.mkdir()
                else:
                    path.write_text("user changes")
                with self.assertRaises(ValueError):
                    self.sync()
                self.assertTrue(path.exists())
                if previous is not None:
                    path.write_bytes(previous)
                elif kind == "directory":
                    path.rmdir()
                else:
                    path.unlink()

    def test_short_ref_and_unmanaged_destination_rejected(self):
        for ref in ["HEAD", self.commit[:12]]:
            with self.assertRaises(ValueError):
                self.sync(ref)
        self.destination.mkdir()
        (self.destination / "keep").write_text("mine")
        with self.assertRaises(FileNotFoundError):
            self.sync()
        self.assertEqual((self.destination / "keep").read_text(), "mine")

    def test_offline_verify_checks_missing_and_unknown_files(self):
        self.sync()
        command = [sys.executable, str(Path(module.__file__)), "--verify",
                   "--destination", str(self.destination)]
        result = subprocess.run(command, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        (self.destination / "LICENSE").unlink()
        self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)
        (self.destination / "LICENSE").write_text(self.tracked["LICENSE"])
        (self.destination / "extra").write_text("unknown")
        self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)

    def test_selected_symlink_rejected(self):
        (self.source / "internal/link").symlink_to("../../outside")
        self.git("add", ".")
        self.git("commit", "-qm", "symlink")
        with self.assertRaises(ValueError):
            self.sync(self.git("rev-parse", "HEAD").strip())
        self.assertFalse(self.destination.exists())


if __name__ == "__main__":
    unittest.main()
