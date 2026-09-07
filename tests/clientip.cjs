'use strict';
// Real public Caddy ingress -> private API -> embedded database validation.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const net = require('node:net');
const path = require('node:path');
const { spawn } = require('node:child_process');
const binary = process.env.WEKANGO_BINARY;
if (!binary) throw new Error('WEKANGO_BINARY must identify the newly built executable');
if (!process.env.TMPDIR) throw new Error('TMPDIR must identify a repository tools temporary directory');
const temporaryRoot = path.resolve(process.env.TMPDIR);
async function freePort() {
  const socket = net.createServer();
  await new Promise((resolve, reject) => { socket.once('error', reject); socket.listen(0, '127.0.0.1', resolve); });
  const port = socket.address().port;
  await new Promise(resolve => socket.close(resolve));
  return port;
}
async function scenario(count) {
  const data = await fs.mkdtemp(path.join(temporaryRoot, 'wekango-clientip-'));
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const child = spawn(binary, [], { env: { ...process.env, WITH_API: 'true', CADDY_AUTO_HTTPS: 'false', FILES_PATH: '', MONGO_URL: '', FERRETDB_SQLITE_DIR: '', FERRETDB_SQLITE_URL: '', PORT: String(port), ROOT_URL: base, BIND_IP: '127.0.0.1', WRITABLE_PATH: data, WEKAN_SKIP_SCHEMA_UPGRADE: 'true', HTTP_FORWARDED_COUNT: count }, stdio: ['ignore', 'pipe', 'pipe'] });
  let log = '';
  child.stdout.on('data', chunk => { log += chunk; });
  child.stderr.on('data', chunk => { log += chunk; });
  const exited = new Promise(resolve => { child.once('exit', (code, signal) => resolve({ code, signal })); child.once('error', error => resolve({ error })); });
  try {
    let ready = false;
    for (let i = 0; i < 100; i++) {
      assert.equal(child.exitCode, null, log);
      try { if ((await fetch(`${base}/health`, { signal: AbortSignal.timeout(500) })).ok) { ready = true; break; } } catch {}
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    assert.ok(ready, log);
    let attempts = 0;
    async function login(chain) {
      const response = await fetch(`${base}/users/login`, { method: 'POST', signal: AbortSignal.timeout(5000), headers: { 'Content-Type': 'application/json', 'X-Forwarded-For': chain, 'X-Wekan-Client-IP': `forged-private-${attempts++}` }, body: JSON.stringify({ username: 'does-not-exist', password: 'invalid' }) });
      await response.arrayBuffer();
      if (response.status === 429) assert.ok(Number(response.headers.get('retry-after')) > 0);
      return response.status;
    }
    const trusted = count === '1' || count === '2';
    const chain = (client, i) => count === '2' ? `forged-${i}, ${client}, proxy` : `forged-${i}, ${client}`;
    for (let i = 0; i < 10; i++) assert.equal(await login(chain('client-a', i)), 401, `count=${count} attempt=${i}`);
    assert.equal(await login(chain('client-a', 10)), 429, `count=${count}: forged prefixes bypassed lockout`);
    assert.equal(await login(chain('client-b', 11)), trusted ? 401 : 429, `count=${count}: wrong address partition`);
    if (count === '2') {
      for (let i = 0; i < 10; i++) assert.equal(await login(`short-${i}`), 401);
      assert.equal(await login('another-short-chain'), 429, 'short chains must share the socket address');
    }
    console.log(`HTTP_FORWARDED_COUNT=${count}: public ingress throttle, forged-header rejection and address partition passed`);
  } finally {
    child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 10000);
    try { const result = await exited; assert.equal(result.code, 0, `${JSON.stringify(result)}: ${log}`); }
    finally { clearTimeout(timer); await fs.rm(data, { recursive: true, force: true }); }
  }
}
(async () => { for (const count of ['0', '1', '2', '0x2']) await scenario(count); })().catch(error => { console.error(error); process.exitCode = 1; });
