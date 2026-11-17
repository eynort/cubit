# Órdenes

Se escriben en la consola del bot, en el panel web o por `/msg <bot> <orden>` (solo los dueños). Siempre en inglés; las explicaciones salen en el idioma de `/lang`.

_Generado con `go run . --gen-docs docs`: no lo edites a mano._

## /ai

`/ai <message> | info | reset [<player>] | compact [<player>] | on | off`

Hablar con la IA (gemma4 en Ollama), o: info, reset [jugador], compact [jugador], on, off.

- `<message>`: lo que decirle
- `info`: modelo, contexto y conversaciones
- `reset [<player>]`: olvidar una conversación (la tuya sin jugador)
- `compact [<player>]`: resumir una conversación ya
- `on`: contestar los /msg con la IA
- `off`: los /msg son solo órdenes

## /around

`/around`

Escribir en el log los bloques alrededor del bot (capa a capa)


## /base

`/base <here|info>`

El almacén: "here" pone la base de los cofres donde está el bot; "info" la muestra.

- `<action>`
  - `here`: la base donde está el bot
  - `info`: dónde está y sus cofres

## /beds

`/beds [<amount>]`

Hacer camas (lana de las ovejas: esquila las de alrededor y las del corral; cuida la granja si falta)

- `<amount>` (opcional): cuántas camas (8 si no se dice)

## /build

`/build load <file> [<x> <y> <z> | me | <player>] [none|cw_90|180|ccw_90] [none|left_right|front_back] | origin <x> <y> <z> | rotate <r> | mirror <m> | import <placements.json> | info | check | materials | chest [add|remove [<x> <y> <z>] | list | clear] | start [noclear] | list`

Construir un schematic (Litematica .litematic, WorldEdit .schem, estructura .nbt) con los materiales de unos cofres: load <archivo> [<x> <y> <z> | here | <jugador>] [rotación] [espejo], origin, rotate, mirror, import <.json de colocaciones de Litematica>, info, check, materials, chest add|remove|list|clear, start [noclear], list.

- `load <file> [<x>] [<y>] [<z>] [<none|90|180|270>] [<none|left_right|front_back>]`: cargar un schematic (y dónde va)
  - `<file>`: un schematic de schematics/
  - `<rotation>` (opcional)
    - `180`: media vuelta
    - `270`: un cuarto de vuelta a la izquierda
    - `90`: un cuarto de vuelta a la derecha
    - `none`: tal cual
  - `<mirror>` (opcional)
    - `front_back`: de delante a atrás
    - `left_right`: de izquierda a derecha
    - `none`: sin espejo
- `origin <x> <y> <z>`: dónde va: <x> <y> <z>, here o un jugador
- `rotate <none|90|180|270>`: girarlo
  - `<rotation>`
    - `180`: media vuelta
    - `270`: un cuarto de vuelta a la izquierda
    - `90`: un cuarto de vuelta a la derecha
    - `none`: tal cual
- `mirror <none|left_right|front_back>`: reflejarlo
  - `<mirror>`
    - `front_back`: de delante a atrás
    - `left_right`: de izquierda a derecha
    - `none`: sin espejo
- `import <file.json> [<name>]`: las colocaciones de un .json de Litematica
  - `<file.json>`: el archivo .json
  - `<name>` (opcional): qué colocación
- `info`: qué está cargado y dónde
- `check`: qué está hecho y qué falta
- `materials`: los materiales que necesita y los de los cofres
- `chest <add|remove|list|clear> [<x>] [<y>] [<z>]`: los cofres de los materiales
  - `<action>`
    - `add`: marcar el más cercano (o <x> <y> <z>)
    - `clear`: olvidarlos todos
    - `list`: enseñarlos
    - `remove`: olvidar uno
- `start [<noclear>]`: empezar a construir
  - `<noclear>` (opcional)
    - `noclear`: sin despejar la zona antes
- `list`: los schematics de schematics/

## /chests

`/chests [<on|off>]`

Usar cofres para guardar lo que saca (sí por defecto); off: trabaja con lo que le quepa encima (se guarda en bot.json)

- `<mode>` (opcional)
  - `off`: no usar cofres nunca
  - `on`: guardar en cofres; si no hay, lo dice y sigue

## /chop

`/chop <wood> [<amount>]`

Talar: con la mejor hacha que lleve; sin ninguna, primero se hace una de piedra; luego la base (mesa y cofre doble)

- `<wood>`: qué madera
- `<amount>` (opcional): cuántos troncos (sin ello, hasta /stop)

