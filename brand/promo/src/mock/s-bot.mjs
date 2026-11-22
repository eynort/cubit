import { mockAll, fakeAll } from './data.mjs';
export const w = +(process.env.W || 1440), h = +(process.env.H || 900), scale = +(process.env.SC || 2), mobile = !!process.env.MOB;
export const mock = (p, j, l, m) => mockAll(p, j, l, m);
export const fake = fakeAll;
export const storage = () => ({ bot: 'sentry' });
const sl = ms => new Promise(r => setTimeout(r, ms));
export async function prep(page) {
  if (process.env.LIST) return;
  await page.keyboard.press('1'); await sl(2500);
  if (process.env.SUB) { await page.evaluate(s => { const b = [...document.querySelectorAll('button')].find(b => b.offsetParent && b.textContent.trim().toLowerCase() == s); b?.click() }, process.env.SUB); await sl(1500) }
  if (process.env.CFG) { await page.evaluate(() => { const b = [...document.querySelectorAll('button,a')].find(b => b.offsetParent && /^(settings|ajustes)$/i.test(b.textContent.trim())); b?.click() }); await sl(2000) }
  await page.evaluate(() => { document.activeElement?.blur(); const lg = document.getElementById('log'); if (lg) lg.scrollTop = lg.scrollHeight });
}
