# Cubit guide

[Español](GUIA.md) · [All commands](COMMANDS.md)

## 1. Run it

### With Docker: the panel and a test server

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
./up.sh YourMinecraftName
```

That one command starts two things:

- **The web panel**, where you manage your bots. The link it prints already includes your access key.
- **A test server**: vanilla Minecraft Java 26.2, with you as op. Join it from Minecraft: *Multiplayer → Direct Connection → `localhost:25565`*.

| | |
|---|---|
| `./up.sh logs` | watch what is happening |
| `./up.sh down` | stop everything |
| `.env` | ports, your name, the test server on or off |

### Without Docker: just the binary

Download the file for your system from [Releases](../../../releases) and run it:

```sh
./cubit --name MyBot --owner YourName play.example.net   # join a server
./cubit --monitor :8080 play.example.net                 # the same, plus the web panel
./cubit --demo                                           # try it with no server at all
```

On Linux and macOS, run `chmod +x cubit` first. Next time, plain `./cubit` rejoins the last server.

On **Android**, install Termux and pick the `android-arm64` file. Add `--view 4 --memlimit 120` to keep it light.

## 2. The panel

- **Bot**: your bots, each shown with its skin head. Open one to start or stop it, change its settings, or move it to another server.
- **Each bot has its own console.** Type there to control it, with autocomplete:
  - `/follow YourName` is an order for the bot;
  - `/time set day` is a server command (if the bot is op);
  - `hello` without a slash is a message in the game chat.
  - `Tab` completes, `↑` and `↓` go through the history.
- **Servers**: start, stop and restart the test server, see who is online, and add external servers. Each server card tells you how to join it. Under *World* you can remake the test world with any seed, or a random one: the old one is kept as `world.anterior`.

## 3. Control the bot

You can give it orders in two ways:

| From | How |
|---|---|
| The panel | type in the bot's console: `/follow YourName` |
| The game | whisper it as an owner: `/msg MyBot follow YourName` |

The orders you will use most:

| | |
|---|---|
| `/follow <player>` · `/goto <x> <y> <z>` | follow you, or go somewhere |
| `/fight <player>` · `/guard <player>` | fight someone, or guard someone |
| `/excavate <x1> <y1> <z1> <x2> <y2> <z2>` | dig out that box |
| `/excavate chests <x> <y> <z>` | where it stores the loot and finds pickaxes |
| `/inventory` · `/drop` | what it carries; give it to you |
| `/queue <command>` · `a ; b` | a to-do list: tasks that run one after another |
| `/stop` | stop whatever it is doing (and pause the queue) |

The full list is in [COMMANDS.md](COMMANDS.md). `/help` in the console shows it too.

To control the bots from scripts or other programs (list them, send commands, follow chat and tasks live), see the [HTTP API](API.md).

## 4. Good to know

- **Owners**: only the owners may give orders in game. Set them with `--owner A,B`, or in the bot's settings in the panel.
- **Premium servers**: add `--microsoft`. The first time it shows a code to sign in with your Microsoft account.
- **Memory**: about 70 MB per bot. `--view` (view distance in chunks) and `--memlimit` (in MB) lower it for phones.
- **Version**: Minecraft Java **26.2** only (protocol 776), and vanilla servers only: it uses no plugin commands.
- **Play fair**: use it on your own server, or on servers that allow bots.
