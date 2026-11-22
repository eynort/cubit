export const w = 1440, h = 900, scale = 1; export const mock = () => undefined;
export const storage = () => ({ bot: 'sentry', tab: 'srv' });
export async function prep(page){await page.click('[data-sec=servers]');await new Promise(r=>setTimeout(r,2500))}
