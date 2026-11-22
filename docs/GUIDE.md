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
- **Version**: Minecraft Java **26.2** (protocol 776), and other versions through ViaProxy (see section 5). Vanilla servers only: it uses no plugin commands.
- **Play fair**: use it on your own server, or on servers that allow bots.

## 5. Other Minecraft versions

The bot speaks Minecraft Java **26.2** only. To join a server running another version (older or newer), it goes through [ViaProxy](https://github.com/ViaVersion/ViaProxy), which translates between versions. It is transparent: nothing to set up per server.

How it works: before joining, the bot asks the server its version (the same ping the multiplayer list does). Same version: it joins directly. Different version: it joins through ViaProxy, telling it the server and version to use. One ViaProxy serves all your servers. The panel shows each server's detected version in the Servers list ("via ViaProxy (version X)", or "needs ViaProxy" if there is none).

**Docker (`./up.sh`)**: add the profile to `.env` and start again.

```
COMPOSE_PROFILES=local,viaproxy      # or just viaproxy if you have no test server
```

This builds `Dockerfile.viaproxy` (ViaProxy 3.4.14 on Java 21, about 200 MB of RAM) and the hub already points at it (`MCBOT_VIAPROXY=viaproxy:25568`). It is not published outside the Docker network.

**Without Docker**: download `ViaProxy-3.4.14.jar` from the [releases](https://github.com/ViaVersion/ViaProxy/releases) (Java 17 or newer; the `+java8` jar for older Java) and give the hub either its path (the hub starts it by itself, listening on `127.0.0.1:25568`, only when a server needs it) or the address of one you run yourself:

```
./cubit --hub=:8080 --viaproxy /path/ViaProxy-3.4.14.jar
./cubit --hub=:8080 --viaproxy 127.0.0.1:25568     # your own, started with:
java -jar ViaProxy.jar cli --bind-address 127.0.0.1:25568 --wildcard-domain-handling public --auth-method none
```

A single bot (no hub) takes the same `--viaproxy host:port`, or the `MCBOT_VIAPROXY` variable.

**Premium (online-mode) servers**: the bot does not log in with its own Microsoft account through ViaProxy. ViaProxy logs in with **its own** account, the same one for every bot. Set `VIAPROXY_AUTH=account` (Docker) or start it with `--auth-method account`, and add the account once from ViaProxy's console: `account add microsoft` (it prints a code to open in the browser), then `account select 0`. In Docker: `docker compose attach viaproxy`. Offline servers need nothing.

Limitations:
- All bots on premium servers through ViaProxy play as that one account (one name, one connection at a time per server).
- A server with an SRV record and no port in its address: write the real `host:port`.
- Game features that do not exist in the server's version may not work (the bot assumes 26.2 behavior). Very old servers (before 1.13) are the most fragile.
- Use ViaProxy only on trusted networks: it accepts any destination from whoever can reach it.
