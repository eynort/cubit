// Genera las páginas HTML de las imágenes promocionales (<nombre>[-es].html, en esta carpeta).
// node gen.mjs  → luego ./render.sh las pasa a PNG en brand/promo/.
// Las capturas del panel que salen en hero y social-preview están en shots/.
import fs from 'fs';
const P=new URL('.',import.meta.url).pathname;
const SH='shots/';           // relativo a las páginas
const OUT=P;
const BRAND=P+'../../';

// ---------- marca ----------
// La insignia (brand/symbol.svg) y «Cubit» en trazados (brand/src/wordmark.json, Fredoka 600).
const WM=JSON.parse(fs.readFileSync(BRAND+'src/wordmark.json','utf8')).name;
const symbol=fs.readFileSync(BRAND+'symbol.svg','utf8').replace(/<\?xml[^>]*>/,'').replace(/\s*<title>.*?<\/title>/,'');
const SYM_IN=32/856;          // aire entre el borde del símbolo y el aro, en fracción del tamaño
let symN=0;
// cada copia lleva sus propios id para que no choquen en la misma página
const cube=(px)=>{symN++;const k='_'+symN;return symbol.replace(/ width="\d+" height="\d+"/,'').replace('<svg ',`<svg width="${px}" height="${px}" class="cube" aria-hidden="true" `)
 .replace(/id="([^"]+)"/g,`id="$1${k}"`).replace(/url\(#([^)]+)\)/g,`url(#$1${k})`)};
// «Cubit» en una caja de h px de alto, con la mayúscula (cap px) centrada en vertical
const word=(h,cap)=>{const s=cap/WM.capHeight,w=Math.ceil((WM.bbox[2]-WM.bbox[0])*s);
 const vb=[WM.bbox[0],-(h/2+cap/2)/s,w/s,h/s].map(v=>+v.toFixed(2)).join(' ');
 return `<svg class="wm" width="${w}" height="${h}" viewBox="${vb}" aria-hidden="true"><path fill="#ecebe6" d="${WM.d}"/></svg>`};
// insignia + nombre; la tinta del aro queda alineada con el borde de la columna
const lockup=(c,cap,gap=Math.round(c*.2))=>`<div class="lockup" style="gap:${gap}px;margin-left:${-Math.round(c*SYM_IN)}px">${cube(c)}${word(c,cap)}</div>`;

// ---------- iconos en píxel (16×16, propios) ----------
const PAL={W:'#f4f4f1',L:'#cfd0cc',M:'#9a9b97',D:'#5d5e5b',E:'#3a3c40',K:'#131417',G:'#a8cfae',g:'#5f8f68',A:'#ddb877',a:'#9c7a3c',R:'#e5907f'};
function pix(rows,size){const n=rows.length;let r='';rows.forEach((row,y)=>[...row].forEach((ch,x)=>{if(PAL[ch])r+=`<rect x="${x}" y="${y}" width="1.02" height="1.02" fill="${PAL[ch]}"/>`}));
 return `<svg class="pix" width="${size}" height="${size}" viewBox="0 0 ${n} ${n}" shape-rendering="crispEdges" aria-hidden="true">${r}</svg>`}

function grid(){return Array.from({length:16},()=>Array(16).fill('.'))}
function swords(){const g=grid(),set=(x,y,c)=>{if(x>=0&&x<16&&y>=0&&y<16)g[y][x]=c};
 for(const m of [0,1]){const X=x=>m?15-x:x;
  for(let i=1;i<=10;i++){set(X(i),i,'W');if(i<10)set(X(i+1),i,'L')}
  set(X(0),0,'.');
  for(let k=-2;k<=2;k++){set(X(11+k),11-k,'D');set(X(12+k),11-k,'D')}
  set(X(12),12,'a');set(X(13),12,'A');set(X(13),13,'a');set(X(14),13,'A');set(X(14),14,'M');set(X(15),14,'D');set(X(15),15,'D')}
 return g.map(r=>r.join(''))}
function digIcon(){const g=grid(),band=y=>y<6?'L':y<8?'M':y<10?'L':y<12?'M':y<14?'L':'D';
 for(let y=4;y<16;y++)for(let x=0;x<16;x++)g[y][x]=band(y);
 const cut=(y0,y1,x0,x1)=>{for(let y=y0;y<=y1;y++)for(let x=x0;x<=x1;x++)g[y][x]='.'};
 cut(4,5,2,13);cut(6,7,3,12);cut(8,9,4,11);cut(10,11,5,8);
 for(let y=10;y<=11;y++)for(let x=9;x<=10;x++)g[y][x]='g';
 // el bot (3×2) en el fondo y un cofre arriba
 for(const [y,r] of [[9,'WWWW'],[10,'GEEG'],[11,'LLLL']])[...r].forEach((c,i)=>g[y][5+i]=c);
 g[2][12]='a';g[2][13]='A';g[2][14]='a';g[3][12]='a';g[3][13]='a';g[3][14]='a';
 for(let x=0;x<16;x++)if(g[4][x]!='.')g[4][x]='W';
 return g.map(r=>r.join(''))}
