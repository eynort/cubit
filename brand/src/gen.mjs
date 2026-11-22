// gen.mjs: genera el logo de Cubit (concepto A, «insignia con mascota»).
// Todo es geometría escrita a mano; el texto sale de wordmark.json (trazados de
// Fredoka 600 y Outfit 500 hechos con outline.py), así que no hace falta
// ninguna fuente instalada.
//   node gen.mjs && ./render.sh
//
// Escribe en brand/:
//   icon.svg        icono de la app (placa oscura + insignia + robot), 1024
//   symbol.svg      la insignia sola, sin placa (imágenes promocionales)
//   favicon.svg     versión pequeña ajustada a la rejilla de 16/32 px
//   logo.svg        icono + «Cubit» + lema, para fondo oscuro
//   logo-light.svg  lo mismo para fondo claro
// y en brand/src/:
//   icon-small.svg  variante sin aro, rejilla ni antena (48 px del .ico)
//   panel-mark.svg  insignia ajustada a 28 px, de referencia
//   avatar.svg      avatar cuadrado 1024 para GitHub
// y reescribe los símbolos #bm-cube y #bm-word de internal/web/panel.html.
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const D = dirname(fileURLToPath(import.meta.url));
const BRAND = join(D, '..');
const WM = JSON.parse(readFileSync(join(D, 'wordmark.json'), 'utf8'));

const f = (n) => +n.toFixed(2);
const P = (p) => `${f(p[0])} ${f(p[1])}`;
const add = (...v) => v.reduce((a, b) => [a[0] + b[0], a[1] + b[1]], [0, 0]);
const mul = (v, k) => [v[0] * k, v[1] * k];
const dist = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1]);
const norm = (v) => { const l = Math.hypot(v[0], v[1]); return [v[0] / l, v[1] / l]; };
const poly = (pts) => 'M' + pts.map(P).join('L') + 'Z';

// Polígono con esquinas redondeadas (r por vértice; 0 = esquina viva).
function rpoly(pts, r) {
	const n = pts.length, R = Array.isArray(r) ? r : pts.map(() => r);
	let d = '';
	for (let i = 0; i < n; i++) {
		const p = pts[i], a = pts[(i - 1 + n) % n], b = pts[(i + 1) % n];
		const ri = R[i];
		if (!ri) { d += (i ? 'L' : 'M') + P(p); continue; }
		const ua = norm([a[0] - p[0], a[1] - p[1]]), ub = norm([b[0] - p[0], b[1] - p[1]]);
		const la = Math.min(ri, dist(a, p) / 2), lb = Math.min(ri, dist(b, p) / 2);
		const p1 = add(p, mul(ua, la)), p2 = add(p, mul(ub, lb));
		d += (i ? 'L' : 'M') + P(p1) + 'Q' + P(p) + ' ' + P(p2);
	}
	return d + 'Z';
}

// Cubo axonométrico: o = vértice superior izquierdo del frontal,
// F = arista frontal (hacia la derecha), S = arista lateral (hacia el fondo), V = altura.
function cube(o, F, S, V) {
	const UL = o, C = add(o, F), T = add(o, S), UR = add(o, S, F);
	const LL = add(o, V), B = add(o, F, V), LR = add(UR, V);
	return { UL, C, T, UR, LL, B, LR, F, S, V,
		top: [UL, T, UR, C], front: [UL, C, B, LL], side: [C, UR, LR, B],
		sil: [UL, T, UR, LR, B, LL] };
}
// matrix() que lleva el cuadrado 0..100 a una cara (u a lo largo de a, v a lo largo de b).
const faceM = (o, a, b) => `matrix(${f(a[0] / 100)} ${f(a[1] / 100)} ${f(b[0] / 100)} ${f(b[1] / 100)} ${f(o[0])} ${f(o[1])})`;

