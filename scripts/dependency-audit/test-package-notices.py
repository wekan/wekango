#!/usr/bin/env python3
"""Exercise corresponding-source packaging without downloading test dependencies."""
import hashlib
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('package-notices.py')


class NoticesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='wekango-notices-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'native/notices/example.org/module').mkdir(parents=True)
        (self.root / 'native/notices/example.org/module/LICENSE').write_text('Mozilla Public License Version 2.0\n')
        (self.root / 'native/licenses.csv').write_text('example.org/module/package,https://example.org/license,MPL-2.0\nexample.org/module/other,https://example.org/license,MPL-2.0\n')
        (self.root / 'modules.json').write_text(json.dumps({'Path': 'example.org/module', 'Version': 'v1.2.3'}))
        archive = self.root / 'upstream.zip'
        archive.write_bytes(b'fixture module archive')
        (self.root / 'bin').mkdir()
        fake_go = self.root / 'bin/go'
        fake_go.write_text('#!' + sys.executable + '\nimport json\nprint(' + repr(json.dumps({'Zip': str(archive), 'Sum': 'h1:test'})) + ')\n')
        fake_go.chmod(0o755)

    def run_packager(self):
        env = dict(os.environ, PATH=str(self.root / 'bin') + os.pathsep + os.environ['PATH'])
        return subprocess.run([sys.executable, str(getattr(self, 'script', SCRIPT)), str(self.root)], env=env, text=True, capture_output=True)

    def test_exact_mpl_archive_and_complete_notices(self):
        result = self.run_packager()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.root / 'sources/manifest.json').read_text())
        self.assertEqual(list(manifest), ['example.org/module@v1.2.3'])
        source = manifest['example.org/module@v1.2.3']
        copied = self.root / source['archive']
        self.assertEqual(copied.read_bytes(), b'fixture module archive')
        self.assertEqual(source['sha256'], hashlib.sha256(copied.read_bytes()).hexdigest())
        self.assertIn('Mozilla Public License Version 2.0', (self.root / 'THIRD_PARTY_NOTICES.md').read_text())

    def test_local_replacement_cannot_claim_unmodified_upstream_source(self):
        (self.root / 'modules.json').write_text(json.dumps({'Path': 'example.org/module', 'Version': 'v1.2.3', 'Replace': {'Path': '../patched-module'}}))
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('local replacement needs', result.stderr)
        self.assertFalse((self.root / 'THIRD_PARTY_NOTICES.md').exists())

    def test_changed_lucent_notice_requires_review(self):
        altered = self.root / 'altered-license'
        altered.write_text('GPL license instead of the reviewed Lucent permission')
        saved = self.root / 'saved-license'
        result = subprocess.run([sys.executable, str(SCRIPT.with_name('save-lucent.py')), str(altered), str(saved)], text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Lucent notice changed', result.stderr)
        self.assertFalse(saved.exists())

    def test_owned_facade_uses_project_source_url(self):
        repo = SCRIPT.resolve().parents[2]
        module = {'Path':'gopkg.in/yaml.v2','Version':'v2.4.0',
                  'Replace':{'Path':'./internal/compat/yamlv2','Dir':str(repo/'internal/compat/yamlv2')}}
        with (self.root/'modules.json').open('a') as stream:
            stream.write('\n'+json.dumps(module))
        with (self.root/'native/licenses.csv').open('a') as stream:
            stream.write('gopkg.in/yaml.v2,Unknown,MIT\n')
        result = self.run_packager()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('github.com/wekan/wekango/blob/HEAD/internal/compat/yamlv2/LICENSE,MIT',
                      (self.root/'licenses.csv').read_text())
        module['Replace']['Dir'] = str(self.root)
        (self.root/'modules.json').write_text(json.dumps(module))
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Unexpected local license source', result.stderr)

    def prepare_local_ferretdb(self, license_id='Apache-2.0'):
        # Isolate the repository layout so path/license guard tests neither depend
        # on nor modify the runtime copy another task is preparing.
        repo = self.root / 'fixture-repo'
        self.script = repo / 'scripts/dependency-audit/package-notices.py'
        self.script.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, self.script)
        source = repo / 'internal/compat/ferretdb'
        source.mkdir(parents=True)
        (source / 'LICENSE').write_text('Apache License\nVersion 2.0, January 2004\n')
        module = {'Path': 'github.com/FerretDB/FerretDB',
                  'Replace': {'Path': './internal/compat/ferretdb', 'Dir': str(source)}}
        (self.root / 'modules.json').write_text(json.dumps(module))
        (self.root / 'native/licenses.csv').write_text(
            f'github.com/FerretDB/FerretDB,Unknown,{license_id}\n')
        notice = self.root / 'native/notices/github.com/FerretDB/FerretDB/LICENSE'
        notice.parent.mkdir(parents=True)
        shutil.copyfile(source / 'LICENSE', notice)
        return module, source

    def test_local_ferretdb_retains_apache_license_and_actual_source_url(self):
        self.prepare_local_ferretdb()
        result = self.run_packager()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('github.com/wekan/wekango/blob/HEAD/internal/compat/ferretdb/LICENSE,Apache-2.0',
                      (self.root / 'licenses.csv').read_text())
        self.assertIn('Apache License\nVersion 2.0',
                      (self.root / 'THIRD_PARTY_NOTICES.md').read_text())

    def test_local_ferretdb_wrong_license_fails_closed(self):
        self.prepare_local_ferretdb('MIT')
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Local compatibility module license changed', result.stderr)
        self.assertFalse((self.root / 'THIRD_PARTY_NOTICES.md').exists())

    def test_local_ferretdb_missing_license_fails_closed(self):
        _, source = self.prepare_local_ferretdb()
        (source / 'LICENSE').unlink()
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Unexpected local license source', result.stderr)

    def test_local_ferretdb_wrong_module_or_directory_fails_closed(self):
        module, _ = self.prepare_local_ferretdb()
        for replacement in [
            dict(module, Path='example.org/not-ferretdb'),
            dict(module, Replace=dict(module['Replace'], Dir=str(self.root))),
        ]:
            with self.subTest(module=replacement):
                (self.root / 'modules.json').write_text(json.dumps(replacement))
                result = self.run_packager()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Unexpected local license source', result.stderr)

    def test_missing_inventory_fails_closed(self):
        (self.root / 'native/licenses.csv').unlink()
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('No target license inventory', result.stderr)


if __name__ == '__main__':
    unittest.main()
