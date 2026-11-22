# Órdenes

Se escriben en la consola del bot, en el panel web o por `/msg <bot> <orden>` (solo los dueños). Siempre en inglés; las explicaciones salen en el idioma de `/lang`.

_Generado con `go run . --gen-docs docs`: no lo edites a mano._

## /afk

`/afk [<x>] [<y>] [<z>]`

Quedarse en un sitio las 24 horas hasta /stop (aquí, o en las coordenadas: primero va andando): pequeños gestos al azar cada 1-4 minutos para que el servidor no lo eche por inactivo, vuelve al sitio si lo empujan, come con hambre, se defiende de los mobs que se acercan, duerme en una cama a menos de 16 bloques de noche o con tormenta, y vuelve al sitio tras morir o reconectar.

- `<x>` (opcional): el sitio (~ = donde está el bot), o here
- `<y>` (opcional): el sitio (~ = donde está el bot), o here
- `<z>` (opcional): el sitio (~ = donde está el bot), o here

**Ejemplos**

```
/afk               # quedarse donde está, hasta /stop
/afk here          # lo mismo
/afk 120 64 -340   # ir a ese bloque y quedarse
/afk ~ ~ ~         # el bloque donde está ahora
```

## /chop

`/chop <woods> <x> <y> <z>`

Talar árboles enteros de las maderas dichas (oak, birch, spruce, jungle, acacia, dark_oak, mangrove, cherry, pale_oak, y crimson/warped en el Nether), separadas por comas (/chop oak,birch 100 64 200; ~ = donde está el bot): va hasta el punto y tala todos los árboles de esas maderas a menos de 24 bloques de él, todos los troncos (también los altos: el pathfinding pone bloques para subir y luego se quitan) y recoge lo que cae; no deja ningún tronco flotando. Solo tala árboles (con hojas naturales alrededor, nada construido pegado a los troncos): las construcciones de troncos de jugadores y los troncos pelados no se tocan. Usa la mejor hacha que lleve, sin romperla (sin hacha, a mano). Al acabar deja en el log cuántos árboles y troncos de cada madera.

- `<woods>`: una o más maderas, separadas por comas (oak,birch)
- `<x>`: el punto alrededor del que talar (~ = donde está el bot)
- `<y>`: el punto alrededor del que talar (~ = donde está el bot)
- `<z>`: el punto alrededor del que talar (~ = donde está el bot)

**Ejemplos**

```
/chop oak ~ ~ ~                 # todos los robles a menos de 24 bloques de donde está
/chop oak,birch 100 64 200      # robles y abedules alrededor de ese punto (y = el suelo)
/chop dark_oak,pale_oak ~ ~ ~   # también los árboles grandes de 2×2
/chop crimson,warped ~ ~ ~      # tallos del Nether
```

## /cmd

`/cmd <command>`

Ejecutar un comando del servidor como jugador (aunque se llame igual que una orden del bot: /cmd help, /cmd time set day)

- `<command>`: el comando del servidor, sin /

**Ejemplos**

```
/cmd time set day   # el bot tiene que ser op
/cmd tp Steve
```

## /drop

`/drop [<item> [<amount>] [<player>] | <player>]`

Tirar al suelo todo lo que lleva, o un objeto (todo, o la cantidad que se diga: /drop dirt 10); con un jugador, va hasta él y se lo tira (/drop Alex, /drop dirt 10 Alex)


**Ejemplos**

```
/drop                   # todo, al suelo
/drop diamond           # todos sus diamantes
/drop cobblestone 32    # 32 de piedra
/drop Steve             # todo, tirado a Steve
/drop diamond 5 Steve   # 5 diamantes, tirados a Steve
```

## /excavate

`/excavate <x> <y> <z> <x2> <y2> <z2> [<exact>] | chunk <y1> <y2> | chests [<x>] [<y>] [<z>]`

Vaciar una caja de bloques entre dos esquinas (/excavate 10 70 10 30 50 30), de 4 en 4 bloques de alto: cada pasada de 4 en toda la caja antes de bajar a la siguiente (el terreno baja parejo); en cada pasada se pone en el fondo y pica los 4 de alto que alcanza alrededor, y sigue, y al acabarla recoge lo que cayó. Usa los picos que lleve, sin romper ninguno (si no le queda ninguno, lo pide por el chat). Cuando se llena tira la basura (tierra, piedra, grava…) y se queda lo valioso; si le das cofres (/excavate chests <x> <y> <z>), lo valioso lo lleva ahí.

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
- `chests [<x>] [<y>] [<z>]`: dónde guarda /excavate (solo /excavate) lo valioso: <x> <y> <z> apunta un cofre (o barril, o caja de shulker; de uno doble vale cualquier mitad), list los enseña, remove <x> <y> <z> olvida uno, clear los olvida todos. Se recuerdan tras reiniciar. Al llenarse, primero tira la basura; si sigue lleno, va al cofre más cercano y guarda lo valioso (nunca sus herramientas, armadura, comida ni equipo de pelea), al siguiente si uno está lleno, y otra vez al acabar
  - `<x>` (opcional): el cofre (~ = donde está el bot), o list / clear / remove
  - `<y>` (opcional): el cofre (~ = donde está el bot), o list / clear / remove
  - `<z>` (opcional): el cofre (~ = donde está el bot), o list / clear / remove

**Ejemplos**

