// Datos de demo creíbles para las capturas del panel (EN/ES).
const X = (l, en, es) => l == 'es' ? es : en;
export const VER = 'v0.3.0';
const now = new Date().toISOString();

export function state(path, j, l, opts = {}) {
  if (!j) return undefined;
  const bots = [
    { id: 'sentry', name: 'Sentry', server: '127.0.0.1', server_id: 'test', on_test: true, owners: ['Steve'], running: true, connected: true, health: 17.5,
      task: X(l, 'fight: Alex_PvP', 'pelear: Alex_PvP'), queue: [X(l, '/goto 212 71 -360', '/goto 212 71 -360')], queue_paused: false, avatar: '/avatar/Sentry' },
    { id: 'miner', name: 'Miner', server: 'mc.example.net', server_id: 'smp', on_test: false, owners: ['Steve'], running: true, connected: true, health: 20,
      task: X(l, 'excavate: 120 64 -40 → 151 72 -9 · 38%', 'excavar: 120 64 -40 → 151 72 -9 · 38%'), queue: ['/mine iron ~ ~ ~', '/goto 0 64 0'], queue_paused: false, avatar: '/avatar/Miner' },
    { id: 'lumber', name: 'Lumber', server: '127.0.0.1', server_id: 'test', on_test: true, owners: ['Steve'], running: true, connected: true, health: 20,
      task: X(l, 'chop: oak · 46 logs', 'talar: roble · 46 troncos'), queue: [], queue_paused: false, avatar: '/avatar/Lumber' },
  ];
  const servers = [
    { id: 'test', name: 'Test server', addr: '127.0.0.1', kind: 'test', premium: false, power: 'on', join_port: 25565, version: '26.2', bots: ['sentry', 'lumber'] },
    { id: 'smp', name: 'Survival SMP', addr: 'mc.example.net', kind: 'external', premium: false, version: '26.2', detected: '1.21.4', via: 'ok', bots: ['miner'] },
  ];
  const out = { ...j, bots: opts.noBots ? [] : bots, servers: opts.noBots ? [servers[0]].map(s => ({ ...s, power: 'off', bots: [] })) : servers,
    last: 'sentry', test_server: { available: true, addr: '127.0.0.1', power: opts.noBots ? 'off' : 'on' },
    update: { ...(j.update || {}), enabled: true, current: VER, available: false, checked: now, method: '', plan: { kind: '' }, busy: 0 },
    setup: !!opts.noBots, defaults: { owner: '' } };
  return out;
}

const INV_EN = { cooked_beef: 40, obsidian: 32, wind_charge: 24, cobweb: 16, ender_pearl: 14, golden_apple: 12, end_crystal: 8, totem_of_undying: 3,
  netherite_sword: 1, shield: 1, mace: 1, netherite_helmet: 1, netherite_chestplate: 1, netherite_leggings: 1, netherite_boots: 1, bow: 1, arrow: 64, respawn_anchor: 4, glowstone: 16 };
const INV_ES = { filete_cocinado: 40, obsidiana: 32, carga_de_viento: 24, telaraña: 16, perla_de_ender: 14, manzana_dorada: 12, cristal_del_End: 8, tótem_de_inmortalidad: 3,
  espada_de_netherita: 1, escudo: 1, maza: 1, casco_de_netherita: 1, peto_de_netherita: 1, grebas_de_netherita: 1, botas_de_netherita: 1, arco: 1, flecha: 64, nexo_de_reaparición: 4, piedra_luminosa: 16 };
export function botJson(j, l) {
  return { ...(j || {}), name: 'Sentry', uptime: '2h 14m', health: 17.5, food: 18, x: 212.4, y: 71, z: -389.6,
    combat: X(l, 'fighting Alex_PvP', 'peleando contra [Alex_PvP]'),
    task: X(l, 'fight: Alex_PvP', 'pelear: Alex_PvP'), chunks: 441, entities: 37, players: 5,
    heap_mb: 31.4, sys_mb: 52.1, rss_mb: 68.2, limit_mb: 150, gc: 412, gc_pause_ms: 0.21, goroutines: 41,
    server: '127.0.0.1', connected: true,
    inventory: l == 'es' ? INV_ES : INV_EN,
    online: ['Alex_PvP', 'Kai_77', 'Lumber', 'Sentry', 'Steve'],
    queue: ['/goto 212 71 -360'], queue_paused: false };
}