## /cmd

`/cmd <command>`

Ejecutar un comando del servidor como jugador (aunque se llame igual que una orden del bot: /cmd help, /cmd time set day)

- `<command>`: el comando del servidor, sin /

## /debug

`/debug`

Escribir en el log de sesión (--logs) dónde está cada parte del bot (las pilas de las goroutines)


## /diag

`/diag`

Prueba saltos y movimientos y cuenta correcciones del servidor.


## /drop

`/drop [<item> [<amount>] [<player>] | <player>]`

Tirar al suelo todo lo que lleva, o un objeto (todo, o la cantidad que se diga: /drop dirt 10); con un jugador, va hasta él y se lo tira (/drop Alex, /drop dirt 10 Alex)


## /elytra

`/elytra <on|off|wear>`

On/off: dejar que /goto vuele con élitros y cohetes cuando está lejos (sí por defecto); wear: ponerse los élitros ya.

- `<mode>`
  - `off`: /goto siempre a pie
  - `on`: /goto puede volar si está lejos
  - `wear`: ponerse los élitros ya

## /excavate

`/excavate <x> <y> <z> <x2> <y2> <z2> [<exact>] | chunk <y1> <y2>`

Vaciar una caja de bloques entre dos esquinas (/excavate 10 70 10 30 50 30), de 4 en 4 bloques de alto: cada pasada de 4 en toda la caja antes de bajar a la siguiente (el terreno baja parejo); en cada pasada se pone en el fondo y pica los 4 de alto que alcanza alrededor, y sigue, y al acabarla recoge lo que cayó. Usa los picos que lleve y los del cofre de minar (/mine chest) o del almacén, sin romper ninguno, y cuando se llena lleva lo cavado a los cofres.

- `<x>`: una esquina (~ = donde está el bot)
- `<y>`: una esquina (~ = donde está el bot)
- `<z>`: una esquina (~ = donde está el bot)
- `<x2>`: la esquina opuesta
- `<y2>`: la esquina opuesta
- `<z2>`: la esquina opuesta
- `<exact>` (opcional)
  - `exact`: justo esos bloques (sin ello, una caja de chunks enteros escrita con los bordes se ajusta a los chunks)
- `chunk <y1> <y2>`: el chunk entero de 16×16 donde está el bot (o el del bloque x z), de y1 a y2: los bordes del chunk los calcula él
  - `<y1>`: desde esta altura
  - `<y2>`: hasta esta altura

## /farm

`/farm`

Granja junto a la base: campo de trigo y corral de ovejas (las mete, las cría y las esquila) — hasta /stop.


## /fight

`/fight <player> | in <x> <z> [<x2> <z2>]`

Pelear con un jugador, o con todos los de una zona.

- `<player>`: el jugador con el que pelear
- `in <x> <z> [<x2>] [<z2>]`: todos los jugadores dentro de una zona: un punto (radio 8) o dos esquinas

## /follow

`/follow <player>`

Seguir a un jugador sin parar (hasta /stop)


## /foodchest

`/foodchest [add [<x> <y> <z>] | remove [<x> <y> <z>] | list | clear | now]`

Cofres de los que el bot puede coger comida (además de su almacén y los de junto a la base): add marca el más cercano (o <x> <y> <z>), remove/clear los olvida, list los muestra, now va ya a por comida. Sin comida va allí antes de cazar, y coge solo lo que necesita.

- `add [<x>] [<y>] [<z>]`: marcar un cofre de comida
  - `<x>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
  - `<y>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
  - `<z>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
- `remove [<x>] [<y>] [<z>]`: olvidar un cofre de comida
  - `<x>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
  - `<y>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
  - `<z>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
- `list`: enseñarlos
- `clear`: olvidarlos todos
- `now`: ir ya a por comida

## /goto

`/goto <player> | <x> <z>`

Ir hasta un jugador o hasta unas coordenadas.

- `<player>`: <x> <z>: unas coordenadas (~ = donde está el bot)
- `<z>`: la coordenada z

## /guard

`/guard <player> [<minutes>]`

Guardaespaldas: sigue a un jugador y ataca a quien le pegue (hasta /stop, o los minutos que se digan)

- `<minutes>` (opcional): cuántos minutos (sin ello, hasta /stop)

## /help

`/help`

Esta ayuda.


## /inventory

`/inventory [<grid>]`

Lo que lleva el bot: el equipo, la barra casilla a casilla y el inventario; grid: como la pantalla del juego.