const ICON={
 pvp:swords(),
 dig:digIcon(),
 path:[ // A*: camino con puente y torre hasta la bandera
 '...........DGG..',
 '...........DGGG.',
 '...........DG...',
 '...........D....',
 '..........WW....',
 '..........WG....',
 '..........WG....',
 '..........WG....',
 '..........WG....',
 '.GGGGGGGGGWG....',
 '.G.......LW.....',
 'GKG.WWW..LW.....',
 'MMMMLLL..MMMMMMM',
 'MMMM.....MMMMMMM',
 'DDDD.....DDDDDDD',
 'DDDD.....DDDDDDD'],
 panel:[ // varios bots en un panel
 'EEEEEEEEEEEEEEEE',
 'EDDDDDDDDDDDDDDE',
 '................',
 'WWWW..WWWW..WWWW',
 'GEEG..GEEG..AEEA',
 'LLLL..LLLL..LLLL',
 'MMMM..MMMM..MMMM',
 '................',
 '.GG....GG....AA.',
 '................',
 'DDDD..DDDD..DDDD',
 '................',
 '................',
 'MMMMMMMMMMMMMMMM',
 'LLLLLLLLLLLLLLLL',
 '................'],
 mine:[ // un bloque con mineral y un árbol: minar y talar
 '................',
 '................',
 '..........GG....',
 '.........GGGG...',
 '........GGGGGG..',
 '........GGgGGG..',
 '.......GGGGGGgG.',
 '.......GgGGGGGG.',
 'WWWWWWW.GGGgGG..',
 'LLLLLLL..gGGg...',
 'MAAMMDM...aA....',
 'MMADMMM...aA....',
 'MDMMMAM...aA....',
 'MMMDAAM...aA....',
 'DMMMMMD...aA....',
 'DDDDDDD.DDDDDDDD'],
 afk:[ // el bot de guardia, de noche
 'GGGGG......LLL..',
 '...G......LL....',
 '..G......LL.....',
 '.G.......LL.....',
 'GGGGG.....LL....',
 '...........LLL..',
 '................',
 '....WWWWWWWW....',
 '....LLLLLLLL....',
 '....EEEEEEEE....',
 '....EGGEEGGE....',
 '....LLLLLLLL....',
 '....LLLLLLLL....',
 '....MMMMMMMM....',
 '................',
 'DDDDDDDDDDDDDDDD'],
 ai:[ // una frase que se convierte en órdenes
 '................',
 '.LLLLLLLLLLLLL..',
 'LEEEEEEEEEEEEEL.',
 'LEWWWWWEWWWWEEL.',
 'LEEEEEEEEEEEEEL.',
 'LEWWWEWWWWWEEEL.',
 'LEEEEEEEEEEEEEL.',
 '.LLLLLLLLLLLLL..',
 '..LLE...........',
 '..LE............',
 '..L.............',
 '................',
 '....gggggggggggg',
 '....gGgDDDDDDDDg',
 '....gggggggggggg',
 '................'],
 // dónde corre
 linux:[
 '................',
 'EEEEEEEEEEEEEEEE',
 'EDDDDDDDDDDDDDDE',
 'EKKKKKKKKKKKKKKE',
 'EKWKKKKKKKKKKKKE',
 'EKKWKKKKKKKKKKKE',
 'EKKKWKKKKKKKKKKE',
 'EKKWKKKKKKKKKKKE',
 'EKWKKGGGGKKKKKKE',
 'EKKKKKKKKKKKKKKE',
 'EKKKKKKKKKKKKKKE',
 'EKKKKKKKKKKKKKKE',
 'EEEEEEEEEEEEEEEE',
 '................',
 '................',
 '................'],
 mac:[
 '................',
 '..EEEEEEEEEEEE..',
 '..EKKKKKKKKKKE..',
 '..EKDDDDDDDDKE..',
 '..EKKKKKKKKKKE..',
 '..EKWWWWKKKKKE..',
 '..EKKKKKKKKKKE..',
 '..EKWWKKKKKKKE..',
 '..EKKKKKKKKKKE..',
 '..EEEEEEEEEEEE..',
 'MMMMMMMMMMMMMMMM',
 'LLLLLLLDDLLLLLLL',
 '.MMMMMMMMMMMMMM.',
 '................',
 '................',
 '................'],
 win:[
 '................',
 'EEEEEEEEEEEEEEEE',
 'EDDDDDDDDDDDDDDE',
 'EKKKKKKKKKKKKKKE',
 'EKLLLLLKKKKKKKKE',
 'EKLKKKLKKWWWWKKE',
 'EKLLLLLKKKKKKKKE',
 'EKKKKKKKKWWKKKKE',
 'EKKKKKKKKKKKKKKE',
 'EKKKKKKKKKKKKKKE',
 'EEEEEEEEEEEEEEEE',
 '......MMMM......',
 '......MMMM......',
 '....LLLLLLLL....',
 '................',
 '................'],
 pi:[
 '................',
 '.D.D.D.D.D.D.D..',
 'gggggggggggggggg',
 'gGGGGGGGGGGGGGGg',
 'gGKKKKKGGGGGGGGg',
 'gGKDDDKGGWWWWGGg',
 'gGKDDDKGGWWWWGGg',
 'gGKKKKKGGGGGGGGg',
 'gGGGGGGGGGGGGGGg',
 'gGGGGGGGMMMGGGGg',
 'gGWWGGGGMMMGGGGg',
 'gGWWGGGGGGGGGGGg',
 'gggggggggggggggg',
 '................',
 '................',
 '................'],
 phone:[
 '....EEEEEEEE....',
 '....EDDDDDDE....',
 '....EKKKKKKE....',
 '....EKWKKKKE....',
 '....EKKWKKKE....',
 '....EKWKKKKE....',
 '....EKKGGGKE....',
 '....EKKKKKKE....',
 '....EKKKKKKE....',
 '....EKKKKKKE....',
 '....EKKKKKKE....',
 '....EKKKKKKE....',
 '....EDDDDDDE....',
 '....EEEMMEEE....',
 '....EEEEEEEE....',
 '................'],
 vps:[
 '................',
 '.EEEEEEEEEEEEEE.',
 '.EKKKKKKKKKKKKE.',
 '.EKGKDDDDDDDDKE.',
 '.EKKKKKKKKKKKKE.',
 '.EEEEEEEEEEEEEE.',
 '.EKKKKKKKKKKKKE.',
 '.EKGKDDDDDDDDKE.',
 '.EKKKKKKKKKKKKE.',
 '.EEEEEEEEEEEEEE.',
 '.EKKKKKKKKKKKKE.',
 '.EKAKDDDDDDDDKE.',
 '.EKKKKKKKKKKKKE.',
 '.EEEEEEEEEEEEEE.',
 '................',
 '................'],
};

