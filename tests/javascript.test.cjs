'use strict';
const assert = require('node:assert/strict');
const { execFileSync, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const root = path.resolve(__dirname, '..');
const step = require('../internal/migrations/testdata/checklist-minicard-unset.js');

test('all repository JavaScript parses as a complete file', () => {
  const files = execFileSync('git', ['ls-files', '-co', '--exclude-standard', '-z', '--', '*.js', '*.cjs', '*.mjs'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
  assert.ok(files.includes('internal/migrations/testdata/checklist-minicard-unset.js'));
  for (const file of new Set(files)) {
    const result = spawnSync(process.execPath, ['--check', path.join(root, file)], { encoding: 'utf8' });
    assert.equal(result.status, 0, `${file}: ${result.error || result.stderr}`);
  }
});

test('syntax check rejects the truncated-object form shown by CodeQL', () => {
  assert.ok(process.env.TMPDIR, 'repository-local TMPDIR is required');
  const dir = fs.mkdtempSync(path.join(process.env.TMPDIR, 'wekango-js-syntax-'));
  try {
    const file = path.join(dir, 'fragment.js');
    fs.writeFileSync(file, "{ name: 'checklist-minicard-unset', async check(db) {} },\n{");
    assert.notEqual(spawnSync(process.execPath, ['--check', file]).status, 0);
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});

test('checklist fixture skips an existing marker and checks only legacy false', async () => {
  for (const done of [true, false]) {
    const calls = [];
    const db = { collection(name) { return { async findOne(selector) {
      calls.push({ name, selector });
      return name === '_wekan_migration' ? (done ? { _id: step.name } : null) : { _id: 'legacy' };
    } }; } };
    assert.equal(await step.check(db), !done);
    assert.deepEqual(calls[0], { name: '_wekan_migration', selector: { _id: 'checklist-minicard-unset' } });
    assert.equal(calls.length, done ? 1 : 2);
    if (!done) assert.deepEqual(calls[1], { name: 'checklists', selector: { showChecklistAtMinicard: false } });
  }
});

test('checklist fixture unsets legacy values and persists its once-ever marker', async () => {
  const calls = [];
  const db = { collection(name) { return {
    async updateMany(selector, update) { calls.push({ name, selector, update }); return { modifiedCount: 2 }; },
    async updateOne(selector, update, options) { calls.push({ name, selector, update, options }); },
  }; } };
  assert.deepEqual(await step.run(db), { fixed: 2, unresolved: 0 });
  assert.deepEqual(calls[0], { name: 'checklists', selector: { showChecklistAtMinicard: false }, update: { $unset: { showChecklistAtMinicard: '' } } });
  assert.equal(calls[1].name, '_wekan_migration');
  assert.deepEqual(calls[1].selector, { _id: step.name });
  assert.ok(calls[1].update.$set.at instanceof Date);
  assert.deepEqual(calls[1].options, { upsert: true });
});
