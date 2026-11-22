export const w = +(process.env.W || 1440), h = +(process.env.H || 900), scale = +(process.env.SC || 2), mobile = !!process.env.MOB;
import { state } from './data.mjs';
export const mock = (p, j, l, m) => m == 'GET' && p.startsWith('/api/state') ? state(p, j, l, { noBots: true }) : undefined;
const steps = +(process.env.STEPS || 0);
export async function prep(page, lang) {
  await page.type('input:not([type=hidden])', 'Sentry', { delay: 30 });
  for (let i = 0; i < steps; i++) {
    await page.keyboard.press('Enter');
    await new Promise(r => setTimeout(r, 700));
    if (process.env.OWNER && i == 1) { await page.keyboard.type('Steve', { delay: 30 }); await new Promise(r => setTimeout(r, 300)); }
  }
  await page.evaluate(() => document.activeElement?.blur());
}
