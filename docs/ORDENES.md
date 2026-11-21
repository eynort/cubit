# Órdenes

Se escriben en la consola del bot, en el panel web o por `/msg <bot> <orden>` (solo los dueños). Siempre en inglés; las explicaciones salen en el idioma de `/lang`.

_Generado con `go run . --gen-docs docs`: no lo edites a mano._

## /cmd

`/cmd <command>`

Ejecutar un comando del servidor como jugador (aunque se llame igual que una orden del bot: /cmd help, /cmd time set day)

- `<command>`: el comando del servidor, sin /

## /drop

`/drop [<item> [<amount>] [<player>] | <player>]`

Tirar al suelo todo lo que lleva, o un objeto (todo, o la cantidad que se diga: /drop dirt 10); con un jugador, va hasta él y se lo tira (/drop Alex, /drop dirt 10 Alex)


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

## /fight

`/fight <player> | in <x> <z> [<x2> <z2>]`

Pelear con un jugador, o con todos los de una zona.

- `<player>`: el jugador con el que pelear
- `in <x> <z> [<x2>] [<z2>]`: todos los jugadores dentro de una zona: un punto (radio 8) o dos esquinas

## /follow

`/follow <player>`

Seguir a un jugador sin parar (hasta /stop)


## /goto

`/goto <player> | <x> <y> <z>`

Ir hasta un jugador o hasta el bloque de unas coordenadas (si esa altura es aire o sólido, el suelo más cercano de la columna)

- `<player>`: <x> <y> <z>: unas coordenadas (~ = donde está el bot)
- `<y>`: la coordenada y (la altura de la celda donde ponerse)
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