// ---------------------------------------------------------------- piezas
const tileDark = (id) => ({
	defs: `
  <linearGradient id="${id}t" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2b2f35"/><stop offset=".55" stop-color="#1a1d21"/><stop offset="1" stop-color="#101215"/></linearGradient>
  <linearGradient id="${id}te" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".22"/><stop offset=".25" stop-color="#fff" stop-opacity=".04"/><stop offset="1" stop-color="#000" stop-opacity=".35"/></linearGradient>`,
	body: `
 <rect x="16" y="16" width="992" height="992" rx="224" fill="url(#${id}t)"/>
 <rect x="17.5" y="17.5" width="989" height="989" rx="222.5" fill="none" stroke="url(#${id}te)" stroke-width="3"/>`,
});

// Insignia circular verde salvia con rejilla y aro blanco.
// sh = sombra del aro: [desviación, desplazamiento].
const badge = (id, { cx = 512, cy = 512, R = 396, ring = 40, sh = [16, 18] } = {}) => ({
	defs: `
  <linearGradient id="${id}bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#bfe0c4"/><stop offset=".5" stop-color="#8dbb95"/><stop offset="1" stop-color="#4f8059"/></linearGradient>
  <radialGradient id="${id}bh" cx=".32" cy=".18" r=".7"><stop offset="0" stop-color="#fff" stop-opacity=".38"/><stop offset=".6" stop-color="#fff" stop-opacity="0"/></radialGradient>
  <radialGradient id="${id}bv" cx=".5" cy=".42" r=".6"><stop offset=".7" stop-color="#173022" stop-opacity="0"/><stop offset="1" stop-color="#173022" stop-opacity=".32"/></radialGradient>
  <linearGradient id="${id}rg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#ffffff"/><stop offset=".55" stop-color="#eeeeeb"/><stop offset="1" stop-color="#c9cac6"/></linearGradient>
  <pattern id="${id}grid" width="58" height="58" patternUnits="userSpaceOnUse" x="${cx - 29}" y="${cy - 29}"><path d="M0 .5H58M.5 0V58" stroke="#fff" stroke-opacity=".16" stroke-width="2"/></pattern>
  <linearGradient id="${id}gm" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity="1"/><stop offset="1" stop-color="#fff" stop-opacity=".25"/></linearGradient>
  <mask id="${id}gmk"><circle cx="${cx}" cy="${cy}" r="${R - ring}" fill="url(#${id}gm)"/></mask>
  <filter id="${id}bs" x="-20%" y="-20%" width="140%" height="150%"><feGaussianBlur in="SourceAlpha" stdDeviation="${sh[0]}"/><feOffset dy="${sh[1]}"/><feComponentTransfer><feFuncA type="linear" slope=".55"/></feComponentTransfer><feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge></filter>`,
	body: `
 <g filter="url(#${id}bs)">
  <circle cx="${cx}" cy="${cy}" r="${R}" fill="url(#${id}rg)"/>
 </g>
 <circle cx="${cx}" cy="${cy}" r="${R - 1.5}" fill="none" stroke="#fff" stroke-width="3"/>
 <circle cx="${cx}" cy="${cy}" r="${R - ring}" fill="url(#${id}bg)"/>
 <circle cx="${cx}" cy="${cy}" r="${R - ring}" fill="url(#${id}grid)" mask="url(#${id}gmk)"/>
 <circle cx="${cx}" cy="${cy}" r="${R - ring}" fill="url(#${id}bv)"/>
 <circle cx="${cx}" cy="${cy}" r="${R - ring}" fill="url(#${id}bh)"/>
 <circle cx="${cx}" cy="${cy}" r="${R - ring + 2}" fill="none" stroke="#2b4a33" stroke-opacity=".35" stroke-width="4"/>`,
});

