'use strict';
// Run against a stopped, disposable browser fixture, never production data.
const assert = require('node:assert/strict');
const { spawn } = require('node:child_process');
const binary = process.env.WEKANGO_BINARY;
const writable = process.env.WEKANGO_TEST_DATA;
if (!binary || !writable) throw new Error('WEKANGO_BINARY and WEKANGO_TEST_DATA must identify a built executable and disposable fixture');
const base = 'http://127.0.0.1:3910';
async function run(flags, validate) {
  const child = spawn(binary, [], { env: { ...process.env, MONGO_URL: '', FERRETDB_SQLITE_DIR: '', FERRETDB_SQLITE_URL: '', PORT: '3910', ROOT_URL: base, BIND_IP: '127.0.0.1', WRITABLE_PATH: writable, WEKAN_SKIP_SCHEMA_UPGRADE: '', WEKAN_FORCE_SCHEMA_UPGRADE: '', ...flags }, stdio: ['ignore', 'pipe', 'pipe'] });
  const exited = new Promise(resolve => {
    child.once('exit', (code, signal) => resolve({ code, signal }));
    child.once('error', error => resolve({ error }));
  });
  let log = '';
  child.stdout.on('data', b => { log += b; });
  child.stderr.on('data', b => { log += b; });
  let started = false;
  try {
    for (let i = 0; i < 100; i++) {
      if (child.exitCode !== null) throw new Error(`Server exited: ${log}`);
      try {
        const response = await fetch(`${base}/schema-upgrade-status?json`, { signal: AbortSignal.timeout(1000) });
        const state = await response.json();
        if (!state.running && (flags.WEKAN_SKIP_SCHEMA_UPGRADE === 'true' || state.finishedAt)) {
          validate(state); started = true; break;
        }
      } catch (err) { if (err instanceof assert.AssertionError) throw err; }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.ok(started, `Server did not reach expected state: ${log}`);
  } finally {
    child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 10000);
    try { const { code, signal, error } = await exited; assert.equal(code, 0, `shutdown ${signal || error || ''}: ${log}`); }
    finally { clearTimeout(timer); }
  }
}
(async () => {
  await run({}, state => {
    assert.equal(state.gated, true);
    assert.equal(Object.keys(state.steps).length, 12);
    for (const step of Object.values(state.steps)) assert.equal(step.status, 'skipped');
  });
  await run({ WEKAN_SKIP_SCHEMA_UPGRADE: 'true' }, state => {
    assert.equal(state.finishedAt, null);
    assert.deepEqual(state.steps, {});
  });
  await run({ WEKAN_FORCE_SCHEMA_UPGRADE: 'true' }, state => {
    assert.equal(state.gated, false);
    assert.ok(state.lastCheck);
    assert.equal(Object.keys(state.steps).length, 12);
    for (const step of Object.values(state.steps)) assert.notEqual(step.status, 'error', step.error);
  });
  console.log('process restart version gate, skip/force flags and graceful shutdown passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
