# Changelog

All notable changes to Cubit. Versions are the `v*` tags of the repository.

## Unreleased

### Security
- **If you have v0.0.2: update by hand once** (re-run `install.sh` or download the new release). The update signing key changed, so the one-click update in v0.0.2 refuses newer versions. From then on it works again.
- A player could post a fake "link your Microsoft account" message in chat and the panel showed it as the bot's own request, with the attacker's code. The panel now only shows the code the bot itself asked for, and only `https` Microsoft links.
- `--hub :8080` now listens only on this device. Use `--hub 0.0.0.0:8080` to open the panel to your network (it is plain HTTP: trusted networks only). Docker publishes the panel and the test server on `127.0.0.1` by default (`PANEL_BIND`, `MC_BIND` in `.env`).
- Owner names are matched with exact capitals: on servers without premium accounts `oWNER` is a different account from `Owner`.
- The AI can no longer chain extra orders with ` ; `.
- The auto-login password is only sent to the server it was set for, and only right after joining.
- Whispers with no real player behind them (command blocks, NPCs) are never orders; whispers are no longer copied to Discord.
- Wrong keys no longer lock the right key out of the panel; changes requested from other websites are refused.
- The built-in test server (no Docker) listens only on this device; Docker no longer defaults the RCON password to `cubit`.
- Releases are signed in a protected environment that needs the maintainer's approval; GitHub Actions are pinned by commit.
- Dependencies: `gorilla/websocket` 1.5.3 and `golang.org/x/crypto` (known vulnerabilities).

## v0.0.2

### Added
- New logo (the cube-robot badge) across the panel, icons and promo images.
- Install without cloning: `install.sh` one-liner and `.deb`/`.rpm` on Linux, Scoop or a plain zip on Windows, raw binaries for macOS and Android (Termux). See [Compile it yourself](docs/BUILD.md) to build from source.
- Built-in test server without Docker: the panel downloads the official Minecraft server and a Java runtime after you accept the EULA.
- Local AI with Ollama (default model `gemma4:e2b-it-q4_K_M`): owners talk to their bots in plain English or Spanish, and `/ask`. Free, no accounts, no API keys.
- `/afk` 24/7 mode (anti-idle, eats, sleeps, comes back after dying) and automatic `/login` for AuthMe/nLogin servers.
- Task queue: `/queue` and chaining with ` ; `.
- Public HTTP API with live events (SSE) and Bearer keys: see [docs/API.md](docs/API.md).
- Discord bridge: `/bot`, `/bots`, `/status` and a channel with what your bots say.
- Other Minecraft versions through ViaProxy, detected automatically.
- `/mine` (strip mining without x-ray) and `/chop` (by wood type); both work without coordinates.
- Updates: the hub checks GitHub for a newer release at startup and every 24 hours (setting "check for updates", on by default; no identifying data is sent) and the panel shows an "Update available" notice with the release notes link.
- One-click update for the install script / zip installs: SHA-256 + ed25519 signature check of `SHA256SUMS`, config backup to `backups/<version>/`, atomic binary swap (`cubit.old` kept), automatic restart of the hub and its bots, and automatic rollback if the new version does not start within 60 seconds.
- For `.deb`, `.rpm`, Scoop and winget installs the panel shows the command to run instead; `./up.sh update` for Docker.
- Bots that were working resume their task after an update restart.
- `"schema"` version in `hub.json` and `bot.json`, with a migration runner at startup.
- Releases now embed their version (`-X main.version`), `cubit --version`, and sign `SHA256SUMS` (`SHA256SUMS.sig`, see `tools/sign`).

### Changed
- About 25 % less memory per bot and a 5× faster start (measured on the fake server: see [docs/PERFORMANCE.md](docs/PERFORMANCE.md)).
- Panel: settings in three columns, new cards aligned.
- The binary is about 20 MB (the README said 17).

### Fixed
- Bots set to English (`/lang en`) no longer show Spanish in `/status`, in the panel's combat and task fields, in the common console lines (connecting, joined, died, task started/finished/stuck, goto, queue, AI), or in ore-name and path errors. The AI gets its state in English, and the "Ollama is down" reply follows the speaker's language.

## v0.0.1

First tagged version: the hub and its panel, bots with PvP, excavate and pathfinding, and the test server.
