// node shoot.mjs <scene.mjs> <lang> <out.png> [w h scale mobile]
// scene.mjs exports: url(tok), w,h,scale,mobile, mock(path, realJson, lang) -> json|undefined, prep(page, lang)
import puppeteer from 'puppeteer-core';
import fs from 'fs';
const [scenePath, lang, out] = process.argv.slice(2);
const scene = await import(new URL(scenePath, 'file://' + process.cwd() + '/').href);
const base = 'http://127.0.0.1:8098';
const tok = fs.readFileSync(new URL('./tok', import.meta.url), 'utf8').trim();
const browser = await puppeteer.launch({ executablePath: '/usr/bin/chromium', headless: 'new', args: ['--hide-scrollbars', '--font-render-hinting=none'] });
const page = await browser.newPage();
const W = scene.w, H = scene.h, S = scene.scale;
await page.setViewport({ width: W, height: H, deviceScaleFactor: S, isMobile: !!scene.mobile, hasTouch: !!scene.mobile });
if (scene.mobile) await page.setUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1');
await page.emulateMediaFeatures([{ name: 'prefers-reduced-motion', value: 'reduce' }]);
await page.evaluateOnNewDocument((l, extra) => { try { localStorage.setItem('lang', l); for (const [k, v] of Object.entries(extra || {})) localStorage.setItem(k, v) } catch (e) { } }, lang, scene.storage ? scene.storage(lang) : {});
const cdp = await page.createCDPSession();
await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*', requestStage: 'Request' }, { urlPattern: '*', requestStage: 'Response' }] });
cdp.on('Fetch.requestPaused', async ev => {
  const u = new URL(ev.request.url);
  const path = u.pathname + u.search;
  let body = null;
  if (ev.responseStatusCode === undefined && ev.responseErrorReason === undefined) {
    const f = scene.fake ? scene.fake(path, ev.request.method, ev.request.postData, lang) : undefined;
    if (f !== undefined) { await cdp.send('Fetch.fulfillRequest', { requestId: ev.requestId, responseCode: 200, responseHeaders: [{ name: 'Content-Type', value: 'application/json' }], body: Buffer.from(JSON.stringify(f)).toString('base64') }).catch(() => { }); return }
    await cdp.send('Fetch.continueRequest', { requestId: ev.requestId }).catch(() => { }); return;
  }
  try {
    const ct = (ev.responseHeaders || []).find(h => h.name.toLowerCase() == 'content-type')?.value || '';
    if (ct.includes('json') && scene.mock) {
      const r = await cdp.send('Fetch.getResponseBody', { requestId: ev.requestId });
      const txt = r.base64Encoded ? Buffer.from(r.body, 'base64').toString() : r.body;
      let j = null; try { j = JSON.parse(txt) } catch (e) { }
      const m = scene.mock(path, j, lang, ev.request.method);
      if (m !== undefined) body = JSON.stringify(m);
      if (process.env.DUMP) fs.appendFileSync('dump.txt', `== ${ev.request.method} ${path}\n${txt.slice(0, 6000)}\n`);
    }
  } catch (e) { console.error('mock err', path, e.message) }
  if (body != null) await cdp.send('Fetch.fulfillRequest', { requestId: ev.requestId, responseCode: 200, responseHeaders: [{ name: 'Content-Type', value: 'application/json' }], body: Buffer.from(body).toString('base64') }).catch(() => { });
  else await cdp.send('Fetch.continueRequest', { requestId: ev.requestId }).catch(() => { });
});
page.on('console', m => { if (m.type() == 'error') console.error('page:', m.text()) });
await page.goto(base + (scene.path ? scene.path(lang) : '/') + (scene.path?.(lang)?.includes('?') ? '&' : '?') + 't=' + tok, { waitUntil: 'networkidle2' });
await new Promise(r => setTimeout(r, scene.wait ?? 2500));
if (scene.prep) await scene.prep(page, lang);
await new Promise(r => setTimeout(r, scene.wait2 ?? 1200));
await page.screenshot({ path: out });
await browser.close();
