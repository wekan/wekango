'use strict';
// Verify the real executable flushes API summaries into the existing SQLite
// collection on shutdown, then read them through its embedded database tool.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const net = require('node:net');
const path = require('node:path');
const { createHash } = require('node:crypto');
const { spawn } = require('node:child_process');
const binary = process.env.WEKANGO_BINARY;
if (!binary || !process.env.TMPDIR) throw new Error('WEKANGO_BINARY and repository-local TMPDIR are required');

async function freePort() {
  const server = net.createServer();
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}
function launch(args, env) {
  const child = spawn(binary, args, { env, stdio: ['ignore', 'pipe', 'pipe'] });
  let log = '';
  child.stdout.on('data', chunk => { log += chunk; });
  child.stderr.on('data', chunk => { log += chunk; });
  const done = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => code === 0 ? resolve(log) : reject(new Error(`${code}/${signal}: ${log}`)));
  });
  // Retain errors for explicit awaits without a premature unhandled rejection.
  done.catch(() => {});
  return { child, done, log: () => log };
}
(async () => {
  const data = await fs.mkdtemp(path.join(process.env.TMPDIR, 'wekango-api-usage-'));
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const env = { ...process.env, MONGO_URL: '', FERRETDB_SQLITE_DIR: '', FERRETDB_SQLITE_URL: '', PORT: String(port), BIND_IP: '127.0.0.1', ROOT_URL: base, WRITABLE_PATH: data, CADDY_AUTO_HTTPS: 'false', WITH_API: 'true', HTTP_FORWARDED_COUNT: '0', WEKAN_SKIP_SCHEMA_UPGRADE: 'true', WEKAN_API_USAGE_FLUSH_MS: '1800000' };
  const token = 'security-report-fixture-token';
  for (const [collection, documents] of [
    ['users', [{_id:'report-user', username:'report-user', services:{resume:{loginTokens:[{hashedToken:createHash('sha256').update(token).digest('base64'), when:{$date:new Date().toISOString()}}]}}}]],
    ['boards', [{_id:'withheld-board', title:'Never expose this title', type:'board', archived:false, members:[{userId:'report-user', isActive:false}]}]],
  ]) {
    const input = path.join(data, `${collection}.json`);
    await fs.writeFile(input, JSON.stringify(documents));
    const importer = launch(['mongoimport', '--db=wekan', `--collection=${collection}`, '--jsonArray', `--file=${input}`], env);
    const timeout = setTimeout(() => importer.child.kill('SIGKILL'), 15000);
    try { await importer.done; } finally { clearTimeout(timeout); }
  }
  const server = launch([], env);
  let stopped = false;
  try {
    let ready = false;
    for (let i = 0; i < 100; i++) {
      assert.equal(server.child.exitCode, null, server.log());
      try { if ((await fetch(`${base}/health`, { signal: AbortSignal.timeout(500) })).ok) { ready = true; break; } } catch {}
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    assert.ok(ready, server.log());
    for (const [url, status] of [['/api/cards/one', 404], ['/api/cards/two', 404], ['/api/guessed-one', 404], ['/api/guessed-two', 404], ['/api', 404]]) {
      const response = await fetch(base + url, { headers: { 'X-Wekan-Client-IP': 'forged', 'X-Forwarded-For': 'forged', 'CF-IPCountry': 'FI', 'CF-IPCity': 'Helsinki' }, signal: AbortSignal.timeout(5000) });
      await response.arrayBuffer();
      assert.equal(response.status, status, url);
    }
    const listing = await fetch(`${base}/api/users/report-user/boards`, {headers:{Authorization:`Bearer ${token}`, 'CF-IPCountry':'FI', 'CF-IPCity':'Helsinki'}, signal:AbortSignal.timeout(5000)});
    assert.equal(listing.status, 200);
    assert.deepEqual(await listing.json(), []);
    server.child.kill('SIGTERM');
    const kill = setTimeout(() => server.child.kill('SIGKILL'), 15000);
    try { await server.done; stopped = true; } finally { clearTimeout(kill); }
    const output = path.join(data, 'usage.json');
    const exporter = launch(['mongoexport', '--db=wekan', '--collection=eventlog', '--jsonArray', `--out=${output}`], env);
    const timeout = setTimeout(() => exporter.child.kill('SIGKILL'), 15000);
    try { await exporter.done; } finally { clearTimeout(timeout); }
    const rows = JSON.parse(await fs.readFile(output, 'utf8'));
    assert.equal(rows.length, 4);
    const apiRows = rows.filter(row => row.stream === 'api');
    assert.deepEqual(apiRows.map(row => row.api).sort(), ['GET (no route)', 'GET /api/cards/:cardId', 'GET /api/users/:userId/boards']);
    for (const row of apiRows) {
      assert.equal(row.stream, 'api');
      assert.equal(typeof row._id, 'string');
      assert.equal(row.ip, '127.0.0.1');
      assert.equal(row.location.city, 'Helsinki');
      assert.ok(row.firstAt && row.at);
      // Source writer increments per flush despite the producer's batched count.
      assert.equal(row.count, 1);
      assert.equal(Object.keys(row.actors).length, row.apiUserId ? 2 : 1);
      if (row.api === 'GET /api/users/:userId/boards') assert.equal(row.apiUserId, 'report-user');
      else assert.ok(!Object.hasOwn(row, 'apiUserId'));
    }
    const security = rows.find(row => row.stream === 'security');
    assert.equal(security.bleed, 'StaleBleed');
    assert.equal(security.severity, 'medium');
    assert.equal(security.userId, 'report-user');
    assert.equal(security.ip, '127.0.0.1');
    assert.equal(security.location.city, 'Helsinki');
    assert.equal(security.count, 1);
    assert.equal(security.detail, 'withheld 1 board(s) whose membership is revoked');
    assert.ok(!JSON.stringify(security).includes('Never expose this title'));
    console.log('Native API/security reports: route grouping, public identity, revoked access, shutdown join and embedded SQLite export passed');
  } finally {
    if (!stopped) { server.child.kill('SIGKILL'); await server.done.catch(() => {}); }
    await fs.rm(data, { recursive: true, force: true });
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