// Cabeza-robot brillante (la mascota).
function robot(id, c, s = 1, { antenna = true, outline = 26 } = {}) {
	const F = [290 * s, 40 * s], S = [116 * s, -100 * s], V = [0, 276 * s];
	const w = F[0] + S[0], hTop = -S[1], h = hTop + F[1] + V[1];
	const o = [c[0] - w / 2, c[1] - h / 2 + hTop];
	const K = cube(o, F, S, V);
	const rS = 30 * s;
	const sil = rpoly(K.sil, [rS, rS * 0.9, rS, rS, rS * 0.7, rS]);
	const fm = faceM(K.UL, F, V), sm = faceM(K.C, S, V), tm = faceM(K.UL, S, F);
	const topC = add(K.UL, mul(S, .5), mul(F, .5));
	const ant = antenna ? (() => {
		const base = topC, tip = add(base, [0, -88 * s]);
		return {
			out: `<path d="M${P(base)}V${f(tip[1])}" stroke="#fff" stroke-width="${f(outline + 16 * s)}" stroke-linecap="round"/><circle cx="${f(tip[0])}" cy="${f(tip[1])}" r="${f(26 * s + outline / 2)}" fill="#fff"/>`,
			body: `<ellipse cx="${f(base[0])}" cy="${f(base[1])}" rx="${f(24 * s)}" ry="${f(12 * s)}" fill="#a9aaa6"/>
   <path d="M${P(base)}V${f(tip[1])}" stroke="url(#${id}st)" stroke-width="${f(14 * s)}" stroke-linecap="round"/>
   <circle cx="${f(tip[0])}" cy="${f(tip[1])}" r="${f(26 * s)}" fill="url(#${id}ball)"/>
   <circle cx="${f(tip[0])}" cy="${f(tip[1])}" r="${f(26 * s)}" fill="none" stroke="#2f5a3a" stroke-opacity=".5" stroke-width="${f(3 * s)}"/>
   <ellipse cx="${f(tip[0] - 8 * s)}" cy="${f(tip[1] - 10 * s)}" rx="${f(9 * s)}" ry="${f(6 * s)}" fill="#fff" fill-opacity=".85"/>`,
		};
	})() : { out: '', body: '' };
	const shadowC = [(K.LL[0] + K.LR[0]) / 2 + 6 * s, K.B[1] + 26 * s];
	return {
		defs: `
  <linearGradient id="${id}top" gradientUnits="userSpaceOnUse" x1="${f(K.UL[0])}" y1="${f(K.T[1])}" x2="${f(K.UR[0])}" y2="${f(K.C[1])}"><stop offset="0" stop-color="#ffffff"/><stop offset="1" stop-color="#e7e8e4"/></linearGradient>
  <linearGradient id="${id}fr" gradientUnits="userSpaceOnUse" x1="0" y1="${f(K.UL[1])}" x2="0" y2="${f(K.B[1])}"><stop offset="0" stop-color="#e9eae6"/><stop offset="1" stop-color="#bdbeba"/></linearGradient>
  <linearGradient id="${id}sd" gradientUnits="userSpaceOnUse" x1="0" y1="${f(K.T[1])}" x2="0" y2="${f(K.LR[1])}"><stop offset="0" stop-color="#a6a7a3"/><stop offset="1" stop-color="#6f706c"/></linearGradient>
  <linearGradient id="${id}vis" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2a2d32"/><stop offset="1" stop-color="#121417"/></linearGradient>
  <linearGradient id="${id}eye" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#e9f7ec"/><stop offset=".55" stop-color="#b4e0bc"/><stop offset="1" stop-color="#86c493"/></linearGradient>
  <linearGradient id="${id}st" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#f4f4f1"/><stop offset="1" stop-color="#9a9b97"/></linearGradient>
  <radialGradient id="${id}ball" cx=".38" cy=".32" r=".75"><stop offset="0" stop-color="#eaf8ed"/><stop offset=".45" stop-color="#a8dcb3"/><stop offset="1" stop-color="#5f9d6c"/></radialGradient>
  <linearGradient id="${id}gl" gradientUnits="userSpaceOnUse" x1="0" y1="${f(K.UL[1])}" x2="0" y2="${f(K.B[1])}"><stop offset="0" stop-color="#fff" stop-opacity=".55"/><stop offset=".22" stop-color="#fff" stop-opacity="0"/></linearGradient>
  <clipPath id="${id}clip"><path d="${sil}"/></clipPath>
  <clipPath id="${id}cf"><path d="${poly(K.front)}"/></clipPath>
  <clipPath id="${id}cs"><path d="${poly(K.side)}"/></clipPath>
  <radialGradient id="${id}spec" gradientUnits="userSpaceOnUse" cx="${f(K.UL[0] + S[0] * .45 + F[0] * .3)}" cy="${f(K.UL[1] + S[1] * .45 + F[1] * .3)}" r="${f(150 * s)}"><stop offset="0" stop-color="#fff" stop-opacity=".9"/><stop offset="1" stop-color="#fff" stop-opacity="0"/></radialGradient>
  <filter id="${id}glow" x="-50%" y="-80%" width="200%" height="260%"><feGaussianBlur stdDeviation="${f(7 * s)}"/></filter>
  <filter id="${id}sh" x="-40%" y="-80%" width="180%" height="260%"><feGaussianBlur stdDeviation="${f(12 * s)}"/></filter>
  <filter id="${id}ds" x="-30%" y="-30%" width="160%" height="170%"><feGaussianBlur in="SourceAlpha" stdDeviation="${f(10 * s)}"/><feOffset dy="${f(12 * s)}"/><feComponentTransfer><feFuncA type="linear" slope=".35"/></feComponentTransfer><feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge></filter>`,
		body: `
 <ellipse cx="${f(shadowC[0])}" cy="${f(shadowC[1])}" rx="${f(w * .4)}" ry="${f(22 * s)}" fill="#11241a" fill-opacity=".5" filter="url(#${id}sh)"/>
 <g filter="url(#${id}ds)">
  <g fill="#fff">${ant.out}<path d="${sil}" stroke="#fff" stroke-width="${f(outline * 2)}" stroke-linejoin="round"/></g>
  ${ant.body}
  <g clip-path="url(#${id}clip)">
   <path d="${poly(K.side)}" fill="url(#${id}sd)"/>
   <path d="${poly(K.front)}" fill="url(#${id}fr)"/>
   <path d="${poly(K.front)}" fill="url(#${id}gl)"/>
   <path d="${poly(K.top)}" fill="url(#${id}top)"/>
   <g transform="${tm}"><path d="${rpoly([[22, 22], [78, 22], [78, 78], [22, 78]], 10)}" fill="none" stroke="#000" stroke-opacity=".07" stroke-width="2.4"/></g>
   <path d="${poly(K.top)}" fill="url(#${id}spec)"/>
   <g clip-path="url(#${id}cf)"><g transform="${fm}">
    <rect x="-2" y="30" width="104" height="34" fill="url(#${id}vis)"/>
    <path d="M-2 30.6H102" stroke="#000" stroke-opacity=".35" stroke-width="1.2"/>
    <path d="M-2 64.4H102" stroke="#fff" stroke-opacity=".5" stroke-width="1"/>
    <g filter="url(#${id}glow)" opacity=".9"><rect x="16" y="38.5" width="22" height="17" rx="5" fill="#a8e0b4"/><rect x="62" y="38.5" width="22" height="17" rx="5" fill="#a8e0b4"/></g>
    <rect x="16" y="38.5" width="22" height="17" rx="5" fill="url(#${id}eye)"/>
    <rect x="62" y="38.5" width="22" height="17" rx="5" fill="url(#${id}eye)"/>
    <rect x="19.5" y="41" width="7" height="4.5" rx="2" fill="#fff" fill-opacity=".9"/>
    <rect x="65.5" y="41" width="7" height="4.5" rx="2" fill="#fff" fill-opacity=".9"/>
    <g fill="#55575b" fill-opacity=".55"><rect x="38" y="76" width="4.5" height="10" rx="2.2"/><rect x="47.75" y="76" width="4.5" height="10" rx="2.2"/><rect x="57.5" y="76" width="4.5" height="10" rx="2.2"/></g>
   </g></g>
   <g clip-path="url(#${id}cs)"><g transform="${sm}">
    <rect x="-2" y="30" width="104" height="34" fill="#0d0e10"/>
    <path d="M-2 64.4H102" stroke="#fff" stroke-opacity=".18" stroke-width="1"/>
    <g stroke="#3e3f3c" stroke-opacity=".55" stroke-width="5" stroke-linecap="round"><path d="M34 76V88M50 76V88M66 76V88"/></g>
   </g></g>
   <path d="M${P(K.UL)}L${P(K.C)}L${P(K.UR)}" fill="none" stroke="#fff" stroke-width="${f(5 * s)}" stroke-linejoin="round"/>
   <path d="M${P(K.C)}L${P(K.B)}" stroke="#fff" stroke-opacity=".55" stroke-width="${f(4 * s)}"/>
  </g>
 </g>`,
	};
}