// ---------- textos ----------
const TX={
 en:{tag1:'Fights.',tag2:'Mines.',tag3:'Stays online.',tag3s:'Stays online.',sj:'<br>',kicker:'Minecraft bots that work for you',
  lead:'Expert PvP, mining, chopping and 24/7 AFK. Free local AI, a web panel for all your bots, ~70 MB of RAM each.',
  specs:['~70 MB per bot','Local AI','No coding'],three:['PvP','Mine','AFK 24/7','Local AI'],
  url:'localhost:8080',
  feat_code:'What Cubit does',feat_h:'Ready-to-use bots',
  f:[{k:'pvp',c:'F-01',h:'Expert PvP',p:'Shield, crits and combos. The mace with wind-charge jumps, end crystals, pearls, cobwebs and totems.',x:'It never runs away.'},
     {k:'dig',c:'F-02',h:'Excavate',p:'Empties a whole box between two corners, layer by layer, and stores the valuables in your chests.',x:'Never breaks a pickaxe.'},
     {k:'mine',c:'F-03',h:'Mine & chop',p:'Strip mining like a real player, no x-ray: it only mines ore it can see. Whole trees, 2×2 and Nether too.',x:'/mine · /chop'},
     {k:'afk',c:'F-04',h:'AFK 24/7',p:'Stays put around the clock: eats, sleeps and fights off mobs. If the server restarts, it rejoins by itself.',x:'Not kicked for idling.'},
     {k:'ai',c:'F-05',h:'Local AI',p:'Orders in plain words: “mine 20 iron and bring it to me”. It runs on your own machine, with Ollama.',x:'Free · no API keys.'},
     {k:'panel',c:'F-06',h:'Multi-bot panel',p:'All your bots, each with its console and autocomplete. Built-in test server, no Docker needed.',x:'English and Spanish.'}],
  inst_code:'Start in one minute',inst_h:'Three steps',
  steps:[{h:'Install',p:'Linux: one command, no root. Windows: Scoop. macOS and Android: the binary from Releases.',
     cmd:[['$','curl -fsSL …/latest/download/install.sh | sh   <i># Linux</i>'],['>','scoop bucket add cubit https://github.com/eynort/cubit'],['>','scoop install cubit/cubit                      <i># Windows</i>']]},
   {h:'Open the panel',p:'Cubit in your app menu opens it in the browser. Keep the whole link: it has your access key.',
     cmd:[['','App menu:  <b>Cubit</b>'],['','Terminal:  cubit --hub :8080 --open'],['','Panel:     http://localhost:8080/?t=…']]},
   {h:'Create your bot',p:'A wizard asks for name, server, owner and language, and can set up a test server for you.',
     cmd:[['','Name:       Sentry'],['','Server:     <em>Test server</em>'],['','Owner:      YourName'],['','Minecraft:  localhost:25565']]}],
  runs_code:'Runs on',runs_h:'Small enough for anything',runs_p:'One ~20 MB binary, no Java or Node. About 70 MB of RAM per bot.',
  plat:[['linux','Linux'],['mac','macOS'],['win','Windows'],['pi','Raspberry Pi'],['phone','Android · Termux'],['vps','Any VPS']],
  mcv:'Minecraft Java',mcx:'+ ViaProxy',proto:'protocol 776 · other versions',
  disclaimer:'Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.',
  banner_s:'PvP · Mining · AFK 24/7 · Local AI · Web panel',
  pan:{bots:['MOD-P1 · Bots','All your bots, one panel','Each bot with its own head, live state, a console with autocomplete and one-key orders.'],
   servers:['MOD-P2 · Servers','A test server, built in','Players, world and rules from the panel. External servers too, on other versions through ViaProxy.'],
   wizard:['MOD-P3 · First run','Ready in five steps','Name, server, owner and language. The wizard does the rest.']}},
 es:{tag1:'Pelea.',tag2:'Mina.',tag3:'No se desconecta.',tag3s:'No se<br>desconecta.',sj:' ',kicker:'Bots de Minecraft que trabajan por ti',
  lead:'PvP de experto, minería, tala y AFK 24/7. IA local y gratis, un panel web para todos tus bots, ~70 MB de RAM cada uno.',
  specs:['~70 MB por bot','IA local','Sin programar'],three:['PvP','Minar','AFK 24/7','IA local'],
  url:'localhost:8080',
  feat_code:'Qué hace Cubit',feat_h:'Bots listos para usar',
  f:[{k:'pvp',c:'F-01',h:'PvP de experto',p:'Escudo, críticos y combos. La maza con saltos de carga de viento, cristales del End, perlas, telarañas y tótems.',x:'Nunca huye.'},
     {k:'dig',c:'F-02',h:'Excavar',p:'Vacía una caja entera entre dos esquinas, capa a capa, y guarda lo valioso en tus cofres.',x:'Nunca rompe un pico.'},
     {k:'mine',c:'F-03',h:'Minar y talar',p:'Minería en ramas como un jugador, sin xray: solo pica lo que ve. Árboles enteros, también 2×2 y del Nether.',x:'/mine · /chop'},
     {k:'afk',c:'F-04',h:'AFK 24/7',p:'Aguanta día y noche: come, duerme y se defiende de los mobs. Si el servidor se reinicia, vuelve solo.',x:'Sin que lo echen.'},
     {k:'ai',c:'F-05',h:'IA local',p:'Órdenes con palabras normales: «mina 20 de hierro y tráemelo». Corre en tu propia máquina, con Ollama.',x:'Gratis · sin claves de API.'},
     {k:'panel',c:'F-06',h:'Panel multi-bot',p:'Todos tus bots, cada uno con su consola y autocompletado. Servidor de pruebas integrado, sin Docker.',x:'En inglés y en español.'}],
  inst_code:'Empieza en un minuto',inst_h:'Tres pasos',
  steps:[{h:'Instala',p:'Linux: un comando, sin root. Windows: Scoop. macOS y Android: el programa de Releases.',
     cmd:[['$','curl -fsSL …/latest/download/install.sh | sh   <i># Linux</i>'],['>','scoop bucket add cubit https://github.com/eynort/cubit'],['>','scoop install cubit/cubit                      <i># Windows</i>']]},
   {h:'Abre el panel',p:'Cubit, en el menú de aplicaciones, lo abre en el navegador. Guarda el enlace entero: lleva tu clave.',
     cmd:[['','Menú:      <b>Cubit</b>'],['','Terminal:  cubit --hub :8080 --open'],['','Panel:     http://localhost:8080/?t=…']]},
   {h:'Crea tu bot',p:'Un asistente te pide nombre, servidor, dueño e idioma, y puede montarte un servidor de pruebas.',
     cmd:[['','Nombre:     Sentry'],['','Servidor:   <em>Servidor de pruebas</em>'],['','Dueño:      TuNombre'],['','Minecraft:  localhost:25565']]}],
  runs_code:'Dónde corre',runs_h:'Cabe en cualquier sitio',runs_p:'Un programa de ~20 MB, sin Java ni Node. Unos 70 MB de RAM por bot.',
  plat:[['linux','Linux'],['mac','macOS'],['win','Windows'],['pi','Raspberry Pi'],['phone','Android · Termux'],['vps','Cualquier VPS']],
  mcv:'Minecraft Java',mcx:'+ ViaProxy',proto:'protocolo 776 · otras versiones',
  disclaimer:'No es un producto oficial de Minecraft. Ni aprobado por Mojang o Microsoft ni asociado con ellos.',
  banner_s:'PvP · Minería · AFK 24/7 · IA local · Panel web',
  pan:{bots:['MOD-P1 · Bots','Todos tus bots, un panel','Cada bot con su cabeza, su estado en vivo, una consola con autocompletado y órdenes a una tecla.'],
   servers:['MOD-P2 · Servidores','Servidor de pruebas incluido','Jugadores, mundo y reglas desde el panel. También servidores externos, de otras versiones con ViaProxy.'],
   wizard:['MOD-P3 · Primer uso','Listo en cinco pasos','Nombre, servidor, dueño e idioma. El asistente hace el resto.']}},
};