- `<view>` (opcional)
  - `grid`: como la pantalla del juego (ancha)

## /lang

`/lang <en|es>`

Idioma de las explicaciones (las órdenes siempre en inglés)

- `<language>`
  - `en`: inglés
  - `es`: español

## /limits

`/limits [<what>] [<value>]`

Lo que el bot no hace nunca: bloques que no rompe, la zona donde puede picar, PvP (se guarda en bot.json)

- `<what>` (opcional)
  - `allow`: allow <bloque>: puede romperlo
  - `area`: area <x1> <z1> <x2> <z2> | area off
  - `deny`: deny <bloque>: no romperlo nunca
  - `pvp`: pvp on|off
  - `reset`: los límites de siempre

## /mem

`/mem`

Memoria, GC, chunks cargados y entidades (lo mismo que --monitor)


## /mine

`/mine <ore[,ore…]> [<amount> | all] | chest [<x> <y> <z> | off | list] | list`

Minar por strip mining, cada mineral con su estrategia (altura, separación y largo de las galerías): /mine diamond, /mine iron,copper 200, /mine debris. /mine list enseña las estrategias. /mine chest marca el cofre de su dimensión (uno por dimensión): de ahí coge picos (sin romper ninguno), comida y armadura y ahí deja lo minado; si en la dimensión del mineral no hay, va al de la otra. Sin pico que valga se hace uno.

- `<ores>`
  - `coal`: carbón: mundo normal, y=44, galerías cada 6
  - `copper`: cobre: mundo normal, y=48, galerías cada 6
  - `debris`: escombros ancestrales: Nether, y=15, galerías cada 3
  - `diamond`: diamantes: mundo normal, y=-53, galerías cada 3
  - `gold`: oro: mundo normal, y=-16, galerías cada 4
  - `iron`: hierro: mundo normal, y=16, galerías cada 5
  - `lapis`: lapislázuli: mundo normal, y=0, galerías cada 4
  - `quartz`: cuarzo: Nether, y=15, galerías cada 5
  - `redstone`: redstone: mundo normal, y=-53, galerías cada 3
- `<amount>` (opcional): cuántos (sin ello, o all: hasta /stop)
  - `all`: hasta /stop
- `chest [<x>] [<y>] [<z>]`: marcar el cofre de minar de esta dimensión (el más cercano, o <x> <y> <z>); off lo olvida; list los enseña
  - `<x>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
    - `list`: ver los cofres de minar
    - `off`: olvidar el cofre de esta dimensión
  - `<y>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
  - `<z>` (opcional): <x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)
- `list`: la estrategia de cada mineral (altura, separación, por qué)

## /protected

`/protected list | here | clear`

Sitios donde el servidor repuso lo que el bot rompió (la protección del spawn): a 16 bloques de ellos el bot no cava, no construye ni busca árboles. list los muestra, here olvida los de alrededor del bot, clear los olvida todos.

- `list`: enseñarlos
- `here`: olvidar los de alrededor del bot
- `clear`: olvidarlos todos

## /queue

`/queue list | add <command> | insert <position> <command> | remove <n> | move <from> <to> | up <n> | down <n> | skip | clear | pause | resume`

La cola de órdenes (goto, guard, fight, chop, mine, beds, farm, build): ver, añadir, quitar, reordenar, saltar, pausar.

- `list`: ver la cola
- `add <command>`: añadir una orden al final
- `insert <position> <command>`: poner una orden en una posición
  - `<position>`: dónde (1 = la siguiente)
- `remove <n>`: quitar una
  - `<n>`: su posición (mira /queue)
- `move <from> <to>`: mover una
  - `<from>`: su posición
  - `<to>`: la posición nueva
- `up <n>`: un puesto antes
  - `<n>`: su posición
- `down <n>`: un puesto después
  - `<n>`: su posición
- `skip`: parar la que va y pasar a la siguiente
- `clear`: quitar todas las pendientes
- `pause`: no empezar las siguientes
- `resume`: seguir

## /quit

`/quit`

Desconectar y salir.


## /say

`/say <text>`

Escribir en el chat (o escribe sin /)

- `<text>`: lo que escribir

## /sprint

`/sprint <on|off>`

Permitir o prohibir correr.

- `<mode>`
  - `off`: siempre camina
  - `on`: puede correr

## /status

`/status`

Vida, comida, posición y combate.


## /stop

`/stop`

Dejar de pelear y de caminar (y pausar la cola)


