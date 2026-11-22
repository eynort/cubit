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
- **Mine.** Strip mining like a real player: down a staircase to the best layer for each ore, no x-ray.
- **Chop.** Whole trees of the woods you name, 2×2 and Nether ones included.
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

When it finishes, it prints your panel link:

```
  Panel:      http://localhost:8080/?t=…
  Minecraft:  localhost:25565
```

1. **Open the panel**: paste the `Panel` link into your browser. The `?t=…` part is your access key, so keep the whole link.
2. **Create your bot**: the panel walks you through it (name, server, owner, language).
3. **Play with it**: in **Minecraft Java 26.2**, go to *Multiplayer → Direct Connection* and enter `localhost:25565`.

You can also skip Docker and run the binary on its own: download it from [Releases](../../releases). The **[Guide](docs/GUIDE.md)** covers everything else: controlling the bot, the panel, servers and phones.

**Updating:** the panel tells you when there is a new version and, depending on how you installed Cubit, updates it in one click or shows the command to run (`./up.sh update` with Docker). See [Updating](docs/GUIDE.md#5-updating).

## How to use the bot

Type commands in the bot's **console in the panel**. In game, whisper them as an owner: `/msg MyBot follow Steve`. `~` means "where the bot is", just like in Minecraft.

**`/follow <player>`**: follows a player non-stop, until `/stop`.
```
/follow Steve
```

**`/goto <player>` · `/goto <x> <y> <z>`**: walks to a player or to a block, building towers and bridges if it needs them. If that block is in the air or inside the ground, it stops on the nearest floor in that column.
```
/goto Steve
/goto 120 64 -340
/goto ~50 ~ ~     → 50 blocks east of where it is now
```

**`/fight <player>` · `/fight in <x> <z> [<x2> <z2>]`**: fights a player until one of them dies, or every player who enters an area (a point gets a radius of 8 blocks, or give two corners).
```
/fight Alex
/fight in 100 200             → everyone within 8 blocks of 100 200
/fight in 0 0 50 50           → everyone inside that square
```

**`/guard <player> [<minutes>]`**: bodyguard mode. It follows a player and attacks whoever hits them.
```
/guard Steve
/guard Steve 30               → for 30 minutes
```

**`/excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact]`**: digs out the whole box between two corners, 4 blocks at a time from the top down. It never breaks a pickaxe.
```
/excavate 10 70 10 30 50 30   → the box between those two corners
/excavate ~ ~ ~ ~15 ~-20 ~15  → a 16×21×16 hole starting where it stands
/excavate 0 60 0 15 40 15 exact → exactly those blocks (otherwise it fits the box to whole chunks)
/excavate chunk 0 64          → the whole chunk it stands in, from y 0 to 64
```

**`/excavate chests …`**: the chests where `/excavate` stores the valuables when it is full, and takes pickaxes from. A whole wall of chests counts if you give it any one of them.
```
/excavate chests 104 103 -224 → add that chest
/excavate chests list         → show them
/excavate chests remove 104 103 -224
/excavate chests clear        → forget them all
```

**`/mine <ores> <x> <y> <z> [every <n>] [total <n>]`**: strip mining like a real player, with no x-ray. It digs a staircase down to the best layer for that ore (diamond at y -59, iron at 16…), then a main tunnel with side branches every 3 blocks, and only mines ore it can actually see. `x y z` is the chest where it drops the ore (every 8 by default) and takes pickaxes from. The mine starts where the bot stands.
```
/mine diamond 104 64 -224              → diamonds, drop them in that chest every 8
/mine iron,copper ~ ~ ~ every 16       → a layer good for both, chest where it stands
/mine diamond 104 64 -224 total 32     → stop after 32
```

**`/chop <woods> <x> <y> <z>`**: cuts down whole trees of those woods within 24 blocks of a point, tops included, and picks up the logs. It leaves player builds alone (logs with no natural leaves, or touching planks, glass, doors…). Give the ground `y`.
```
/chop oak ~ ~ ~                  → every oak around it
/chop oak,birch 100 64 200       → oaks and birches around that point
/chop dark_oak,pale_oak ~ ~ ~    → big 2×2 trees too
/chop crimson,warped ~ ~ ~       → Nether stems
```

**`/inventory [grid]`**: shows what it carries: armor, hotbar and inventory.
```
/inventory
/inventory grid               → laid out like the game screen
```

**`/drop [<item> [<amount>]] [<player>]`**: drops everything, or one item, or walks to a player and throws it to them.
```
/drop                         → everything, on the ground
/drop diamond                 → all its diamonds
/drop cobblestone 32          → 32 cobblestone
/drop Steve                   → everything, thrown to Steve
/drop diamond 5 Steve         → 5 diamonds, thrown to Steve
```

**`/stop`**: stops whatever it is doing: fighting, walking or digging.
```
/stop
```

**`/status`**: health, food, position and what it is fighting.
```
/status
```

**`/say <text>`** (or just type with no `/`): writes in the game chat.
```
/say hello everyone
hello everyone
```

**`/cmd <command>`**: runs a server command as the bot (it needs to be op for most of them).
```
/cmd time set day
/cmd tp Steve
```

**`/sprint on|off`**: lets it run, or makes it always walk.
```
/sprint off
```

**`/lang en|es`**: the language of its messages. Commands are always in English.
```
/lang es
```

**`/help`**: lists every command. **`/quit`** disconnects the bot and closes it.

The full reference, with every option, is in [docs/COMMANDS.md](docs/COMMANDS.md).

---

<sub>MIT. Not an official Minecraft product. Not approved by or associated with Mojang or Microsoft.</sub>