```
/excavate 10 70 10 30 50 30            # la caja entre esas dos esquinas
/excavate ~ ~ ~ ~15 ~-20 ~15           # un hoyo de 16×21×16 desde donde está
/excavate 0 60 0 15 40 15 exact        # justo esos bloques (sin ajustar a chunks)
/excavate chunk 0 64                   # el chunk entero donde está, de y 0 a 64
/excavate chests 104 103 -224          # guardar ahí lo que saque (cuenta la pared de cofres entera)
/excavate chests list                  # ver los cofres
/excavate chests remove 104 103 -224
/excavate chests clear                 # olvidarlos todos
```

## /fight

`/fight <player> | in <x> <z> [<x2> <z2>]`

Pelear con un jugador, o con todos los de una zona.

- `<player>`: el jugador con el que pelear
- `in <x> <z> [<x2>] [<z2>]`: todos los jugadores dentro de una zona: un punto (radio 8) o dos esquinas

**Ejemplos**

```
/fight Alex           # pelear con Alex hasta que uno muera
/fight in 100 200     # todos a menos de 8 bloques de 100 200
/fight in 0 0 50 50   # todos dentro de ese cuadrado
```

## /follow

`/follow <player>`

Seguir a un jugador sin parar (hasta /stop)


**Ejemplos**

```
/follow Steve
```

## /goto

`/goto <player> | <x> <y> <z>`

Ir hasta un jugador o hasta el bloque de unas coordenadas (si esa altura es aire o sólido, el suelo más cercano de la columna)

- `<player>`: <x> <y> <z>: unas coordenadas (~ = donde está el bot)
- `<y>`: la coordenada y (la altura de la celda donde ponerse)
- `<z>`: la coordenada z

**Ejemplos**

```
/goto Steve         # ir hasta Steve
/goto 120 64 -340   # ir a ese bloque
/goto ~50 ~ ~       # 50 bloques al este de donde está
```

## /guard

`/guard <player> [<minutes>]`

Guardaespaldas: sigue a un jugador y ataca a quien le pegue (hasta /stop, o los minutos que se digan)

- `<minutes>` (opcional): cuántos minutos (sin ello, hasta /stop)

**Ejemplos**

```
/guard Steve      # hasta /stop
/guard Steve 30   # durante 30 minutos
```

## /help

`/help`

Esta ayuda.


**Ejemplos**

```
/help
```

## /inventory

`/inventory [<grid>]`

Lo que lleva el bot: el equipo, la barra casilla a casilla y el inventario; grid: como la pantalla del juego.

- `<view>` (opcional)
  - `grid`: como la pantalla del juego (ancha)

**Ejemplos**

```
/inventory
/inventory grid   # como en la pantalla del juego
```

## /lang

`/lang <en|es>`

Idioma de las explicaciones (las órdenes siempre en inglés)

- `<language>`
  - `en`: inglés
  - `es`: español

**Ejemplos**

```
/lang es
/lang en
```

## /mine

`/mine <ores> <x> <y> <z> [every <n>] [total <n>]`

Minar minerales como un jugador de verdad (strip mining, sin xray): desde donde está baja por una escalera a la mejor capa del mineral, cava un túnel principal de 1×2 con una rama a cada lado cada 3 bloques, y solo pica lo que vería un jugador (mineral que toca el aire del túnel, y el resto de una veta cuando se ve). Nunca rompe un pico: los coge del cofre y, si no le quedan, vuelve a por más (o los pide por el chat). Cada 8 minerales (every <n>) vuelve andando al cofre, deja los minerales y lo valioso (nunca herramientas ni comida) y regresa a la mina. /stop lo para; /status dice qué hace. Al acabar da un resumen.

- `<ores>`: qué minar: uno, o varios separados por comas (diamond,iron); en el Nether, ancient_debris y quartz
- `<x>`: el cofre (o una pared de cofres: vale uno cualquiera) de donde coge picos y donde deja los minerales (~ = donde está el bot)
- `<y>`: el cofre (o una pared de cofres: vale uno cualquiera) de donde coge picos y donde deja los minerales (~ = donde está el bot)
- `<z>`: el cofre (o una pared de cofres: vale uno cualquiera) de donde coge picos y donde deja los minerales (~ = donde está el bot)
- `<option>` (opcional)
  - `every`: dejar los minerales en el cofre cada <n> (por defecto 8)
  - `total`: acabar al juntar <n> minerales (por defecto: hasta /stop)
- `<n>` (opcional): cuántos

**Ejemplos**

```
/mine diamond 104 103 -224              # minar diamantes, con el cofre de ahí (los deja cada 8)
/mine iron,coal 104 103 -224 every 16   # hierro y carbón a la vez, dejándolos cada 16
/mine diamond ~ ~ ~ total 12            # el cofre está donde está él; acaba al juntar 12 diamantes
/mine ancient_debris 104 103 -224       # en el Nether: escombros ancestrales en y=15
```

## /quit

`/quit`

Desconectar y salir.


**Ejemplos**

```
/quit   # desconectar y cerrar el bot
```

## /say

`/say <text>`

Escribir en el chat (o escribe sin /)

- `<text>`: lo que escribir

**Ejemplos**

```
/say hello everyone
hello everyone        # lo mismo: el texto sin / va al chat
```

## /sprint

`/sprint <on|off>`

Permitir o prohibir correr.

- `<mode>`
  - `off`: siempre camina
  - `on`: puede correr

**Ejemplos**

```
/sprint off   # andar siempre
/sprint on
```

## /status

`/status`

Vida, comida, posición y combate.


**Ejemplos**

```
/status
```

## /stop

`/stop`

Dejar de pelear y de caminar (y pausar la cola)


**Ejemplos**

```
/stop
```

