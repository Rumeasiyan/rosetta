// Drives Chrome through the editor and the plug-in, and writes out what the network carried.
//   NODE_PATH=<dir with playwright-core> node walk.js <run> [out-dir]
// A run is a list of steps below. Everything in the document is invented.
const { chromium } = require('playwright-core');
const fs = require('fs');
const path = require('path');

const run = process.argv[2] || 'open';
const out = process.argv[3] || path.join(__dirname, 'walk-out', run);
fs.mkdirSync(out, { recursive: true });
const CHROME = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const PORT = process.env.WALK_PORT || '53443';
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a);

(async () => {
  const browser = await chromium.launch({
    executablePath: CHROME,
    headless: true,
    args: [`--host-resolver-rules=MAP *.epc.test 127.0.0.1:${PORT}`, '--ignore-certificate-errors'],
  });
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 }, ignoreHTTPSErrors: true, locale: 'en-GB' });
  if (run !== 'signed-out') { // the sign-in cookie, as the stand-in reads it
    await ctx.addCookies([{ name: 'standin_session', value: 'dev-officer', domain: '.epc.test', path: '/', secure: true, httpOnly: true, sameSite: 'Lax' }]);
  }
  const page = await ctx.newPage();

  // The network panel: every call that is not the editor's own files, with the headers the
  // browser really sent, including Cookie.
  const net = [];
  const interesting = (u) => /rosetta\.epc\.test|echo\.epc\.test|languagetoolplus|onlyoffice\.github\.io/.test(u);
  ctx.on('request', async (q) => {
    if (!interesting(q.url())) return;
    const e = { at: Date.now(), method: q.method(), url: q.url(), frame: '', body: q.postData() };
    try { e.frame = q.frame().url(); } catch { /* a worker's */ }
    net.push(e);
    q._e = e;
    try { e.headers = await q.allHeaders(); } catch { /* gone */ }
    log('->', q.method(), q.url(), e.body ? `body=${e.body}` : '', 'cookie=' + ((e.headers || {}).cookie || '(none)'), 'origin=' + ((e.headers || {}).origin || '(none)'));
  });
  ctx.on('response', async (r) => {
    if (!interesting(r.url())) return;
    const e = r.request()._e;
    if (!e) return;
    e.status = r.status();
    try { e.responseHeaders = await r.allHeaders(); e.responseBody = (await r.text()).slice(0, 2000); } catch { /* opaque */ }
    log('<-', r.status(), r.url(), 'acao=' + ((e.responseHeaders || {})['access-control-allow-origin'] || '(none)'));
  });
  ctx.on('requestfailed', (q) => {
    if (!interesting(q.url())) return;
    if (q._e) q._e.failed = q.failure() && q.failure().errorText;
    log('FAILED', q.method(), q.url(), q.failure() && q.failure().errorText);
  });
  page.on('console', (m) => { if (/rosetta|cors|ouch|plugin|access-control|error/i.test(m.text())) log('console', m.type(), m.text().slice(0, 300)); });
  page.on('pageerror', (e) => log('pageerror', String(e).slice(0, 300)));

  const shot = async (name) => { await page.screenshot({ path: path.join(out, name + '.png') }); log('shot', name); };

  await page.goto('https://page.epc.test/', { waitUntil: 'load' });
  const editor = page.frameLocator('iframe[name="frameEditor"]');
  await editor.locator('#id_viewer, #editor_sdk canvas').first().waitFor({ timeout: 120000 });
  await wait(4000);
  await shot('01-open');

  const editorFrame = () => page.frames().find((f) => f.name() === 'frameEditor');

  // Text sent before Check? Note the count, and hold a while with the plug-in open.
  const tab = editor.locator('.ribtab a[data-tab="plugins"], li.ribtab >> text=Plugins').first();
  await tab.click({ timeout: 20000 });
  await wait(1000);
  await shot('02-plugins-tab');
  await editor.getByText('LanguageTool', { exact: true }).first().click({ timeout: 20000 });
  await wait(800);
  await editor.getByText('Start', { exact: true }).first().click({ timeout: 10000 });
  await wait(3000);
  await shot('02b-clicked');
  log('frames', JSON.stringify(page.frames().map((f) => f.url().slice(0, 140))));

  const pluginFrame = async () => {
    for (let i = 0; i < 40; i++) {
      const f = page.frames().find((x) => /(languagetool|7CDB02C9[^/]*)\/index\.html/i.test(x.url()));
      if (f) return f;
      await wait(500);
    }
    throw new Error('the plug-in frame did not appear');
  };
  const plugin = await pluginFrame();
  await plugin.waitForLoadState('load');
  const origin = await plugin.evaluate(() => ({ origin: self.origin, href: location.href, top: (() => { try { return top.location.href; } catch { return 'cross-origin'; } })(), parent: (() => { try { return parent.location.href; } catch { return 'cross-origin'; } })(), cookieVisible: document.cookie }));
  log('plug-in frame', JSON.stringify(origin));
  fs.writeFileSync(path.join(out, 'frame.json'), JSON.stringify(origin, null, 2));
  await wait(3000);
  await shot('03-plugin-open');

  const state = async (label) => {
    const s = await plugin.evaluate(() => ({
      textarea: document.getElementById('textarea').innerText,
      result: document.getElementById('result').innerText,
      errors: [...document.querySelectorAll('#result .error')].map((e) => e.textContent),
      options: [...document.querySelectorAll('#language_id option')].map((o) => o.textContent),
      selected: (document.querySelector('#language_id') || {}).value,
      checkDisabled: document.getElementById('check').classList.contains('disabled'),
      loaderShown: !document.getElementById('loader-container').classList.contains('display-none'),
    }));
    log('panel', label, JSON.stringify(s));
    return s;
  };
  const panel = {};
  const before = net.length;
  panel.opened = await state('opened, nothing selected');

  // Select everything, watch what moves with nothing clicked.
  const canvas = editor.locator('#id_main_view, #editor_sdk').first();
  await canvas.click({ position: { x: 500, y: 200 } });
  await page.keyboard.press('Control+a');
  await wait(1000);
  await page.keyboard.press('Meta+a');
  await wait(3000);
  await shot('04-selected');
  panel.selected = await state('all selected');
  log('text sent before Check:', net.slice(before).filter((e) => e.method === 'POST').length, 'POSTs');

  const check = async (label) => {
    await plugin.locator('#check').click({ timeout: 5000 });
    await wait(3000);
    await shot('05-' + label);
    panel[label] = await state(label);
  };

  await check('check-auto');

  if (run === 'B') {
    // The first suggestion: where does it land, and does a replacement correct that word?
    panel.highlighted = await plugin.evaluate(() => [...document.querySelectorAll('#textarea span')].map((s) => s.textContent));
    log('highlighted', JSON.stringify(panel.highlighted));
    const first = plugin.locator('.result_div').first();
    if (await first.count()) {
      panel.contexts = await plugin.evaluate(() => [...document.querySelectorAll('.miniText')].map((m) => m.innerHTML));
      log('contexts', JSON.stringify(panel.contexts));
      await first.click();
      await wait(800);
      await shot('06-suggestion-open');
      await first.locator('button.replacment').first().click();
      await wait(3000);
      await shot('07-after-correct');
      panel.afterCorrect = await state('after one correction');
      // All the rest, then Replace into the document.
      for (let i = 0; i < 10; i++) {
        const next = plugin.locator('.result_div').first();
        if (!(await next.count())) break;
        await next.click();
        await wait(500);
        await next.locator('button.replacment').first().click();
        await wait(2500);
      }
      panel.allCorrected = await state('all corrected');
      await plugin.locator('#replace').click();
      await wait(3000);
      await shot('08-after-replace');
      const f = editorFrame();
      panel.documentAfter = f ? await f.evaluate(() => new Promise((res) => {
        try { res(window.Asc && window.Asc.editor ? window.Asc.editor.asc_GetSelectedText && window.Asc.editor.asc_GetSelectedText() : 'no api'); } catch (e) { res(String(e)); }
      })) : null;
      log('document selection after Replace', JSON.stringify(panel.documentAfter));
    }
    // Sinhala, Tamil, then Auto again, from the panel's own list.
    const choose = async (name) => {
      await plugin.evaluate((n) => {
        const select = window.$('#language_id');
        const option = [...document.querySelectorAll('#language_id option')].find((o) => o.textContent.startsWith(n));
        if (!option) return;
        select.val(option.value).trigger('change');
        select.trigger({ type: 'select2:select', params: { data: { id: Number(option.value), text: option.textContent, longcode: { Auto: 'auto', English: 'en-GB', Sinhala: 'si-LK', Tamil: 'ta-LK' }[n] } } });
      }, name);
      await wait(500);
      await plugin.locator('#textarea').click();
      await plugin.evaluate(() => { document.getElementById('textarea').innerText = 'We recieve the letters.'; });
      await page.keyboard.press('End');
      await wait(300);
      await check('language-' + name.toLowerCase());
    };
    for (const n of ['Sinhala', 'Tamil', 'English', 'Auto']) await choose(n);
  }

  const control = (service, mode, params) => fetch(`http://localhost:53190/control/${service}/mode`, { method: 'POST', body: JSON.stringify({ mode, params: params || {} }) });
  const registered = { registered_origin: 'https://writedocument.epc.test' };
  const signIn = () => ctx.addCookies([{ name: 'standin_session', value: 'dev-officer', domain: '.epc.test', path: '/', secure: true, httpOnly: true, sameSite: 'Lax' }]);
  const stillEdits = async (label) => {
    await canvas.click({ position: { x: 500, y: 400 } });
    await wait(300);
    await page.keyboard.type('Still typing. ');
    await wait(1000);
    await shot('10-editor-' + label);
  };

  if (run === 'refusals') {
    // Signed in at the start, then each of CERBERUS's refusals on the same open panel.
    await ctx.clearCookies();
    await check('cookie-removed');
    await stillEdits('cookie-removed');
    await signIn();
    await control('cerberus', 'refuse', registered);
    await check('office-rule');
    await control('cerberus', 'cookie');
    await ctx.clearCookies();
    await check('signed-out-address-not-registered');
    await signIn();
    await control('cerberus', 'refuse');
    await check('office-rule-address-not-registered');
    await control('cerberus', 'cookie', registered);
    await check('signed-in-again');
    if (process.env.STANDIN_PID) {
      process.kill(Number(process.env.STANDIN_PID));
      await wait(1000);
      await check('cerberus-down');
      await stillEdits('cerberus-down');
    }
  }

  if (run === 'selection') {
    // Replace acts on the selection as it is when clicked, not as it was at Check.
    await canvas.click({ position: { x: 900, y: 60 } });
    await page.mouse.click(620, 259, { clickCount: 3 });
    await wait(2000);
    panel.oneLine = await state('third line selected');
    await check('one-line');
    await plugin.locator('.result_div').first().click();
    await wait(500);
    await plugin.locator('.result_div').first().locator('button.replacment').first().click();
    await wait(2500);
    await plugin.locator('#replace').click();
    await wait(2500);
    await shot('11-one-line-replaced');
    // Check the first line, then move away before Replace.
    await page.mouse.click(620, 229, { clickCount: 3 });
    await wait(2000);
    panel.first = await state('first line selected');
    await check('first-line');
    await page.mouse.click(620, 244, { clickCount: 3 });
    await wait(2000);
    panel.moved = await state('second line selected after Check');
    await plugin.locator('.result_div').first().click();
    await wait(500);
    await plugin.locator('.result_div').first().locator('button.replacment').first().click();
    await wait(2500);
    await plugin.locator('#replace').click();
    await wait(2500);
    await shot('12-moved-selection-replaced');
  }

  if (run === 'edge') {
    // Type into the panel's own field, as how-to-run.md allows.
    const type = async (text) => {
      await plugin.evaluate((t) => {
        document.getElementById('textarea').innerText = t;
        window.$('#textarea').trigger('keyup');
      }, text);
    };
    const highlights = () => plugin.evaluate(() => {
      const t = document.getElementById('textarea');
      return { words: [...t.querySelectorAll('span')].map((s) => s.textContent), html: t.innerHTML.slice(0, 400), red: [...document.querySelectorAll('.miniText span')].map((s) => s.textContent).slice(0, 5), images: document.querySelectorAll('#result img, #textarea img').length };
    });
    await type('If a < b then we recieve <img src=x onerror=alert(1)> now.');
    await check('angle-brackets');
    panel.angle = await highlights();
    log('angle', JSON.stringify(panel.angle));
    // Past the source's size: split, then joined.
    const para = 'An invented paragraph about letters that we recieve at the office every single day of the week. ';
    await type(Array.from({ length: 80 }, (_, i) => para + i).join('\n'));
    await check('long');
    panel.long = await highlights();
    panel.longCount = panel.long.words.length;
    log('long', panel.long.words.length, new Set(panel.long.words).size, JSON.stringify([...new Set(panel.long.words)]));
    // ROSETTA's own refusals, reaching the panel.
    await control('bumblebee', 'unavailable');
    await type('A fresh invented sentence to recieve.');
    await check('no-source');
    await control('bumblebee', 'normal');
    await type('Another fresh invented sentence to recieve.');
    for (let i = 0; i < 4; i++) { await plugin.locator('#check').click(); await wait(150); }
    await wait(2500);
    await shot('13-over-limit');
    panel.overLimit = await state('asked too often');
  }

  await wait(1000);
  fs.writeFileSync(path.join(out, 'panel.json'), JSON.stringify(panel, null, 2));
  fs.writeFileSync(path.join(out, 'network.json'), JSON.stringify(net, null, 2));
  await browser.close();
  log('done', run);
})().catch((e) => { log('WALK FAILED', e.message.slice(0, 600)); process.exit(1); });