const svg = (body, defs, { vb = '0 0 1024 1024', size = [1024, 1024] } = {}) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vb}" width="${size[0]}" height="${size[1]}" role="img" aria-label="Cubit">
 <title>Cubit</title>
 <defs>${defs}
 </defs>${body}
</svg>
`;
const inner = (s) => s.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '').replace(/\s*<title>.*?<\/title>/, '');

// ---------------------------------------------------------------- icono
function icon() {
	const t = tileDark('a'), b = badge('a'), r = robot('ar', [512, 552], 1.0);
	return svg(t.body + b.body + r.body, t.defs + b.defs + r.defs);
}

// La insignia sola. La sombra del aro es más corta para que quepa en el recorte.
const SYM_VB = '84 84 856 856';
function symbol() {
	const b = badge('y', { sh: [7, 6] }), r = robot('yr', [512, 552], 1.0);
	return svg(b.body + r.body, b.defs + r.defs, { vb: SYM_VB, size: [856, 856] });
}

// Variante para 48 px: sin aro, rejilla ni antena; la insignia llena la placa.
function iconSmall() {
	const t = tileDark('as');
	const r = robot('asr', [512, 512], 1.32, { antenna: false, outline: 30 });
	const defs = t.defs + `
  <linearGradient id="asbg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#b4dbbb"/><stop offset="1" stop-color="#5a8c64"/></linearGradient>` + r.defs;
	const body = t.body + `
 <circle cx="512" cy="512" r="440" fill="#f4f4f1"/>
 <circle cx="512" cy="512" r="404" fill="url(#asbg)"/>` + r.body;
	return svg(body, defs);
}

// ---------------------------------------------------------------- versiones ajustadas a píxel
// Cubo en proyección oblicua (caras a 45°) con todas las aristas en coordenadas
// enteras de su rejilla. g = {front:[x0,y0,x1,y1], d: fondo, visor:[y0,y1], eyes:[[x0,x1],[x0,x1]], ey:[y0,y1]}
function pixBot(id, g) {
	const [x0, y0, x1, y1] = g.front, d = g.d, [v0, v1] = g.visor;
	const clip = rpoly([[x0, y0], [x0 + d, y0 - d], [x1 + d, y0 - d], [x1 + d, y1 - d], [x1, y1], [x0, y1]], [1, 1, 1, 1, 1, 1]);
	const eyes = g.eyes.map(([a, b]) => `M${a} ${g.ey[0]}H${b}V${g.ey[1]}H${a}Z`).join('');
	return {
		defs: `
  <linearGradient id="${id}fr" gradientUnits="userSpaceOnUse" x1="0" y1="${y0}" x2="0" y2="${y1}"><stop offset="0" stop-color="#e3e4e0"/><stop offset="1" stop-color="#b4b5b1"/></linearGradient>
  <linearGradient id="${id}sd" gradientUnits="userSpaceOnUse" x1="0" y1="${y0 - d}" x2="0" y2="${y1}"><stop offset="0" stop-color="#9a9b97"/><stop offset="1" stop-color="#686965"/></linearGradient>
  <clipPath id="${id}c"><path d="${clip}"/></clipPath>`,
		body: `
 <g clip-path="url(#${id}c)">
  <path d="M${x0} ${y0} ${x0 + d} ${y0 - d}H${x1 + d}L${x1} ${y0}Z" fill="#fff"/>
  <path d="M${x0} ${y0}H${x1}V${y1}H${x0}Z" fill="url(#${id}fr)"/>
  <path d="M${x1} ${y0} ${x1 + d} ${y0 - d}V${y1 - d}L${x1} ${y1}Z" fill="url(#${id}sd)"/>
  <path d="M${x0} ${v0}H${x1}V${v1}H${x0}Z" fill="#17191c"/>
  <path d="M${x1} ${v0} ${x1 + d} ${v0 - d}V${v1 - d}L${x1} ${v1}Z" fill="#0b0c0e"/>
  <path d="${eyes}" fill="#b8e3c0"/>
 </g>`,
	};
}

// Favicon: rejilla de 32 con las aristas en pares, así cae en píxel entero a 16 y a 32.
function favicon() {
	const r = pixBot('f', { front: [8, 12, 20, 24], d: 4, visor: [14, 20], eyes: [[10, 12], [16, 18]], ey: [16, 18] });
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" role="img" aria-label="Cubit">
 <title>Cubit</title>
 <defs>
  <linearGradient id="ft" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2b2f35"/><stop offset="1" stop-color="#101215"/></linearGradient>
  <linearGradient id="fg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#9ccaa4"/><stop offset="1" stop-color="#4c7d56"/></linearGradient>${r.defs}
 </defs>
 <rect width="32" height="32" rx="7" fill="url(#ft)"/>
 <circle cx="16" cy="16" r="15" fill="#f4f4f1"/>
 <circle cx="16" cy="16" r="13.5" fill="url(#fg)"/>${r.body}
</svg>
`;
}

