#!/usr/bin/env python3
"""Package exact license texts and MPL source archives from audited Go closures."""
import csv
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

output = Path(sys.argv[1]).resolve()
modules_raw = (output / 'modules.json').read_text()
modules = []
decoder = json.JSONDecoder()
while modules_raw.strip():
    module, end = decoder.raw_decode(modules_raw.lstrip())
    modules.append(module)
    modules_raw = modules_raw.lstrip()[end:]
modules.sort(key=lambda item: len(item['Path']), reverse=True)
# Owned compatibility modules use historical import paths. go-licenses cannot
# infer URLs for relative module replacements; point their already-scanned
# license rows to the actual project source, never the archived upstream repo.
local_urls = {}
repo = Path(__file__).resolve().parents[2]
for module in modules:
    replacement = module.get('Replace', {})
    relative = replacement.get('Path', '')
    if relative in ('./internal/compat/termbox', './internal/compat/yamlv2'):
        actual = Path(replacement.get('Dir', '')).resolve()
        expected = (repo / relative).resolve()
        if actual != expected or not (expected / 'LICENSE').is_file():
            raise SystemExit(f'Unexpected local license source: {module["Path"]}')
        local_urls[module['Path']] = 'https://github.com/wekan/wekango/blob/HEAD/' + relative[2:] + '/LICENSE'
rows = set()
for report in sorted(output.glob('*/licenses.csv')):
    with report.open(newline='') as stream:
        for row in csv.reader(stream):
            if not row:
                continue
            if row[0] in local_urls:
                if row[2] != 'MIT':
                    raise SystemExit(f'Owned compatibility facade license changed: {row}')
                row[1] = local_urls[row[0]]
            rows.add(tuple(row))
if not rows:
    raise SystemExit('No target license inventory: cannot package notices')
with (output / 'licenses.csv').open('w', newline='') as stream:
    csv.writer(stream).writerows(sorted(rows))
source_dir = output / 'sources'
source_dir.mkdir(exist_ok=True)
sources = {}
for package, url, license_id in sorted(rows):
    if license_id != 'MPL-2.0':
        continue
    module = next((m for m in modules if package == m['Path'] or package.startswith(m['Path'] + '/')), None)
    if module is None:
        raise SystemExit(f'Cannot map MPL package to a pinned module: {package}')
    resolved = module.get('Replace', module)
    path, version = resolved['Path'], resolved.get('Version')
    if not version:
        raise SystemExit(f'MPL local replacement needs an explicit corresponding-source archive: {path}')
    identity = path + '@' + version
    if identity in sources:
        continue
    downloaded = json.loads(subprocess.check_output(['go', 'mod', 'download', '-json', identity], text=True))
    if downloaded.get('Error') or not downloaded.get('Zip'):
        raise SystemExit(f'Cannot retrieve corresponding source for {identity}: {downloaded}')
    name = path.replace('/', '_') + '@' + version + '.zip'
    archive = source_dir / name
    shutil.copyfile(downloaded['Zip'], archive)
    sources[identity] = {
        'module': path, 'version': version, 'archive': 'sources/' + name,
        'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
        'go_sum': downloaded.get('Sum'), 'origin': downloaded.get('Origin'),
        'source_url': ((downloaded.get('Origin') or {}).get('URL', '') + '/tree/' + (downloaded.get('Origin') or {}).get('Hash', '')),
        'license': 'MPL-2.0', 'modifications': 'Unmodified module source from the pinned Go module archive',
    }
(source_dir / 'manifest.json').write_text(json.dumps(sources, indent=2, sort_keys=True) + '\n')
texts = {}
for target in sorted(output.iterdir()):
    notices = target / 'notices'
    if not notices.is_dir():
        continue
    for file in sorted(notices.rglob('*')):
        if file.is_file() and file.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'COPYRIGHT')):
            content = file.read_text(errors='replace')
            relative = str(file.relative_to(notices))
            key = (relative, hashlib.sha256(content.encode()).hexdigest())
            texts[key] = content
if not texts:
    raise SystemExit('No complete license files saved: cannot package notices')
lines = ['# Third-party notices', '',
         'Generated from the audited target package closures. WeKan-owned source is MIT;',
         'adapted MongoDB tool entry points retain Apache-2.0;',
         'each third-party component retains its own license below. See `licenses.csv`',
         'for the package inventory, `modules.json` for resolved versions, and',
         '`sources/manifest.json` for the exact MPL-2.0 corresponding-source archives.',
         'These files and source archives accompany the release in its dependency',
         'notices archive. They must remain available to recipients of the binaries.', '',
         '## MPL-2.0 corresponding source', '']
for identity, info in sorted(sources.items()):
    lines += [f"- `{identity}`: [pinned corresponding source]({info['source_url']}); `{info['archive']}`; SHA-256 `{info['sha256']}`."]
if not sources:
    lines += ['No MPL-2.0 package was reported in these audited targets.']
for (name, digest), content in sorted(texts.items()):
    lines += ['', '## ' + name, '', '````text', content.rstrip(), '````']
(output / 'THIRD_PARTY_NOTICES.md').write_text('\n'.join(lines) + '\n')
