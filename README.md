# minecraft-bot

**A survival companion that lives on your phone.** Talk to it in chat and it chops trees, digs out areas layer by layer, mines, farms, builds from a Litematica schematic, stores everything in chests and guards you, all on its own.

[Español](README.es.md) · [Commands](docs/COMMANDS.md) · [Órdenes](docs/ORDENES.md)

<!-- TODO: a 20–30 s GIF here: whisper "dig this out" in chat and watch it clear the box layer by layer -->

## Why this bot

- **One small binary, no Java or Node.** It's written in Go and fits in about 150 MB of RAM, so you can leave it running on an old Android phone (Termux), a Raspberry Pi or a cheap VPS.
- **A teammate, not a macro.** Owners give it orders by whisper (`/msg Bot chop 64 oak`). With an optional local AI (Ollama) or any OpenAI-compatible API, it also chats like a player and turns plain language into commands.
- **Real survival work, done carefully:**
  - It excavates boxes layer by layer and keeps your pickaxes from breaking.
  - It stores loot in chests and crafts a chest when it has none.
  - It respects claimed land: blocks that grow back mean someone else's claim, so it moves on.
  - It escapes lava, saves itself from falls with a water bucket and flies to far places with elytra.
- **Joins any server.** It handles login plugins by itself (AuthMe, nLogin, LibreLogin, VeloAuth…): it registers, saves the password and logs in. It reconnects after restarts and resumes the job it was doing. It can also join premium (online-mode) servers with your Microsoft account.
- **Safe to leave alone.** You can list blocks it never breaks (chests, beds, signs and doors by default), set an area it must stay in and turn off PvP. It sends Discord alerts when it dies, gets stuck or finishes, and there's a web panel to watch and control it from another device.

## Quick start

1. Download the binary for your system from [Releases](../../releases) (Linux, Windows, macOS, Android/Termux).
2. Try it without a server:
   ```sh
   ./minecraft-bot --demo
   ```
   It starts a tiny built-in test world, opens the console and serves the panel at http://127.0.0.1:8080.
3. Join a real server:
   ```sh
   ./minecraft-bot --name My_Bot --owner YourName play.example.net
   ```
   Next time, `./minecraft-bot` alone joins the last server. In game, whisper it: `/msg My_Bot follow me`.

## What it can do

| | |
|---|---|
| `/chop`, `/mine`, `/farm` | Chop trees, mine ores (strip mining, tunnels) and harvest and replant crops |
| `/excavate x1 y1 z1 x2 y2 z2` | Clear a box layer by layer, collecting drops and stashing them in chests |
| `/build` | Build from a Litematica schematic |
| `/goto`, `/follow`, `/guard` | Walk (or fly) anywhere, follow you, or act as your bodyguard |
| `/fight` | Fight a player or everyone in an area (shield, critical hits, edge awareness) |
| `/queue` | Line up jobs: `/queue add chop oak 64`, then `/queue add mine iron 32` |
| `… until got logs 64` | Stop conditions on any job: amount gathered, amount held, time |
| `/drop`, `/inventory`, `/status` | Hand items to a player, see what it carries, see its health and job |
| `/limits` | What it must never do (see below) |

The full reference is in [docs/COMMANDS.md](docs/COMMANDS.md), generated from the same descriptions the console's `/help` and autocomplete use.

## Setup

Everything is a flag, and what you set is remembered in `bot.json`:

| Flag | |
|---|---|
| `<host[:port]>` or `--server` | Server; without a port, SRV record or 25565 |
| `--name`, `--owner A,B` | The bot's name; who may give it orders (`*` = anyone, careful) |
| `--password` | Login-plugin password (otherwise saved, or generated when it registers) |
| `--microsoft` | Premium server: log in with a Microsoft account (device code, once) |
| `--monitor :8080` | Web panel: status, inventory, live log, command box (protected by a key) |
| `--webhook <url>` | Discord or Slack alerts |
| `--area "x1 z1 x2 z2"`, `--pvp off`, `--never-break …` | Safety limits |
| `--ai <url> --ai-model <m>` | Chat AI: Ollama (default `http://127.0.0.1:11434`) or `--ai-api openai` for OpenAI, OpenRouter, Groq, LM Studio… (key in `MCBOT_AI_KEY`). `--ai=` turns it off |
| `--view 6 --memlimit 150` | Memory: view distance and soft memory limit (for phones) |

What the bot learns about each world (chests, paths, build progress, notes for the AI in `ia_servidor.md`) is kept per server in `datos/<server>/`.

### Running on a phone (Termux)

```sh
chmod +x minecraft-bot-*-android-arm64
./minecraft-bot-*-android-arm64 --monitor :8080 play.example.net
```
Then open the panel URL it prints from any browser on your network.

## Be a good player

Use it on your own server, or on servers whose rules allow bots: many forbid AFK automation. It plays by the server's rules as far as it can tell: it backs off from claimed land, and it never registers with an email or solves captchas for you.

Minecraft Java **26.2** (protocol 776).

## Building

```sh
go build .            # Go version from go.mod
go test ./...
go run . --gen-docs docs
```

The packages: `automation/` (jobs), `pathing/` (A* and movement), `combat/`, `safety/` (lava, falls, fire), `elytra/`, `schematic/`. `third_party/` holds a vendored Minecraft protocol library and bot kit (MIT, derived from [Tnze/go-mc](https://github.com/Tnze/go-mc)). Most tests run against a deterministic simulated world (`third_party/go-mc26-kit/bot/simworld`), so they need no server.

The code comments are in Spanish. Contributions in English are welcome.

## License

MIT. Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.
