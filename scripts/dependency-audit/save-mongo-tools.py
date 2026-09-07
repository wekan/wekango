#!/usr/bin/env python3
"""Review-bound workaround for mongo-tools' abbreviated Apache-2.0 notice.

Never generalize the exception to unknown licenses. Package ownership, pinned
module identity and all three exact notice texts must validate before emitting
any inventory or files. The normal scanner still checks every dependency.
"""
import csv
import hashlib
import json
from pathlib import Path
import sys

MODULE = 'github.com/mongodb/mongo-tools'
VERSION = 'v0.0.0-20260903204226-5df87866650a'
SUM = 'h1:c3PF12qFA5PDxyMy68LFnc0QaWlCVW4JT/6Vg5swfbY='
DIGESTS = {
    'LICENSE.md': 'e0b215145cf51c766603856643a746420f60aedfbf9a523a2d82a251cac3cce3',
    'THIRD-PARTY-NOTICES': '036c3102575183964caff76f2408b455d83e889567c564aed14cb5bda7aa57f9',
}
# Exact unmodified text from https://www.apache.org/licenses/LICENSE-2.0.txt.
APACHE_DIGEST = 'cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30'


def checked_bytes(path, digest):
    content = path.read_bytes()
    if hashlib.sha256(content).hexdigest() != digest:
        raise ValueError(f'mongo-tools notice changed: {path.name}; review before updating approved digest')
    return content


def save(module_file, packages_file, owners_file, notices, inventory,
         apache_file=Path(__file__).with_name('licenses') / 'Apache-2.0.txt'):
    module = json.loads(module_file.read_text())
    if (module.get('Path') != MODULE or module.get('Version') != VERSION
            or module.get('Sum') != SUM or module.get('Replace') or module.get('Main')):
        raise ValueError('mongo-tools module identity changed; review the pinned version and license')
    directory = Path(module['Dir'])
    packages = set(packages_file.read_text().splitlines())
    imported = sorted(p for p in packages if p.startswith(MODULE + '/'))
    if not imported:
        raise ValueError('mongo-tools has no imported packages in this target')
    owners = {}
    for line in owners_file.read_text().splitlines():
        if not line:
            continue
        path, owner, version, replaced = line.split('|')
        if path in owners:
            raise ValueError(f'duplicate package ownership: {path}')
        owners[path] = (owner, version, replaced)
    for package in imported:
        if owners.get(package) != (MODULE, VERSION, ''):
            raise ValueError(f'missing or changed mongo-tools imported package ownership: {package}')
        # A nested module cannot borrow this module's top-level notice.
        relative = package[len(MODULE) + 1:]
        if '..' in relative.split('/') or not (directory / relative).is_dir():
            raise ValueError(f'mongo-tools imported package missing from pinned source: {package}')
    content = {name: checked_bytes(directory / name, digest) for name, digest in DIGESTS.items()}
    apache = checked_bytes(apache_file, APACHE_DIGEST)
    # Do not create partial approval evidence on a failed review.
    destination = notices / MODULE
    destination.mkdir(parents=True, exist_ok=True)
    (destination / 'LICENSE.md').write_bytes(content['LICENSE.md'])
    (destination / 'LICENSE.Apache-2.0.txt').write_bytes(apache)
    # package-notices.py intentionally collects NOTICE* names. Preserve every
    # byte of the upstream attribution file under a collected filename.
    (destination / 'NOTICE.upstream-THIRD-PARTY-NOTICES').write_bytes(content['THIRD-PARTY-NOTICES'])
    inventory.parent.mkdir(parents=True, exist_ok=True)
    with inventory.open('w', newline='') as stream:
        writer = csv.writer(stream)
        for package in imported:
            writer.writerow((package, 'https://github.com/mongodb/mongo-tools/blob/5df87866650a/LICENSE.md', 'Apache-2.0'))


def main():
    if len(sys.argv) != 6:
        raise SystemExit('usage: save-mongo-tools.py module.json packages.txt package-owners.txt notices licenses.csv')
    try:
        save(*(Path(arg) for arg in sys.argv[1:]))
    except (ValueError, KeyError, OSError) as error:
        raise SystemExit(str(error)) from error


if __name__ == '__main__':
    main()
