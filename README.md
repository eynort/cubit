# minecraft-bot

**A Minecraft bot that does three things, and does them well: fight, dig and move.** Give it orders in chat (or from a web panel) and it digs out areas layer by layer, follows you or goes where you tell it, and fights mobs and players.

[Español](README.es.md) · [Commands](docs/COMMANDS.md) · [Órdenes](docs/ORDENES.md)

## What it does

- **PvP**: fights one player or everyone in an area, or works as a bodyguard. It never runs from a player: it wins, or walls itself in to heal and comes back. It uses everything it carries: sword and shield with crits, the spear (jabs from out of reach, charges), the mace (with a wind charge under its feet it lands on you), tridents, bows, lava buckets, cobwebs, flint and steel, wind charges near edges and harmful splash potions. Details in [combat/PVP.md](combat/PVP.md).
- **Excavate**: empties a box between two corners, 4 blocks high at a time, never breaking a pickaxe. No chests: it keeps what's valuable and throws away the junk (dirt, stone, gravel…). Give it a chunk written with its edges (Z -48 to -63) and it fits the whole chunk.
- **Move**: goes to coordinates or to a player, and follows you. It plans paths with A*, places blocks for towers and bridges when needed, and saves itself from lava, falls and fire.
- **Vanilla only**: no plugin commands (no /tpa, /home or /login).
- **Small**: one Go binary, no Java or Node, about 150 MB of RAM: it can live on an old Android phone (Termux), a Raspberry Pi or a cheap VPS.

## Commands

| | |
|---|---|
| `/fight <player>` · `/fight in <x> <z> [<x2> <z2>]` | Fight a player, or everyone in an area |
| `/guard <player> [<minutes>]` | Follow a player and attack whoever hits them |
| `/excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact]` | Dig out that box (`/excavate chunk <y1> <y2>`: the chunk it stands in) |
| `/goto <player>` · `/goto <x> <z>` | Go to a player or to coordinates |
| `/follow <player>` | Follow a player until `/stop` |
| `/inventory` · `/drop [<item> [<amount>]] [<player>]` | See what it carries; drop or give things |
| `/stop` · `/status` · `/say` · `/cmd` · `/lang` · `/sprint` · `/help` · `/quit` | The basics |

Owners whisper them: `/msg MyBot follow me`. The full reference is in [docs/COMMANDS.md](docs/COMMANDS.md); it comes from the same descriptions as `/help` and the console autocomplete.

## Getting started

### With Docker (the panel for several bots, plus a test server)

```sh
./up.sh YourName
```

It starts a vanilla 26.2 test server and the web panel. Open the URL it prints: with no bots yet, it walks you through creating the first one (name, server, owner, language). From the panel:

- **Bot**: your bots with their skin heads; start, stop, give orders, delete, and move them to another server.
- **Servers**: the test server (start, stop, restart, players, game rules, `server.properties`) and any external servers you add. Each one tells you how to join from Minecraft.

`./up.sh logs` shows what's going on; `./up.sh down` stops everything.

### A single bot (also on a phone)

```sh
./minecraft-bot --name MyBot --owner YourName play.example.net
./minecraft-bot --monitor :8080 play.example.net   # with the web panel
./minecraft-bot --demo                             # no server: a test world inside the program
```

Next time, `./minecraft-bot` alone joins the last server. Whatever you set is remembered in `bot.json`.

| Flag | |
|---|---|
| `<host[:port]>` or `--server` | The server; without a port, the SRV record's or 25565 |
| `--name`, `--owner A,B` | The bot's name; who may give it orders (`*` = anyone, careful) |
| `--microsoft` | Premium server: join with a Microsoft account (a code, once) |
| `--monitor :8080` | The web panel (protected by a key) |
| `--hub :8080` | The panel for several bots, each in its own process (what Docker uses) |
| `--view 6 --memlimit 150` | Memory: view distance and soft limit (for phones) |

## Play fair

Use it on your own server, or on servers whose rules allow bots: many forbid AFK automation.

Minecraft Java **26.2** (protocol 776).

## Building

```sh
go build .
go test ./...
go run . --gen-docs docs
```

Packages: `automation/` (excavate), `pathing/` (A* and movement), `combat/` (PvP) and `safety/` (lava, falls, fire). `third_party/` vendors a Minecraft protocol library and a bot kit (MIT, derived from [Tnze/go-mc](https://github.com/Tnze/go-mc)). Almost every test runs in a deterministic simulated world, so no server is needed.

## License

MIT. Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.
