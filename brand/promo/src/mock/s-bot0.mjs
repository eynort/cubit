export const w = 1440, h = 900, scale = 1; export const mock = () => undefined;
export const storage = () => ({ bot: 'sentry', tab: 'bot' });
export async function prep(page) { await page.keyboard.press('1'); await new Promise(r => setTimeout(r, 2500)); }
