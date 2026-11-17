# minecraft-bot

**Un compañero de survival que vive en tu móvil.** Le hablas por el chat y tala, excava zonas capa a capa, mina, cultiva, construye desde un schematic de Litematica, guarda todo en cofres y te defiende, él solo.

[English](README.md) · [Órdenes](docs/ORDENES.md) · [Commands](docs/COMMANDS.md)

<!-- PENDIENTE: un GIF de 20–30 s aquí: le dices "excava esto" por el chat y se le ve vaciar la caja capa a capa -->

## Por qué este bot

- **Un solo programa pequeño, sin Java ni Node.** Está hecho en Go y cabe en unos 150 MB de RAM: puedes dejarlo jugando en un Android viejo (Termux), una Raspberry Pi o un VPS barato.
- **Un compañero, no una macro.** Los dueños le mandan órdenes por privado (`/msg Bot chop 64 oak`). Con una IA local opcional (Ollama) o cualquier API compatible con OpenAI, además charla como un jugador y convierte lo que le dices en órdenes.
- **Trabajo de survival de verdad, y con cuidado:**
  - Excava cajas capa a capa sin romper tus picos.
  - Guarda lo que saca en cofres, y si no tiene ninguno, lo fabrica.
  - Respeta la protección del spawn: si los bloques reaparecen, no insiste y se va a otro sitio.
  - Se escapa de la lava, se salva de las caídas con el cubo de agua y vuela con élitros a los sitios lejanos.
- **Solo Minecraft vanilla.** No usa comandos de plugins (nada de /tpa, /home ni /login): todo lo hace con lo que trae el juego. Vuelve a entrar tras un reinicio y retoma el trabajo que tenía. También entra en servidores premium con tu cuenta de Microsoft.
- **Puedes dejarlo solo con confianza.** Puedes decirle qué bloques no romper nunca (por defecto cofres, camas, carteles y puertas), en qué zona quedarse y que no pelee con jugadores. Te avisa por Discord si muere, se atasca o termina, y tiene un panel web para verlo y manejarlo desde otro dispositivo.

## Empezar

1. Descarga el programa para tu sistema desde [Releases](../../releases) (Linux, Windows, macOS, Android/Termux).
2. Pruébalo sin servidor:
   ```sh
   ./minecraft-bot --demo
   ```
   Arranca un mundo de prueba dentro del propio programa, abre la consola y deja el panel en http://127.0.0.1:8080.
3. Entra en un servidor de verdad:
   ```sh
   ./minecraft-bot --name Mi_Bot --owner TuNombre play.ejemplo.net
   ```
   La próxima vez, `./minecraft-bot` sin nada entra al último. En el juego, mándale un privado: `/msg Mi_Bot follow me`.

## Qué sabe hacer

| | |
|---|---|
| `/chop`, `/mine`, `/farm` | Talar, minar menas (en ramas, en túneles) y cosechar y replantar |
| `/excavate x1 y1 z1 x2 y2 z2` | Vaciar una caja capa a capa, recogiendo lo que cae y guardándolo en cofres |
| `/build` | Construir desde un schematic de Litematica |
| `/goto`, `/follow`, `/guard` | Ir (andando o volando) a cualquier sitio, seguirte o hacer de guardaespaldas |
| `/fight` | Pelear con un jugador o con todos los de una zona (escudo, críticos, cuidado con los bordes) |
| `/queue` | Encadenar trabajos: `/queue add chop oak 64` y luego `/queue add mine iron 32` |
| `… until got logs 64` | Condiciones de parada para cualquier trabajo: cantidad recogida, cantidad que lleva, tiempo |
| `/drop`, `/inventory`, `/status` | Dar cosas a un jugador, ver qué lleva, ver su vida y su trabajo |
| `/limits` | Lo que no debe hacer nunca (ver abajo) |

La referencia completa está en [docs/ORDENES.md](docs/ORDENES.md). Se genera desde las mismas descripciones que usan el `/help` y el autocompletado de la consola.

## Configuración

Todo se configura con flags, y lo que pongas se recuerda en `bot.json`:

| Flag | |
|---|---|
| `<host[:puerto]>` o `--server` | El servidor; sin puerto, el del registro SRV o 25565 |
| `--name`, `--owner A,B` | El nombre del bot; quién puede darle órdenes (`*` = cualquiera, cuidado) |
| `--microsoft` | Servidor premium: entrar con una cuenta de Microsoft (un código, una sola vez) |
| `--monitor :8080` | El panel web: estado, inventario, log en directo, caja de órdenes y **Ajustes** (dueños, servidor, IA, límites… sin reiniciar a mano; protegido con una clave) |
| `--webhook <url>` | Avisos a Discord o Slack |
| `--area "x1 z1 x2 z2"`, `--pvp off`, `--never-break …` | Límites de seguridad |
| `--ai <url> --ai-model <m>` | La IA para charlar: Ollama (por defecto `http://127.0.0.1:11434`) o `--ai-api openai` para OpenAI, OpenRouter, Groq, LM Studio… (la clave en `MCBOT_AI_KEY`). `--ai=` la apaga |
| `--view 6 --memlimit 150` | Memoria: distancia de visión y límite blando (para móviles) |

Lo que el bot aprende de cada mundo (cofres, caminos, cómo va la obra, notas para la IA en `ia_servidor.md`) se guarda por servidor en `datos/<servidor>/`.

### En un móvil (Termux)

```sh
chmod +x minecraft-bot-*-android-arm64
./minecraft-bot-*-android-arm64 --monitor :8080 play.ejemplo.net
```
Luego abre la dirección del panel que sale en el log desde cualquier navegador de tu red.

## Juega limpio

Úsalo en tu propio servidor o en los que permiten bots: muchos prohíben automatizar estando AFK. El bot solo usa lo vanilla: no entra en servidores que piden registrarse con un plugin de login.

Minecraft Java **26.2** (protocolo 776).

## Compilar

```sh
go build .            # la versión de Go, la de go.mod
go test ./...
go run . --gen-docs docs
```

Los paquetes: `automation/` (trabajos), `pathing/` (A* y movimiento), `combat/`, `safety/` (lava, caídas, fuego), `elytra/` y `schematic/`. `third_party/` lleva una librería del protocolo de Minecraft y un kit de bots incluidos en el repositorio (MIT, derivados de [Tnze/go-mc](https://github.com/Tnze/go-mc)). Casi todos los tests corren en un mundo simulado determinista (`third_party/go-mc26-kit/bot/simworld`), así que no hace falta servidor.

## Licencia

MIT. No es un producto oficial de Minecraft. No está aprobado por Mojang ni Microsoft, ni asociado con ellos.
