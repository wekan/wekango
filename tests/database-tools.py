#!/usr/bin/env python3
"""Exercise the actual single executable against its own disposable SQLite files."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

binary = Path(os.environ['WEKANGO_BINARY']).resolve()
root = Path(tempfile.mkdtemp(prefix='database-tools.', dir=os.environ['TMPDIR']))
env = dict(os.environ, WRITABLE_PATH=str(root/'data'), MONGO_URL='', FERRETDB_SQLITE_DIR='', FERRETDB_SQLITE_URL='')

def run(tool, *args, success=True, extra_env=None):
    result = subprocess.run([str(binary), tool, *args], env=dict(env, **(extra_env or {})), capture_output=True, text=True, timeout=30)
    if success and result.returncode != 0:
        raise AssertionError(f'{tool} failed ({result.returncode}): {result.stderr}')
    if not success and result.returncode == 0:
        raise AssertionError(f'{tool} accepted invalid input')
    return result

for tool in ('bsondump', 'mongodump', 'mongorestore', 'mongoexport', 'mongoimport', 'mongofiles', 'mongostat', 'mongotop'):
    result = run(tool, '--version', extra_env={'PORT':'invalid'})
    assert 'main-5df87866650a' in result.stdout, result.stdout
    run(tool, '--help', extra_env={'PORT':'invalid'})
    run(tool, '--not-a-real-option', success=False)
source = root/'boards.json'
source.write_text(json.dumps({'_id':'existing', 'title':'Embedded database tools', 'createdAt':{'$date':{'$numberLong':'1577934245000'}}})+'\n')
run('mongoimport', '--db=wekan', '--collection=boards', '--file='+str(source))
archive = root/'backup.archive'
run('mongodump', '--db=wekan', '--archive='+str(archive))
assert archive.stat().st_size > 0
run('mongorestore', '--archive='+str(archive), '--nsFrom=wekan.*', '--nsTo=restored.*')
exported = root/'restored.json'
run('mongoexport', '--db=restored', '--collection=boards', '--jsonFormat=canonical', '--out='+str(exported))
assert json.loads(source.read_text()) == json.loads(exported.read_text())
# Explicit connection options retain upstream behavior; the command fails against
# a deliberately closed loopback port rather than silently opening local SQLite.
run('mongoexport', '--uri=mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=1000', '--db=wekan', '--collection=boards', success=False)
print('single-executable tool dispatch, SQLite dump/restore/export/import, typed data and explicit target passed')
print('Evidence:', root)