const LOG = {
  en: [
    ['12:02:41', 'connecting to 127.0.0.1:25565 as Sentry'],
    ['12:02:41', 'login ok'],
    ['12:02:42', 'in the server'],
    ['12:03:10', '[panel] /guard Steve'],
    ['12:03:10', 'guarding Steve: following, attacking whoever hits him'],
    ['12:07:55', 'Alex_PvP hit Steve · target: Alex_PvP'],
    ['12:07:56', 'fight: Alex_PvP · shield up, closing in'],
    ['12:07:57', 'crit 9.5 · knockback chain ×3'],
    ['12:07:59', 'wind charge jump · mace smash 22.0'],
    ['12:08:01', 'Alex_PvP used a totem'],
    ['12:08:02', 're-totem · golden apple'],
    ['12:08:04', 'Alex_PvP pearled away · pearl after him'],
    ['12:08:06', 'cobweb on Alex_PvP · crit 9.5'],
    ['12:08:07', '<Steve> nice one'],
    ['12:08:09', 'Alex_PvP is low (3.0) · finishing'],
  ],
  es: [
    ['12:02:41', 'conectando a 127.0.0.1:25565 como Sentry'],
    ['12:02:41', 'login correcto'],
    ['12:02:42', 'dentro del servidor'],
    ['12:03:10', '[panel] /guard Steve'],
    ['12:03:10', 'protegiendo a Steve: lo sigo y ataco a quien le pegue'],
    ['12:07:55', 'Alex_PvP pegó a Steve · objetivo: Alex_PvP'],
    ['12:07:56', 'pelea: Alex_PvP · escudo arriba, me acerco'],
    ['12:07:57', 'crítico 9.5 · cadena de empujones ×3'],
    ['12:07:59', 'salto con carga de viento · golpe de maza 22.0'],
    ['12:08:01', 'Alex_PvP usó un tótem'],
    ['12:08:02', 'otro tótem a la mano · manzana dorada'],
    ['12:08:04', 'Alex_PvP se fue con una perla · perla detrás'],
    ['12:08:06', 'telaraña sobre Alex_PvP · crítico 9.5'],
    ['12:08:07', '<Steve> buena esa'],
    ['12:08:09', 'a Alex_PvP le queda poca vida (3.0) · a rematar'],
  ],
};
export function log(path, l) {
  const after = +(new URL('http://x' + path).searchParams.get('after') || 0);
  const lines = LOG[l].map(([t, s]) => '2026/11/22 ' + t + ' ' + s);
  return after >= lines.length ? { lines: [], next: lines.length } : { lines, next: lines.length };
}

export function settings(j) { return { ...(j || {}), server: '127.0.0.1', name: 'Sentry', owners: ['Steve'], ai: true, ai_model: '', ai_url: '', ai_state: 'ready', ai_pct: 100 } }

export function discord(j) { return { enabled: true, token_set: true, token_mask: '…k9Qw', channel_id: '1183920466771234816', allowed: ['402871963355201537'], state: 'connected' } }

export function server(j, l) {
  const p = (name, op, bot) => ({ name, op, bot });
  return { ...(j || {}), rcon: true, files: true, online: true, power: 'on', players: [p('Steve', true, false), p('Alex_PvP', false, false), p('Kai_77', false, false), p('Sentry', false, true), p('Lumber', false, true)],
    max: 20, difficulty: 'hard', mspt: 3.4, seed: '4126750918837215603', level_seed: '', whitelist: [], bans: [], ops: ['Steve'],
    gamerules: { keep_inventory: 'false', pvp: 'true', advance_time: 'true', advance_weather: 'true', spawn_monsters: 'true', mob_griefing: 'true', immediate_respawn: 'false', natural_health_regeneration: 'true', players_sleeping_percentage: '100' },
    props: { motd: 'Cubit test server', difficulty: 'hard', gamemode: 'survival', 'max-players': '20', 'view-distance': '10', 'simulation-distance': '10', pvp: 'true', 'white-list': 'false' },
    pending: false, power_since: 8040 };
}

export const RCON = {
  'list': 'There are 5 of a max of 20 players online: Steve, Alex_PvP, Kai_77, Sentry, Lumber',
  'time set day': 'Set the time to 1000',
  'weather clear': 'Set the weather to clear',
  'gamemode creative Steve': "Set Steve's game mode to Creative Mode",
  'give @p diamond 5': 'Gave 5 [Diamond] to Steve',
};
export const RCON_ES = {
  'list': 'Hay 5 de un máximo de 20 jugadores conectados: Steve, Alex_PvP, Kai_77, Sentry, Lumber',
  'time set day': 'Se ha cambiado la hora a 1000',
  'weather clear': 'Se ha cambiado el tiempo a despejado',
  'gamemode creative Steve': 'Se ha cambiado el modo de juego de Steve a Modo creativo',
  'give @p diamond 5': 'Se han dado 5 [Diamante] a Steve',
};

export function mockAll(path, j, l, method, opts = {}) {
  if (method != 'GET') return undefined;
  if (path.startsWith('/api/state')) return state(path, j, l, opts);
  if (/^\/b\/[^/]+\/json/.test(path)) return botJson(j, l);
  if (/^\/b\/[^/]+\/log/.test(path)) return log(path, l);
  if (/^\/b\/[^/]+\/settings/.test(path)) return settings(j);
  if (path.startsWith('/api/discord')) return discord(j);
  if (path == '/server' || path.startsWith('/server?')) return server(j, l);
  return undefined;
}
export function fakeAll(path, method, body, l) {
  if (method == 'POST' && /\/rcon$/.test(path)) { const line = JSON.parse(body || '{}').line; return { out: (l == 'es' ? RCON_ES : RCON)[line] || '' } }
  if (method == 'POST' && /\/(cmd|open)$/.test(path)) return {};
  return undefined;
}
