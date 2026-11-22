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

## Discord (optional)

Off by default. When you turn it on, the hub connects a Discord bot of yours so you can run your Cubit bots from Discord and watch what they say. Everything runs inside the hub; nothing is sent anywhere else.

1. Go to <https://discord.com/developers/applications> and click **New Application**.
2. Open **Bot**, click **Reset Token** and copy it. Treat it like a password.
3. You do not need any *Privileged Gateway Intents*: leave them all off. The bot only uses slash commands.
4. Open **OAuth2 → URL Generator**, tick the scopes `bot` and `applications.commands`, and under *Bot permissions* tick **View Channels** and **Send Messages**. Open the generated URL and add the bot to your server.
5. In Discord, turn on *Settings → Advanced → Developer Mode*. Then right-click the channel you want as the bridge and choose **Copy Channel ID**, and right-click each person who may give orders and choose **Copy User ID**.
6. In the panel, open **Servers**, find the **Discord** card and click **Set up**. Paste the token, the channel ID and the user IDs (comma separated), switch it on and save.

The card shows the connection state (connecting, connected, no connection); if it drops, it retries by itself with a growing wait.

- `/bot <name> <command>` runs a console command on that bot, e.g. `/bot Rocky /goto 100 64 200`.
- `/bots` lists your bots and what each is doing. `/status <name>` shows one.
- Only the user IDs you listed can use them; anyone else gets a polite refusal only they can see.
- The bridge posts what the bots say in the game chat (task done, stuck, needs a pickaxe, died...) and what players say to them, prefixed with the bot name, in batches of at most one message per second.

The token is stored only in `hub.json` on the machine running the hub (it is git-ignored). The panel shows just its last four characters and it never appears in logs.

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