// Cabecera del panel: insignia en rejilla de 28 (28 px, y 56 a 2x) y «Cubit»
// con la altura de mayúscula en 14 px y la línea base en el píxel 21 de 28.
const PM = { front: [6, 10, 18, 22], d: 4, visor: [13, 17], eyes: [[8, 11], [13, 16]], ey: [14, 16] };
function panelMarkParts() {
	const r = pixBot('bm', PM);
	return {
		defs: `
  <linearGradient id="bmg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#9ccaa4"/><stop offset="1" stop-color="#4c7d56"/></linearGradient>${r.defs}`,
		body: `
 <circle cx="14" cy="14" r="14" fill="#f4f4f1"/>
 <circle cx="14" cy="14" r="12.5" fill="url(#bmg)"/>${r.body}`,
	};
}
function panelMark() {
	const m = panelMarkParts();
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28" role="img" aria-label="Cubit">
 <title>Cubit</title>
 <defs>${m.defs}
 </defs>${m.body}
</svg>
`;
}
// Símbolos del panel (#bm-cube 28×28 y #bm-word, 28 de alto y PW px de ancho).
function panelSprite() {
	const m = panelMarkParts();
	const k = 14 / WM.name.capHeight;
	const PW = Math.ceil((WM.name.bbox[2] - WM.name.bbox[0]) * k);
	const vb = [WM.name.bbox[0], -21 / k, PW / k, 28 / k].map(f).join(' ');
	return { PW, html: `${m.defs.trim().replace(/\n\s*/g, '\n ')}
 <symbol id="bm-cube" viewBox="0 0 28 28">${m.body.replace(/\n\s*/g, '\n  ')}
 </symbol>
 <symbol id="bm-word" viewBox="${vb}"><path fill="currentColor" d="${WM.name.d}"/></symbol>
` };
}

// ---------------------------------------------------------------- logotipo
// Medidas en el sistema de wordmark.json (Cubit a 214 px, lema a 30 px).
const N = WM.name, T = WM.tag;
const LK = (() => {
	const iconAt = [38, 40], iconSize = 320;                 // la placa visible va de 43 a 353 (y de 45 a 355)
	const x = 398 - N.bbox[0];                               // tinta de la C a 45 px de la placa
	const nameBase = 245;                                    // bloque de texto centrado en la placa
	const tagBase = nameBase + 60;
	const tagX = x + N.bbox[0] - T.bbox[0];                  // M alineada con la C
	const W = 940;                                            // ~43 px a cada lado; 470 y 1128 px de ancho dan altos enteros
	return { iconAt, iconSize, x, nameBase, tagBase, tagX, W, H: 400 };
})();
function logo(ic, { wm, tg }) {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${LK.W} ${LK.H}" width="${LK.W}" height="${LK.H}" role="img" aria-label="Cubit">
 <title>Cubit</title>
 <!-- Texto en trazados: Fredoka 600 y Outfit 500 (SIL OFL 1.1), ver brand/FONTS.md -->
 <svg x="${LK.iconAt[0]}" y="${LK.iconAt[1]}" width="${LK.iconSize}" height="${LK.iconSize}" viewBox="0 0 1024 1024">${inner(ic)}</svg>
 <path transform="translate(${f(LK.x)} ${LK.nameBase})" fill="${wm}" d="${N.d}"/>
 <path transform="translate(${f(LK.tagX)} ${LK.tagBase})" fill="${tg}" d="${T.d}"/>
</svg>
`;
}

