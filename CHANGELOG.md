# Changelog

All notable changes to Cubit. Versions are the `v*` tags of the repository.

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
- Panel: settings in three columns, new cards aligned, more of the bot's own messages in English.
- The binary is about 20 MB (the README said 17).

## v0.0.1

First tagged version: the hub and its panel, bots with PvP, excavate and pathfinding, and the test server.
