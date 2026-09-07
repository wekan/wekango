#!/usr/bin/env python3
"""Exercise exact notice approval and packaging using the pinned module cache."""
import csv
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True

SCRIPT = Path(__file__).with_name('save-mongo-tools.py').resolve()
spec = importlib.util.spec_from_file_location('mongo_notice', SCRIPT)
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class MongoNoticesTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.module = json.loads(subprocess.check_output(
            ['go', 'list', '-m', '-json', helper.MODULE],
            cwd=SCRIPT.parents[2], text=True))
        cls.source = Path(cls.module['Dir'])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='mongo-tools-notices-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source_copy = self.root / 'source'
        (self.source_copy / 'mongodump').mkdir(parents=True)
        (self.source_copy / 'common/json').mkdir(parents=True)
        for name in helper.DIGESTS:
            shutil.copyfile(self.source / name, self.source_copy / name)
        self.module_file = self.root / 'module.json'
        self.current_module = dict(self.module, Dir=str(self.source_copy))
        self.module_file.write_text(json.dumps(self.current_module))
        self.packages_file = self.root / 'packages.txt'
        self.imported = [helper.MODULE + '/mongodump', helper.MODULE + '/common/json']
        self.packages_file.write_text('\n'.join(self.imported + ['fmt', 'example.org/other/pkg']) + '\n')
        self.owners_file = self.root / 'owners.txt'
        self.owners_file.write_text('\n'.join(p + '|' + helper.MODULE + '|' + helper.VERSION + '|' for p in self.imported) + '\n')
        self.notices = self.root / 'native/notices'
        self.inventory = self.root / 'native/licenses.csv'

    def save(self):
        helper.save(self.module_file, self.packages_file, self.owners_file, self.notices, self.inventory)

    def assert_unapproved(self, message):
        with self.assertRaisesRegex(ValueError, message):
            self.save()
        self.assertFalse(self.inventory.exists())
        self.assertFalse(self.notices.exists())

    def test_exact_imports_and_all_license_texts_are_packaged(self):
        self.save()
        with self.inventory.open(newline='') as stream:
            rows = list(csv.reader(stream))
        self.assertEqual([row[0] for row in rows], sorted(self.imported))
        self.assertTrue(all(row[2] == 'Apache-2.0' for row in rows))
        saved = self.notices / helper.MODULE
        self.assertEqual((saved / 'LICENSE.md').read_bytes(), (self.source / 'LICENSE.md').read_bytes())
        self.assertEqual((saved / 'NOTICE.upstream-THIRD-PARTY-NOTICES').read_bytes(), (self.source / 'THIRD-PARTY-NOTICES').read_bytes())
        self.assertEqual((saved / 'LICENSE.Apache-2.0.txt').read_bytes(), (SCRIPT.parent / 'licenses/Apache-2.0.txt').read_bytes())
        (self.root / 'modules.json').write_text(json.dumps(self.current_module))
        subprocess.run([sys.executable, str(SCRIPT.with_name('package-notices.py')), str(self.root)], check=True)
        packaged = (self.root / 'THIRD_PARTY_NOTICES.md').read_text()
        for file in saved.iterdir():
            self.assertIn(file.read_text().rstrip(), packaged)

    def test_changed_abbreviated_notice_fails_closed(self):
        with (self.source_copy / 'LICENSE.md').open('a') as stream:
            stream.write('Additional restrictions')
        self.assert_unapproved('notice changed')

    def test_changed_third_party_notices_fail_closed(self):
        (self.source_copy / 'THIRD-PARTY-NOTICES').write_text('Truncated attribution')
        self.assert_unapproved('notice changed')

    def test_changed_module_version_fails_closed(self):
        self.current_module['Version'] = 'v0.0.0-20990101000000-deadbeef0000'
        self.module_file.write_text(json.dumps(self.current_module))
        self.assert_unapproved('module identity changed')

    def test_local_replacement_fails_closed(self):
        self.current_module['Replace'] = {'Path': '../fork'}
        self.module_file.write_text(json.dumps(self.current_module))
        self.assert_unapproved('module identity changed')

    def test_missing_import_inventory_fails_closed(self):
        self.packages_file.write_text('fmt\n')
        self.assert_unapproved('no imported packages')

    def test_missing_package_ownership_fails_closed(self):
        self.owners_file.write_text('')
        self.assert_unapproved('package ownership')

    def test_nested_module_cannot_borrow_license(self):
        self.owners_file.write_text(self.owners_file.read_text().replace(
            '|' + helper.MODULE + '|', '|' + helper.MODULE + '/nested|'))
        self.assert_unapproved('package ownership')

    def test_missing_package_source_fails_closed(self):
        (self.source_copy / 'mongodump').rmdir()
        self.assert_unapproved('package missing')

    def test_changed_full_apache_text_fails_closed(self):
        changed = self.root / 'Apache.txt'
        changed.write_text('Not the Apache license')
        with self.assertRaisesRegex(ValueError, 'notice changed'):
            helper.save(self.module_file, self.packages_file, self.owners_file, self.notices, self.inventory, apache_file=changed)
        self.assertFalse(self.inventory.exists())
        self.assertFalse(self.notices.exists())


if __name__ == '__main__':
    unittest.main()
