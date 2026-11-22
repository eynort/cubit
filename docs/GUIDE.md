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

## 5. Talk to it (local AI)

Owners can write plain sentences instead of commands, by whisper (`/msg MyBot mine 20 iron and bring it to me`), in the chat naming the bot, or with `/ask <text>`. A small AI model that runs **on your own machine** reads it, plans the commands (the same ones as above, only those: never server commands, never `/quit`), runs them, checks the results and answers in the chat like a mate. Plain commands (`/follow Steve`) never go through the AI and keep working even if it is down.

It is free: no account, no API key, nothing leaves your network. It uses [Ollama](https://ollama.com).

- **With Docker (`./up.sh`)**: it is already on. `up.sh` starts an `ollama` service and downloads the model the first time: about **4.6 GB, once** (`./up.sh logs ollama-pull` shows the progress). Until it finishes, the bot says it is still downloading its brain; the panel (bot settings, *AI assistant*) shows the progress too. Turn it off per bot there, or pick another model.
- **Without Docker (binary, Linux, macOS, Windows)**: install Ollama, then `ollama pull gemma4:e2b-it-q4_K_M`. The bot finds it at `http://localhost:11434`; if it is elsewhere, use `--ollama http://host:11434`, the `OLLAMA_URL` variable, or the *Ollama address* box in the panel. If the model is missing the bot starts downloading it by itself.
- **Android (Termux)**: `pkg install ollama`, `ollama serve &`, `ollama pull gemma4:e2b-it-q4_K_M`, then run the bot with the default address. Or keep Ollama on your PC and point the phone's bot to it with `--ollama http://<pc-ip>:11434`.

**Models and RAM, honestly.** The default `gemma4:e2b-it-q4_K_M` (4.6 GB download) needs roughly **3-4 GB of free RAM** while it answers. `gemma4:e4b` is smarter but needs more (about 7 GB download). `qwen3:4b` is another option. A phone with 800 MB cannot run a useful model: there, run Ollama on another machine and use `--ollama`, or switch the AI off. Answers take a few seconds on a PC and longer on a phone.

Settings (per bot, in the panel): on/off, model, Ollama address. The bot remembers the last few exchanges with each player for 10 minutes, does at most 6 steps per sentence and at most 60 sentences per hour.