// ---------- estilos ----------
const CSS=`
:root{--bg0:#0b0c0e;--bg1:#17191d;--fill0:#1c1f23;--fill1:#15171a;--fill2:#121417;--edge:rgba(255,255,255,.15);--edge2:rgba(255,255,255,.045);
 --well:#0c0d0f;--line:#2a2e34;--seam:#060708;--fg:#ecebe6;--mut:#a6a8ab;--dim:#878a8f;--clay0:#e6e6e2;--clay1:#cfd0cc;--clay2:#b6b7b3;--ink:#131417;--ink2:#45474b;
 --ok:#a8cfae;--warn:#ddb877;--bad:#e5907f;
 --cond:"Bahnschrift SemiCondensed","Bahnschrift","DIN Condensed","DIN Alternate","Roboto Condensed","Barlow Condensed","Arial Narrow","Nimbus Sans Narrow","Helvetica Neue",sans-serif;
 --sans:system-ui,-apple-system,"Segoe UI","Inter",Roboto,"Noto Sans",sans-serif;
 --mono:ui-monospace,"SF Mono","JetBrains Mono","JetBrainsMono Nerd Font",Menlo,Consolas,"DejaVu Sans Mono",monospace}
*{box-sizing:border-box}
html,body{margin:0;background:var(--bg0)}
body{width:var(--W);height:var(--H);overflow:hidden;position:relative;color:var(--fg);font:16px/1.5 var(--sans);-webkit-font-smoothing:antialiased;
 background:radial-gradient(ellipse 60% 55% at 62% -10%,rgba(255,255,255,.10),transparent 70%),
  radial-gradient(ellipse 80% 40% at 50% 112%,rgba(255,255,255,.06),transparent 70%),
  linear-gradient(180deg,var(--bg0) 0%,#111316 55%,var(--bg1) 100%)}
.floor{position:absolute;left:-30%;right:-30%;bottom:-2%;height:var(--fh,46%);pointer-events:none;opacity:.5;
 background:repeating-linear-gradient(90deg,rgba(255,255,255,.04) 0 1px,transparent 1px 64px),repeating-linear-gradient(0deg,rgba(255,255,255,.04) 0 1px,transparent 1px 64px);
 transform:perspective(420px) rotateX(62deg);transform-origin:bottom;-webkit-mask:linear-gradient(transparent,#000 70%)}
.marks{position:absolute;inset:24px;pointer-events:none}
.marks i{position:absolute;width:14px;height:14px;border:0 solid rgba(255,255,255,.18)}
.marks i:nth-child(1){left:0;top:0;border-left-width:1px;border-top-width:1px}
.marks i:nth-child(2){right:0;top:0;border-right-width:1px;border-top-width:1px}
.marks i:nth-child(3){left:0;bottom:0;border-left-width:1px;border-bottom-width:1px}
.marks i:nth-child(4){right:0;bottom:0;border-right-width:1px;border-bottom-width:1px}
.code{font:600 11px/1 var(--mono);letter-spacing:.18em;color:var(--dim);text-transform:uppercase}
.code b{color:var(--ok);font-weight:600}
.h{font-family:var(--cond);font-stretch:condensed;font-weight:700;text-transform:uppercase;letter-spacing:.03em;line-height:.95;margin:0}
.p{color:var(--mut);margin:0}
.lockup{display:flex;align-items:center}
.lockup svg{display:block;flex:none}
.ticks{height:6px;flex:1;opacity:.55;background:repeating-linear-gradient(90deg,#3a3e45 0 1px,transparent 1px 6px)}
/* placas */
.pl{position:relative;isolation:isolate;--c:14px;filter:drop-shadow(0 18px 28px rgba(0,0,0,.5)) drop-shadow(0 2px 3px rgba(0,0,0,.5))}
.pl::before,.pl::after{content:"";position:absolute;z-index:-1;clip-path:polygon(var(--c) 0,100% 0,100% calc(100% - var(--c)),calc(100% - var(--c)) 100%,0 100%,0 var(--c))}
.pl::before{inset:0;background:linear-gradient(180deg,var(--edge),var(--edge2) 22%,rgba(255,255,255,.06))}
.pl::after{inset:1px;background:radial-gradient(circle at calc(100% - 10px) 10px,#33373e 0 1.6px,#0d0e10 2px,transparent 2.6px),
  radial-gradient(circle at 10px calc(100% - 10px),#33373e 0 1.6px,#0d0e10 2px,transparent 2.6px),linear-gradient(180deg,var(--fill0),var(--fill1) 40%,var(--fill2))}
.pl.clay{color:var(--ink)}
.pl.clay::before{background:linear-gradient(180deg,#fbfbf8,#a9aaa6 60%,#8d8e8a)}
.pl.clay::after{background:radial-gradient(circle at calc(100% - 11px) 11px,#8f908c 0 1.8px,#f2f2ef 2.2px,transparent 2.8px),
  radial-gradient(circle at 11px calc(100% - 11px),#8f908c 0 1.8px,#f2f2ef 2.2px,transparent 2.8px),linear-gradient(176deg,var(--clay0) 0%,var(--clay1) 48%,var(--clay2) 100%)}
.clay .code{color:var(--ink2)}
.reflect{-webkit-box-reflect:below 10px linear-gradient(transparent 82%,rgba(255,255,255,.10))}
/* ventana del navegador, en placa */
.win{position:absolute;--c:18px;padding:0 6px 6px}
.wbar{height:36px;display:flex;align-items:center;gap:10px;padding:0 10px 0 22px}
.wbar .sq{width:9px;height:9px;background:#2c3036;box-shadow:inset 0 1px 0 rgba(255,255,255,.08)}
.wbar .sq.on{background:var(--ok);box-shadow:0 0 0 3px rgba(168,207,174,.14),0 0 8px rgba(168,207,174,.6)}
.wbar .url{flex:1;max-width:46%;margin:0 auto;height:22px;display:flex;align-items:center;justify-content:center;gap:8px;font:500 11.5px/1 var(--mono);color:var(--mut);
 background:#0c0d0f;box-shadow:inset 0 0 0 1px #25292f,0 1px 0 rgba(255,255,255,.04);clip-path:polygon(6px 0,100% 0,100% calc(100% - 6px),calc(100% - 6px) 100%,0 100%,0 6px)}
.wbar .url svg{width:11px;height:11px}
.wbar .wc{font:600 9.5px/1 var(--mono);letter-spacing:.18em;color:#62666c;text-transform:uppercase}
.screen{position:relative;display:block;overflow:hidden;box-shadow:0 0 0 1px #050607;clip-path:polygon(0 0,100% 0,100% calc(100% - 12px),calc(100% - 12px) 100%,0 100%)}
.screen img{display:block;width:100%;height:100%;object-fit:cover;object-position:top left}
.screen::after{content:"";position:absolute;inset:0;pointer-events:none;background:linear-gradient(180deg,rgba(255,255,255,.035),transparent 30%)}
/* teléfono */
.phone{position:absolute;--c:22px;padding:14px 9px 16px}
.phone .slot{position:absolute;top:5px;left:50%;width:44px;height:4px;margin-left:-22px;background:#0a0b0d;box-shadow:0 1px 0 rgba(255,255,255,.06)}
.phone .screen{clip-path:polygon(10px 0,100% 0,100% calc(100% - 10px),calc(100% - 10px) 100%,0 100%,0 10px)}
.chip{display:inline-flex;align-items:center;gap:8px;height:28px;padding:0 12px;font:600 11px/1 var(--mono);letter-spacing:.12em;text-transform:uppercase;color:var(--mut);
 box-shadow:inset 0 0 0 1px #2c3036;background:rgba(255,255,255,.02);clip-path:polygon(7px 0,100% 0,100% calc(100% - 7px),calc(100% - 7px) 100%,0 100%,0 7px)}
.chip i{width:7px;height:7px;background:var(--ok);box-shadow:0 0 0 3px rgba(168,207,174,.12)}
.chip.clay{background:linear-gradient(180deg,#e9e9e5,#c3c4c0);color:var(--ink);box-shadow:none}
.chip.clay i{background:#3d8a4b;box-shadow:none}
.foot{position:absolute;font:500 11px/1 var(--mono);letter-spacing:.12em;color:#5d6066;text-transform:uppercase}
`;
const lockSvg='<svg viewBox="0 0 12 12"><path d="M3.5 5.5V4a2.5 2.5 0 0 1 5 0v1.5M2.5 5.5h7v5h-7z" fill="none" stroke="#878a8f" stroke-width="1.1"/></svg>';
const win=(img,{x,y,w,ar,url='localhost:8080',code='',z=0,reflect=true,clip})=>{const sh=Math.round(w*ar);
 return `<div class="pl win${reflect?' reflect':''}" style="left:${x}px;top:${y}px;width:${w+12}px;z-index:${z}">
 <div class="wbar"><span class="sq on"></span><span class="sq"></span><span class="sq"></span><span class="url">${lockSvg}${url}</span><span class="wc">${code}</span></div>
 <div class="screen" style="width:${w}px;height:${clip||sh}px"><img src="${img}" alt=""></div></div>`};
