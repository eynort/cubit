# Cubit guide

[Español](GUIA.md) · [All commands](COMMANDS.md)

## 1. Run it

### Install it (no Docker, no cloning)

- **Windows**: `scoop bucket add cubit https://github.com/eynort/cubit` then `scoop install cubit/cubit`. It adds a *Cubit* Start-menu shortcut that opens the panel; your data stays in Scoop's `persist\cubit\data` folder across updates. Or download `cubit-<version>-windows-amd64.zip` (or `arm64`) from [Releases](../../../releases), unzip it and run `cubit.exe --hub :8080 --open`; a `data` folder next to `cubit.exe` keeps your data there (portable mode), otherwise it uses the current folder or `--data`. There is no installer, and `cubit.exe` is not signed, so SmartScreen may warn you: **More info → Run anyway**. winget will come later.
- **Linux**: `curl -fsSL https://github.com/eynort/cubit/releases/latest/download/install.sh | sh` installs into `~/.local/bin` (it checks the SHA256), or use the `.deb` / `.rpm`. Start the panel from your app menu, or keep it always on with `systemctl --user enable --now cubit`. Remove it with `sh install.sh --uninstall`. Data lives in `~/.local/share/cubit`.
- **macOS / Android (Termux)**: download the binary from Releases and run `./cubit --hub :8080 --open`.

The panel offers to set up a test server (and the local AI) the first time you run it. To choose where the data goes, use `--data <folder>` (or `CUBIT_DATA`); `--open` opens the panel in your browser, or just opens it if a hub is already running.

To build it yourself, see [BUILD.md](BUILD.md).

### Advanced: Docker, the panel and a test server

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

### A test server without Docker

With the panel running as a hub (`./cubit --hub :8080`) and no Docker, open **Servers → Test server**: it offers to set one up. You tick *I accept the Minecraft EULA* (<https://aka.ms/MinecraftEULA>, never accepted for you) and press **Install**. Cubit then downloads, checking each checksum:

- **Java**: Eclipse Temurin (JRE) from the official Adoptium API, for your system and the version Minecraft 26.2 asks for (about 50 MB);
- **the server**: the official `server.jar` from Mojang (about 60 MB).

Everything lives in `server/` inside the hub's data folder (about 250 MB). Progress shows in the panel, and if the connection drops, pressing the button again resumes. After that the server is started by the hub itself (offline mode, port 25565, view distance 8, RCON only for the panel with a random password), and *Start / Stop / Restart / New world* work exactly as with Docker. Closing the hub stops it cleanly. Its latest output is at `GET /server/logs` and in `server/logs/latest.log`.

| | |
|---|---|
| `MCBOT_SERVER_XMX=2G` | memory for the server (default `1G`) |
| `MCBOT_TEST_PORT` / `MCBOT_SERVER_RCON_PORT` | game port (25565) / RCON port (25575) |
| `MCBOT_NO_NATIVE=1` | never offer the native server |

It is picked automatically: if `MCBOT_SERVER_DIR` or `MCBOT_RCON` is set (what `compose.yaml` does), Docker is used and nothing changes. The RCON port listens on all interfaces, protected by that password: close it in your firewall if the machine is not yours alone.

## 2. The panel

- **Bot**: your bots, each shown with its skin head. Open one to start or stop it, change its settings, or move it to another server.
- **Each bot has its own console.** Type there to control it, with autocomplete:
  - `/follow YourName` is an order for the bot;
  - `/time set day` is a server command (if the bot is op);
  - `hello` without a slash is a message in the game chat.
  - `Tab` completes, `↑` and `↓` go through the history.
- **Servers**: start, stop and restart the test server, see who is online, and add external servers. Each server card tells you how to join it. Under *World* you can remake the test world with any seed, or a random one: the old one is kept as `world.anterior`.

## 3. Discord (optional)

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

## 4. Control the bot

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

## 5. Good to know

- **Owners**: only the owners may give orders in game. Set them with `--owner A,B`, or in the bot's settings in the panel.
- **Server login (AuthMe / nLogin)**: if the server asks for `/login` or `/register`, put the password in the bot's settings in the panel ("Server login password"); the bot answers by itself, in English or Spanish prompts. It is saved only in `bot.json` and never shown again or written to the log.
- **Premium servers**: add `--microsoft`. The first time it shows a code to sign in with your Microsoft account.
- **Memory**: about 70 MB per bot. `--view` (view distance in chunks) and `--memlimit` (in MB) lower it for phones.
- **Version**: Minecraft Java **26.2** (protocol 776), and other versions through ViaProxy (see section 6). Vanilla servers only: it uses no plugin commands.
- **Play fair**: use it on your own server, or on servers that allow bots.

## 6. Other Minecraft versions

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

## 7. Talk to it (local AI)

Owners can write plain sentences instead of commands, by whisper (`/msg MyBot mine 20 iron and bring it to me`), in the chat naming the bot, or with `/ask <text>`. A small AI model that runs **on your own machine** reads it, plans the commands (the same ones as above, only those: never server commands, never `/quit`), runs them, checks the results and answers in the chat like a mate. Plain commands (`/follow Steve`) never go through the AI and keep working even if it is down.

It is free: no account, no API key, nothing leaves your network. It uses [Ollama](https://ollama.com).

- **With Docker (`./up.sh`)**: it is already on. `up.sh` starts an `ollama` service and downloads the model the first time: about **4.6 GB, once** (`./up.sh logs ollama-pull` shows the progress). Until it finishes, the bot says it is still downloading its brain; the panel (bot settings, *AI assistant*) shows the progress too. Turn it off per bot there, or pick another model.
- **Without Docker (binary, Linux, macOS, Windows)**: install Ollama, then `ollama pull gemma4:e2b-it-q4_K_M`. The bot finds it at `http://localhost:11434`; if it is elsewhere, use `--ollama http://host:11434`, the `OLLAMA_URL` variable, or the *Ollama address* box in the panel. If the model is missing the bot starts downloading it by itself.
- **Android (Termux)**: `pkg install ollama`, `ollama serve &`, `ollama pull gemma4:e2b-it-q4_K_M`, then run the bot with the default address. Or keep Ollama on your PC and point the phone's bot to it with `--ollama http://<pc-ip>:11434`.

**Models and RAM, honestly.** The default `gemma4:e2b-it-q4_K_M` (4.6 GB download) needs roughly **3-4 GB of free RAM** while it answers. `gemma4:e4b` is smarter but needs more (about 7 GB download). `qwen3:4b` is another option. A phone with 800 MB cannot run a useful model: there, run Ollama on another machine and use `--ollama`, or switch the AI off. Answers take a few seconds on a PC and longer on a phone.

Settings (per bot, in the panel): on/off, model, Ollama address. The bot remembers the last few exchanges with each player for 10 minutes, does at most 6 steps per sentence and at most 60 sentences per hour.

## 8. Updating

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