// Avatar cuadrado: la insignia centrada sobre el fondo oscuro, segura para el recorte circular.
function avatar(sym) {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">
 <defs><radialGradient id="avbg" cx=".5" cy=".32" r=".8"><stop offset="0" stop-color="#2b2f35"/><stop offset=".6" stop-color="#17191d"/><stop offset="1" stop-color="#0d0e10"/></radialGradient></defs>
 <rect width="1024" height="1024" fill="url(#avbg)"/>
 <svg x="82" y="82" width="860" height="860" viewBox="${SYM_VB}">${inner(sym)}</svg>
</svg>
`;
}

// ---------------------------------------------------------------- salida
const ic = icon(), sym = symbol();
const out = {
	[join(BRAND, 'icon.svg')]: ic,
	[join(BRAND, 'symbol.svg')]: sym,
	[join(BRAND, 'favicon.svg')]: favicon(),
	[join(BRAND, 'logo.svg')]: logo(ic, { wm: '#ecebe6', tg: '#90918d' }),
	[join(BRAND, 'logo-light.svg')]: logo(ic, { wm: '#131417', tg: '#6a6b68' }),
	[join(D, 'icon-small.svg')]: iconSmall(),
	[join(D, 'panel-mark.svg')]: panelMark(),
	[join(D, 'avatar.svg')]: avatar(sym),
};
for (const [p, s] of Object.entries(out)) writeFileSync(p, s);

// Los símbolos del panel se reescriben dentro de su <svg class="sprite"> en panel.html.
const PANEL = join(BRAND, '..', 'internal', 'web', 'panel.html');
const sp = panelSprite();
const html = readFileSync(PANEL, 'utf8');
const re = /(<svg class="sprite"[^>]*><defs>\n)[\s\S]*?(<\/defs><\/svg>)/;
if (!re.test(html)) throw new Error('panel.html: no encuentro el sprite');
writeFileSync(PANEL, html.replace(re, (_, a, b) => a + ' ' + sp.html + b));
console.log(`panel.html · bm-word ${sp.PW}×28`);
console.log(`ok · logo ${LK.W}×${LK.H}`);