const phone=(img,{x,y,w,ar,z=5})=>`<div class="pl phone reflect" style="left:${x}px;top:${y}px;width:${w+18}px;z-index:${z}"><span class="slot"></span><div class="screen" style="width:${w}px;height:${Math.round(w*ar)}px"><img src="${img}" alt=""></div></div>`;
const marks='<div class="marks"><i></i><i></i><i></i><i></i></div>';
const page=(name,W,H,body,extra='')=>{fs.writeFileSync(OUT+name+'.html',`<!doctype html><html><head><meta charset="utf-8"><style>${CSS}${extra}</style></head><body style="--W:${W}px;--H:${H}px">${body}</body></html>`);return name};

const list=[];
for(const L of ['en','es']){const t=TX[L];const sfx=L=='en'?'':'-es';
 const shot=n=>SH+n+'-'+L+'.png';
 // ---------- social preview 1280×640 ----------
 list.push([page('social-preview'+sfx,1280,640,`<div class="floor" style="--fh:42%"></div>${marks}
 <div style="position:absolute;left:72px;top:88px;width:480px">
  ${lockup(100,46)}
  <div class="code" style="margin-top:40px">${t.kicker}</div>
  <h1 class="h" style="font-size:72px;margin-top:16px">${t.tag1}${t.sj}${t.tag2}<br><span style="color:var(--mut)">${t.tag3s}</span></h1>
  <div style="display:flex;gap:8px;margin-top:36px;flex-wrap:wrap">${t.specs.map((s,i)=>`<span class="chip${i==0?' clay':''}">${i==0?'<i></i>':''}${s}</span>`).join('')}</div>
 </div>
 ${win(shot('bot'),{x:600,y:96,w:900,ar:900/1440,code:'MOD-01'})}
 <div class="foot" style="left:72px;bottom:44px">github.com/eynort/cubit</div>`),1280,640,1]);
 // ---------- hero 1280×600 @2x ----------
 list.push([page('hero'+sfx,1280,600,`<div class="floor" style="--fh:40%"></div>${marks}
 <div style="position:absolute;left:64px;top:70px;width:400px">
  ${lockup(76,35)}
  <div class="code" style="margin-top:34px">${t.kicker}</div>
  <h1 class="h" style="font-size:58px;margin-top:14px">${t.tag1} ${t.tag2}<br><span style="color:var(--mut)">${t.tag3}</span></h1>
  <p class="p" style="margin-top:20px;max-width:380px;font-size:15.5px;line-height:1.55">${t.lead}</p>
  <div style="display:flex;gap:8px;margin-top:26px;flex-wrap:wrap">${t.three.map((s,i)=>`<span class="chip${i==0?' clay':''}">${i==0?'<i></i>':''}${s}</span>`).join('')}</div>
 </div>
 ${win(shot('bot'),{x:476,y:62,w:700,ar:900/1440,code:'MOD-01'})}
 ${phone(shot('mob'),{x:1092,y:212,w:150,ar:844/390})}`),1280,600,2]);
 // ---------- features ----------
 const card=(f,i)=>`<div class="pl${i==0?' clay':''} fcard"><div class="ftop"><div class="fic">${pix(ICON[f.k],64)}</div>
  <div><div class="code">${f.c}</div><h3 class="h" style="font-size:27px;margin-top:9px">${f.h}</h3></div></div><p class="p">${f.p}</p>
  <div class="fx"><span class="ticks"></span><span>${f.x}</span></div></div>`;
 list.push([page('features'+sfx,1280,700,`<div class="floor" style="--fh:30%"></div>${marks}
  <div style="position:absolute;left:64px;top:52px;right:64px;display:flex;align-items:flex-end;gap:32px"><div><div class="code"><b>■</b> ${t.feat_code}</div><h2 class="h" style="font-size:40px;margin-top:12px">${t.feat_h}</h2></div><span class="ticks" style="margin-bottom:8px"></span>${lockup(40,18)}</div>
  <div class="fgrid">${t.f.map(card).join('')}</div>`,`
  .fgrid{position:absolute;left:64px;right:64px;top:168px;bottom:56px;display:grid;grid-template-columns:repeat(3,1fr);grid-template-rows:1fr 1fr;gap:24px}
  .fcard{display:flex;flex-direction:column;padding:22px 26px 22px 24px;--c:18px}
  .ftop{display:flex;align-items:center;gap:20px}
  .fcard>.p{margin-top:14px;font-size:15px;line-height:1.5}
  .fic{flex:none;width:80px;height:80px;display:grid;place-items:center;background:#0c0d0f;box-shadow:inset 0 0 0 1px #25292f,inset 0 10px 24px rgba(0,0,0,.5);
   clip-path:polygon(10px 0,100% 0,100% calc(100% - 10px),calc(100% - 10px) 100%,0 100%,0 10px)}
  .clay .fic{background:#1a1c20;box-shadow:inset 0 0 0 1px #000,inset 0 10px 24px rgba(0,0,0,.6)}
  .clay .p{color:#3c3e42}
  .fx{margin-top:auto;padding-top:14px;display:flex;align-items:center;gap:12px;font:600 11px/1 var(--mono);letter-spacing:.12em;text-transform:uppercase;color:var(--fg)}
  .fx span:last-child{flex:none}
  .clay .fx{color:var(--ink)}
  .clay .ticks{background:repeating-linear-gradient(90deg,#8d8e8a 0 1px,transparent 1px 6px)}`),1280,700,2]);
 // ---------- install ----------
 const step=(s,i)=>`<div class="pl${i==2?' clay':''} st"><div class="num">0${i+1}</div><div class="sb"><h3 class="h" style="font-size:28px">${s.h}</h3><p class="p" style="margin-top:8px">${s.p}</p></div>
  <div class="term">${s.cmd.map(([pr,c])=>`<div><span class="pr">${pr||' '}</span><span class="${pr?'c':'o'}">${c.replace(/(http:\/\/localhost:8080\/\?t=…)/,'<u>$1</u>').replace(/Started$/,'<em>Started</em>')}</span></div>`).join('')}</div></div>`;
 list.push([page('install'+sfx,1280,700,`<div class="floor" style="--fh:30%"></div>${marks}
  <div style="position:absolute;left:64px;top:52px;right:64px;display:flex;align-items:flex-end;gap:32px"><div><div class="code"><b>■</b> ${t.inst_code}</div><h2 class="h" style="font-size:40px;margin-top:12px">${t.inst_h}</h2></div><span class="ticks" style="margin-bottom:8px"></span>${lockup(40,18)}</div>
  <div class="steps">${t.steps.map(step).join('<div class="conn"></div>')}</div>`,`
  .steps{position:absolute;left:64px;right:64px;top:168px;bottom:52px;display:flex;flex-direction:column}
  .conn{height:16px;margin-left:58px;width:1px;background:repeating-linear-gradient(180deg,#3a3e45 0 3px,transparent 3px 6px)}
  .st{flex:1;display:flex;align-items:center;gap:28px;padding:0 24px 0 28px;--c:16px}
  .num{flex:none;width:60px;font:700 44px/1 var(--cond);font-stretch:condensed;color:#3b3f46;letter-spacing:.02em}
  .clay .num{color:#9a9b97}
  .sb{flex:none;width:330px}
  .sb .p{font-size:14.5px;line-height:1.5}
  .clay .sb .p{color:#3c3e42}
  .term{flex:1;align-self:stretch;margin:14px 0;display:flex;flex-direction:column;justify-content:center;gap:6px;padding:0 22px;background:#0b0c0e;
   box-shadow:inset 0 0 0 1px #23272d,inset 0 8px 20px rgba(0,0,0,.45);font:500 15px/1.35 var(--mono);color:var(--fg);white-space:pre;
   clip-path:polygon(0 0,100% 0,100% calc(100% - 10px),calc(100% - 10px) 100%,0 100%)}
  .term .pr{color:var(--ok);margin-right:12px}
  .term .o{color:var(--mut)}
  .term em{font-style:normal;color:var(--ok)}
  .term i{font-style:normal;color:#5d6066}
  .term b{font-weight:500;color:var(--fg)}
  .term u{text-decoration:none;color:var(--fg);box-shadow:inset 0 -1px 0 var(--ok)}
  .clay .term{background:#141619;box-shadow:inset 0 0 0 1px #000,inset 0 8px 20px rgba(0,0,0,.5)}`),1280,700,2]);
 // ---------- runs on ----------
 list.push([page('runs-on'+sfx,1280,440,`<div class="floor" style="--fh:40%"></div>${marks}
  <div style="position:absolute;left:64px;top:52px;right:64px;display:flex;align-items:flex-end;gap:32px"><div><div class="code"><b>■</b> ${t.runs_code}</div><h2 class="h" style="font-size:40px;margin-top:12px">${t.runs_h}</h2><p class="p" style="margin-top:10px;font-size:15px">${t.runs_p}</p></div><span class="ticks" style="margin-bottom:8px"></span>
   <div class="pl clay mc"><div class="code">${t.mcv}</div><div class="h" style="font-size:44px;margin-top:6px">26.2<span style="font-size:26px;margin-left:10px;color:var(--ink2)">${t.mcx}</span></div><div class="code" style="margin-top:8px;letter-spacing:.1em">${t.proto}</div></div></div>
  <div class="plats">${t.plat.map(([k,n])=>`<div class="pl pt"><div class="fic">${pix(ICON[k],64)}</div><div class="code" style="color:var(--fg);letter-spacing:.14em">${n}</div></div>`).join('')}</div>`,`
  .mc{flex:none;padding:18px 26px 18px 24px;--c:14px}
  .plats{position:absolute;left:64px;right:64px;top:216px;bottom:56px;display:grid;grid-template-columns:repeat(6,1fr);gap:16px}
  .pt{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:18px;--c:14px}
  .fic{width:96px;height:96px;display:grid;place-items:center;background:#0c0d0f;box-shadow:inset 0 0 0 1px #25292f,inset 0 10px 24px rgba(0,0,0,.5);
   clip-path:polygon(9px 0,100% 0,100% calc(100% - 9px),calc(100% - 9px) 100%,0 100%,0 9px)}`),1280,440,2]);
 // ---------- capturas del panel (panel-bots, panel-servers, panel-wizard) 1280×800 @2x ----------
 const phead=([c,h,p])=>`<div style="position:absolute;left:64px;top:52px;right:64px;display:flex;align-items:flex-end;gap:32px"><div><div class="code"><b>■</b> ${c}</div><h2 class="h" style="font-size:40px;margin-top:12px">${h}</h2><p class="p" style="margin-top:10px;font-size:15px">${p}</p></div><span class="ticks" style="margin-bottom:8px"></span>${lockup(40,18)}</div>`;
 const AR=900/1440;
 list.push([page('panel-bots'+sfx,1280,800,`<div class="floor" style="--fh:34%"></div>${marks}${phead(t.pan.bots)}
  ${win(shot('list'),{x:58,y:206,w:700,ar:AR,clip:296,code:'MOD-P1',reflect:false})}
  ${win(shot('bot'),{x:470,y:262,w:744,ar:AR,code:'MOD-01',z:2})}`),1280,800,2]);
 list.push([page('panel-servers'+sfx,1280,800,`<div class="floor" style="--fh:34%"></div>${marks}${phead(t.pan.servers)}
  ${win(shot('server'),{x:58,y:206,w:744,ar:AR,code:'MOD-S1',reflect:false})}
  ${win(shot('servers'),{x:470,y:392,w:744,ar:AR,clip:276,code:'MOD-P2',z:2})}`),1280,800,2]);
 list.push([page('panel-wizard'+sfx,1280,800,`<div class="floor" style="--fh:34%"></div>${marks}${phead(t.pan.wizard)}
  ${win(shot('wizard'),{x:58,y:206,w:820,ar:AR,code:'MOD-W1'})}
  ${phone(shot('wizard-mob'),{x:1000,y:262,w:200,ar:844/390})}`),1280,800,2]);
 // ---------- banner con el título de estilo Minecraft (secundaria) ----------
 list.push([page('banner-mc'+sfx,1280,400,`<div class="floor" style="--fh:52%"></div>${marks}
  <img src="../title-mc-style.png" alt="" style="position:absolute;left:64px;top:72px;width:620px;filter:drop-shadow(0 18px 24px rgba(0,0,0,.55))" class="reflect">
  <div style="position:absolute;left:740px;top:84px;right:64px">
   <div class="code"><b>■</b> ${t.kicker}</div>
   <div class="h" style="font-size:44px;margin-top:14px">${t.tag1} ${t.tag2}<br><span style="color:var(--mut)">${t.tag3}</span></div>
   <div class="code" style="margin-top:16px;color:var(--mut);letter-spacing:.12em">${t.banner_s}</div>
   <div style="display:flex;align-items:center;gap:14px;margin-top:30px">${cube(36)}<span class="code" style="color:var(--fg);letter-spacing:.14em">github.com/eynort/cubit</span></div>
  </div>
  <div class="foot" style="left:64px;bottom:40px;text-transform:none;letter-spacing:.04em;font-size:11px;color:#6b6e74">${t.disclaimer}</div>`),1280,400,1]);
}
fs.writeFileSync(P+'list.txt',list.map(r=>r.join(' ')).join('\n')+'\n');
console.log(list.length,'páginas');
