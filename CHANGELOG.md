# Changelog

All notable changes to Cubit. Versions are the `v*` tags of the repository.

## Unreleased

### Added
- Updates: the hub checks GitHub for a newer release at startup and every 24 hours (setting "check for updates", on by default; no identifying data is sent) and the panel shows an "Update available" notice with the release notes link.
- One-click update for the install script / zip installs: SHA-256 + ed25519 signature check of `SHA256SUMS`, config backup to `backups/<version>/`, atomic binary swap (`cubit.old` kept), automatic restart of the hub and its bots, and automatic rollback if the new version does not start within 60 seconds.
- For `.deb`, `.rpm`, Scoop and winget installs the panel shows the command to run instead; `./up.sh update` for Docker.
- Bots that were working resume their task after an update restart.
- `"schema"` version in `hub.json` and `bot.json`, with a migration runner at startup.
- Releases now embed their version (`-X main.version`), `cubit --version`, and sign `SHA256SUMS` (`SHA256SUMS.sig`, see `tools/sign`).

## v0.0.1

First tagged version: the hub and its panel, bots with PvP, excavate and pathfinding, and the test server.
