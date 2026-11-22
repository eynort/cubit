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
| `/stop` | stop whatever it is doing |

The full list is in [COMMANDS.md](COMMANDS.md). `/help` in the console shows it too.

## 4. Good to know

- **Owners**: only the owners may give orders in game. Set them with `--owner A,B`, or in the bot's settings in the panel.
- **Premium servers**: add `--microsoft`. The first time it shows a code to sign in with your Microsoft account.
- **Memory**: about 70 MB per bot. `--view` (view distance in chunks) and `--memlimit` (in MB) lower it for phones.
- **Version**: Minecraft Java **26.2** only (protocol 776), and vanilla servers only: it uses no plugin commands.
- **Play fair**: use it on your own server, or on servers that allow bots.

## 5. Updating

The hub checks GitHub for a newer release when it starts and then every 24 hours (it sends nothing about you: a plain request, no account, no identifiers). When there is one, the panel shows **Update available** with a link to the release notes. You can turn the check off, or run it now, in *Bots* → *Check for updates*.

What the notice offers depends on how you installed Cubit:

| Installed with | Update with |
|---|---|
| Install script, or the zip (Linux, Windows, Termux) | **Update now** in the panel (see below) |
| `.deb` | download the new `.deb` from [Releases](../../../releases), then `sudo apt install ./cubit_<version>_<arch>.deb` |
| `.rpm` | download the new `.rpm`, then `sudo dnf install ./cubit_<version>_<arch>.rpm` |
| Scoop | `scoop update cubit` |
| winget | `winget upgrade Cubit` |
| Docker (`up.sh`) | `./up.sh update`: `git pull --ff-only`, rebuilds only the bot image and restarts only the hub (the bots reconnect after a few seconds; the Minecraft world is never touched) |

**Update now** downloads the file for your system, checks its SHA-256 against `SHA256SUMS`, and checks the ed25519 signature of `SHA256SUMS` against the public key built into Cubit. Only then does it back up `hub.json` and every `bots/*/bot.json` to `backups/<old version>/`, swap the binary (the old one stays next to it as `cubit.old`) and restart the hub and its bots. Bots that were working resume their task when they are back (the panel warns you how many are working before you confirm).

If the new version does not start (or does not answer within 60 seconds), the next launch puts `cubit.old` back and the panel tells you the update was rolled back. If the folder of the binary is not writable, the panel just links the release page.

Manual update: replace the binary with the one from [Releases](../../../releases). Config files carry a `"schema"` number and are migrated automatically; a file written by a newer Cubit is never touched, so go back by restoring `backups/<version>/`.
