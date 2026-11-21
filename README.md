<p align="center"><img src="brand/promo/hero.png" alt="Cubit — a Minecraft bot that fights, digs and moves" width="100%"></p>

<h3 align="center">A Minecraft bot that fights, digs and moves.<br>Featherweight: <b>~70 MB of RAM per bot</b>.</h3>

<p align="center">
  <img alt="Minecraft Java 26.2" src="https://img.shields.io/badge/Minecraft%20Java-26.2-6b8f71?style=flat-square">
  <img alt="RAM per bot" src="https://img.shields.io/badge/RAM-~70%20MB%20per%20bot-4a5a6a?style=flat-square">
  <img alt="Binary" src="https://img.shields.io/badge/binary-17%20MB-5a6470?style=flat-square">
  <img alt="License MIT" src="https://img.shields.io/badge/license-MIT-8a7f6a?style=flat-square">
</p>

<p align="center"><a href="README.es.md">Español</a> · <a href="docs/GUIDE.md"><b>Guide</b></a> · <a href="docs/COMMANDS.md">Commands</a></p>

---

| | |
|---|---|
| **~70 MB** | RAM per bot (75 MB at the peak of a PvP fight) |
| **0.7 GB** | ten bots at the same time |
| **17 MB** | one binary, with no Java, Node or runtime to install |

It runs anywhere: Linux, macOS, Windows, a Raspberry Pi, a $5 VPS, or an old Android phone.

## What it does

- **PvP.** It fights like an expert: shield, crits and combos, the mace, crystals, pearls and totems. It never runs away.
- **Excavate.** It empties whole areas layer by layer, stores the loot in your chests and never breaks a pickaxe.
- **Move.** It finds its own way with A*, building towers and bridges when it needs them.

You run all your bots from a **web panel**. Each bot has its own command console there, and the panel includes a test server you can play on right away.

<p align="center"><img src="brand/promo/panel-bots.png" alt="The panel" width="100%"></p>

## Start in one minute

You need [Docker](https://docs.docker.com/get-docker/).

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
./up.sh YourMinecraftName
```

Open the panel link it prints, then create your first bot. Join the test server from **Minecraft Java 26.2** at `localhost:25565`.

You can also skip Docker and run the binary on its own: download it from [Releases](../../releases). The **[Guide](docs/GUIDE.md)** covers everything else: controlling the bot, the panel, servers and phones.

---

<sub>MIT. Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.</sub>
