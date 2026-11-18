# minecraft-bot

**Un bot de Minecraft que hace tres cosas, y bien: pelear, excavar y moverse.** Le mandas órdenes por el chat (o desde un panel web) y vacía zonas capa a capa, te sigue o va donde le digas, y pelea con mobs y jugadores.

[English](README.md) · [Órdenes](docs/ORDENES.md) · [Commands](docs/COMMANDS.md)

## Qué hace

- **PvP**: pelea con un jugador o con todos los de una zona (escudo, críticos, arco, perlas, pociones, cuidado con los bordes), o hace de guardaespaldas.
- **Excavar**: vacía una caja entre dos esquinas, de 4 en 4 bloques de alto, sin romper nunca un pico. No usa cofres: se queda lo valioso y tira la basura (tierra, piedra, grava…). Si le das un chunk escrito con sus bordes (Z -48 a -63), lo ajusta al chunk entero.
- **Moverse**: va a coordenadas o hasta un jugador, y te sigue. Busca el camino con A*, pone bloques para hacer torres y puentes si hace falta, y se salva de la lava, las caídas y el fuego.
- **Solo vanilla**: no usa comandos de plugins (ni /tpa, ni /home, ni /login).
- **Pequeño**: un solo programa en Go, sin Java ni Node, que cabe en unos 150 MB de RAM: puede vivir en un Android viejo (Termux), una Raspberry Pi o un VPS barato.

## Órdenes

| | |
|---|---|
| `/fight <jugador>` · `/fight in <x> <z> [<x2> <z2>]` | Pelear con un jugador, o con todos los de una zona |
| `/guard <jugador> [<minutos>]` | Seguir a un jugador y atacar a quien le pegue |
| `/excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact]` | Vaciar esa caja (`/excavate chunk <y1> <y2>`: el chunk donde está) |
| `/goto <jugador>` · `/goto <x> <z>` | Ir hasta un jugador o unas coordenadas |
| `/follow <jugador>` | Seguir a un jugador hasta `/stop` |
| `/inventory` · `/drop [<objeto> [<cantidad>]] [<jugador>]` | Ver lo que lleva; tirar o dar cosas |
| `/stop` · `/status` · `/say` · `/cmd` · `/lang` · `/sprint` · `/help` · `/quit` | Lo básico |

Los dueños se las mandan por privado: `/msg MiBot follow me`. La referencia completa está en [docs/ORDENES.md](docs/ORDENES.md); sale de las mismas descripciones que el `/help` y el autocompletado.

## Empezar

### Con Docker (el panel para varios bots y un servidor de pruebas)

```sh
./up.sh TuNombre
```

Levanta un servidor de pruebas vanilla 26.2 y el panel web. Abre la URL que te da: si no tienes bots, te pregunta cómo crear el primero (nombre, servidor, dueño, idioma). Desde el panel:

- **Bot**: tus bots con la cabeza de su skin; arrancarlos, pararlos, darles órdenes, eliminarlos y moverlos de servidor.
- **Servidores**: el de pruebas (encender, apagar, reiniciar, jugadores, reglas del juego, `server.properties`) y los externos que añadas. Cada uno te dice cómo entrar desde Minecraft.

`./up.sh logs` enseña lo que pasa; `./up.sh down` lo para todo.

### Un bot suelto (también en un móvil)

```sh
./minecraft-bot --name MiBot --owner TuNombre play.ejemplo.net
./minecraft-bot --monitor :8080 play.ejemplo.net   # con el panel web
./minecraft-bot --demo                             # sin servidor: un mundo de prueba dentro del programa
```

La próxima vez, `./minecraft-bot` sin nada entra al último servidor. Lo que pongas se recuerda en `bot.json`.

| Flag | |
|---|---|
| `<host[:puerto]>` o `--server` | El servidor; sin puerto, el del registro SRV o 25565 |
| `--name`, `--owner A,B` | El nombre del bot; quién puede darle órdenes (`*` = cualquiera, cuidado) |
| `--microsoft` | Servidor premium: entrar con una cuenta de Microsoft (un código, una sola vez) |
| `--monitor :8080` | El panel web (protegido con una clave) |
| `--hub :8080` | El panel para varios bots, cada uno en su proceso (lo que usa Docker) |
| `--view 6 --memlimit 150` | Memoria: distancia de visión y límite blando (para móviles) |

## Juega limpio

Úsalo en tu propio servidor o en los que permiten bots: muchos prohíben automatizar estando AFK.

Minecraft Java **26.2** (protocolo 776).

## Compilar

```sh
go build .
go test ./...
go run . --gen-docs docs
```

Los paquetes: `automation/` (excavar), `pathing/` (A* y movimiento), `combat/` (PvP) y `safety/` (lava, caídas, fuego). `third_party/` lleva una librería del protocolo de Minecraft y un kit de bots incluidos en el repositorio (MIT, derivados de [Tnze/go-mc](https://github.com/Tnze/go-mc)). Casi todos los tests corren en un mundo simulado determinista, así que no hace falta servidor.

## Licencia

MIT. No es un producto oficial de Minecraft. No está aprobado por Mojang ni Microsoft, ni asociado con ellos.
