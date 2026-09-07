'use strict';
// Requires a disposable fixture from go run ./tests/browserfixture NEW_DB_DIR.
// Reuses WeKan's installed Playwright via PLAYWRIGHT_MODULE when run alongside it.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { chromium, firefox } = require(process.env.PLAYWRIGHT_MODULE || '@playwright/test');
const base = process.env.WEKANGO_BROWSER_URL || 'http://127.0.0.1:3900';
(async () => {
  fs.mkdirSync(process.env.WEKANGO_SCREENSHOTS || '.', { recursive: true });
  for (const [name, engine] of [['chromium', chromium], ['firefox', firefox]]) {
    const browser = await engine.launch();
    try {
      const page = await browser.newPage();
      await page.goto(base);
      assert.equal(await page.title(), 'WeKan Go — compatibility preview');
      await page.locator('[name=username]').fill('browser-user');
      await page.locator('[name=password]').fill('wrong-password');
      await page.locator('#login button').click();
      await page.waitForFunction(() => document.querySelector('#status').textContent.includes('Incorrect'));
      await page.locator('[name=password]').fill('browser-fixture-password');
      await page.locator('#login button').click();
      await page.locator('#board').waitFor({ state: 'visible' });
      await page.locator('[name=boardId]').fill('browser-board');
      await page.locator('#board button').click();
      await page.waitForFunction(() => document.querySelector('#title').textContent === 'Existing SQLite board');
      assert.match(await page.locator('#description').textContent(), /FerretDB/);
      assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
      await page.screenshot({ path: `${process.env.WEKANGO_SCREENSHOTS || '.'}/${name}-board.png` });
      await page.goto(`${base}/schema-upgrade-status`);
      await page.waitForFunction(() => document.body.textContent.includes('Completed') || document.body.textContent.includes('Already re-checked'));
      assert.match(await page.title(), /WeKan Schema Upgrade/);
      const response = await page.request.get(`${base}/schema-upgrade-status?json`);
      assert.equal(response.status(), 200);
      const state = await response.json();
      assert.equal(state.running, false);
      assert.equal(Object.keys(state.steps).length, 12);
      assert.ok(state.lastCheck, 'successful startup must persist its version check');
      assert.equal(state.steps['board-allows-defaults'].status, state.gated ? 'skipped' : 'done');
      for (const [step, progress] of Object.entries(state.steps)) assert.notEqual(progress.status, 'error', `${step}: ${progress.error}`);
      await page.screenshot({ path: `${process.env.WEKANGO_SCREENSHOTS || '.'}/${name}-schema-upgrade.png` });
      console.log(`${name}: existing SQLite board, authentication and twelve-step startup dashboard passed`);
    } finally { await browser.close(); }
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
