import { mockAll, fakeAll } from './data.mjs';
export const w = +(process.env.W || 1440), h = +(process.env.H || 900), scale = +(process.env.SC || 2), mobile = !!process.env.MOB;
export const mock = (p, j, l, m) => mockAll(p, j, l, m);
export const fake = fakeAll;
export const storage = () => ({ bot: 'sentry' });
const sl = ms => new Promise(r => setTimeout(r, ms));
export async function prep(page) {
  await page.click('[data-sec=servers]'); await sl(2500);
  if (process.env.MANAGE) {
    await page.evaluate(() => document.querySelector('[data-manage]')?.click()); await sl(2500);
    if (process.env.SV) { await page.evaluate(v => document.querySelector(`[data-sv=${v}]`)?.click(), process.env.SV); await sl(1500) }
    if (process.env.RCON) {
      for (const c of ['list', 'time set day', 'weather clear', 'gamemode creative Steve', 'give @p diamond 5']) {
        await page.evaluate(c => { const i = document.getElementById('rline'); i.value = c; document.getElementById('rf').requestSubmit() }, c); await sl(500);
      }
      await sl(4000); // los toasts se van
    }
  }
  await page.evaluate(() => document.activeElement?.blur());
}
