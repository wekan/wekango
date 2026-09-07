#!/usr/bin/env python3
"""Snapshot source contracts; heuristic inventory, not a parity certificate.
Run with the source WeKan checkout path. Does not modify the source repository.
"""
import json
import pathlib
import re
import subprocess
import sys

source = pathlib.Path(sys.argv[1]).resolve()
target = pathlib.Path(__file__).resolve().parents[1] / 'internal/compatibility/source-manifest.json'
env, routes, collections = set(), set(), set()
for directory in ('client', 'server', 'models', 'imports'):
    for path in (source / directory).rglob('*.js'):
        if 'node_modules' in path.parts:
            continue
        text = path.read_text(encoding='utf-8')
        env.update(re.findall(r'process\.env\.([A-Z][A-Z0-9_]*)', text))
        env.update(re.findall(r'process\.env\[[\'"]([A-Z][A-Z0-9_]*)[\'"]\]', text))
        routes.update((method.upper(), route) for method, route in re.findall(
            r'WebApp\.handlers\.(get|post|put|patch|delete|options)\(\s*[\'"]([^\'"]+)[\'"]', text))
        collections.update(re.findall(r'new Mongo\.Collection\([\'"]([^\'"]+)', text))
steps = re.findall(r"name: '([^']+)'", (source / 'server/lib/schemaUpgradeSteps.js').read_text())
result = {
    'source': 'https://github.com/wekan/wekan',
    'commit': subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip(),
    'scope': 'literal JavaScript references; dynamic routes, environment aliases and collection constructors require additional audit',
    'environment': sorted(env),
    'restRoutes': [{'method': m, 'path': p} for m, p in sorted(routes)],
    'collections': sorted(collections),
    'schemaUpgradeSteps': steps,
    'implementedREST': ['POST /users/login (local password only)', 'POST /users/logout', 'GET /api/boards/:boardId'],
    'implementedMigrations': steps,
    'migrationScope': 'current twelve-step schema-upgrade pipeline only; older Meteor migration history remains pending',
}
target.write_text(json.dumps(result, indent=2) + '\n')
print(f'{len(env)} environment names, {len(routes)} REST routes, {len(collections)} literal collections, {len(steps)} schema steps')
