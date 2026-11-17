# Commands

Type them in the bot's console, in the web panel, or whisper them with `/msg <bot> <command>` (owners only).

_Generated with `go run . --gen-docs docs`: don't edit by hand._

## /ai

`/ai <message> | info | reset [<player>] | compact [<player>] | on | off`

Talk to the AI (gemma4 in Ollama), or: info, reset [player], compact [player], on, off.

- `<message>`: what to tell it
- `info`: model, context and conversations
- `reset [<player>]`: forget a conversation (yours without a player)
- `compact [<player>]`: summarise a conversation now
- `on`: answer /msg with the AI
- `off`: /msg are only commands

## /around

`/around`

Write the blocks around the bot (layer by layer) to the log.


## /base

`/base <here|info>`

The storage: "here" sets the chests' base where the bot stands; "info" shows it.

- `<action>`
  - `here`: the base where the bot stands
  - `info`: where it is and its chests

## /beds

`/beds [<amount>]`

Make beds (wool from sheep: shears the ones around and in the pen; tends the farm if short)

- `<amount>` (optional): how many beds (8 if not given)

## /build

`/build load <file> [<x> <y> <z> | me | <player>] [none|cw_90|180|ccw_90] [none|left_right|front_back] | origin <x> <y> <z> | rotate <r> | mirror <m> | import <placements.json> | info | check | materials | chest [add|remove [<x> <y> <z>] | list | clear] | start [noclear] | list`

Build a schematic (Litematica .litematic, WorldEdit .schem, structure .nbt) with the materials of some chests: load <file> [<x> <y> <z> | here | <player>] [rotation] [mirror], origin, rotate, mirror, import <litematica placements .json>, info, check, materials, chest add|remove|list|clear, start [noclear], list.

- `load <file> [<x>] [<y>] [<z>] [<none|90|180|270>] [<none|left_right|front_back>]`: load a schematic (and where it goes)
  - `<file>`: a schematic of schematics/
  - `<rotation>` (optional)
    - `180`: half a turn
    - `270`: a quarter turn counterclockwise
    - `90`: a quarter turn clockwise
    - `none`: as it is
  - `<mirror>` (optional)
    - `front_back`: mirrored front to back
    - `left_right`: mirrored left to right
    - `none`: no mirror
- `origin <x> <y> <z>`: where it goes: <x> <y> <z>, here or a player
- `rotate <none|90|180|270>`: turn it
  - `<rotation>`
    - `180`: half a turn
    - `270`: a quarter turn counterclockwise
    - `90`: a quarter turn clockwise
    - `none`: as it is
- `mirror <none|left_right|front_back>`: mirror it
  - `<mirror>`
    - `front_back`: mirrored front to back
    - `left_right`: mirrored left to right
    - `none`: no mirror
- `import <file.json> [<name>]`: the placements of a Litematica .json
  - `<file.json>`: the .json file
  - `<name>` (optional): which placement
- `info`: what is loaded and where
- `check`: what is already built and what is missing
- `materials`: the materials it needs and those in the chests
- `chest <add|remove|list|clear> [<x>] [<y>] [<z>]`: the chests with the materials
  - `<action>`
    - `add`: mark the nearest one (or <x> <y> <z>)
    - `clear`: forget them all
    - `list`: show them
    - `remove`: forget one
- `start [<noclear>]`: start building
  - `<noclear>` (optional)
    - `noclear`: without clearing the area first
- `list`: the schematics in schematics/

## /chests

`/chests [<on|off>]`

Use chests to store what it gathers (on by default); off: it works with what fits in its inventory (saved in bot.json)

- `<mode>` (optional)
  - `off`: never use chests
  - `on`: store in chests; with none nearby it says so and keeps working

## /chop

`/chop <wood> [<amount>]`

Chop trees: with the best axe it carries; without one, it makes a stone axe first; then the base (table and double chest)

- `<wood>`: which wood
- `<amount>` (optional): how many logs (without it, until /stop)

## /cmd

`/cmd <command>`

Run a server command as a player (even one named like a bot command: /cmd help, /cmd time set day)

- `<command>`: the server command, without /

## /debug

`/debug`

Write where every part of the bot is (goroutine stacks) to the session log (--logs)


## /diag

`/diag`

Tests jumps and moves and counts the server's corrections.


## /drop

`/drop [<item> [<amount>] [<player>] | <player>]`

Throw on the ground everything it carries, or one item (all of it, or the amount given: /drop dirt 10); with a player, it goes to them and throws it to them (/drop Alex, /drop dirt 10 Alex)


## /elytra

`/elytra <on|off|wear>`

On/off: let /goto fly with elytra and rockets when far (on by default); wear: put the elytra on now.

- `<mode>`
  - `off`: /goto always walks
  - `on`: /goto may fly when far
  - `wear`: put the elytra on now

## /excavate

`/excavate <x> <y> <z> <x2> <y2> <z2> [<exact>] | chunk <y1> <y2>`

Dig out a whole box of blocks between two corners (/excavate 10 70 10 30 50 30), 4 blocks deep at a time: each 4-block pass through the whole box before going down to the next (the ground goes down evenly); in each pass it stands on the bottom and digs the 4 blocks high it reaches around it, then moves on, and picks up what fell when the pass is done. It uses the pickaxes it carries and the ones in the mining chest (/mine chest) or the storage, never breaking one, and when full it takes what it dug to the chests.

- `<x>`: one corner (~ = where the bot is)
- `<y>`: one corner (~ = where the bot is)
- `<z>`: one corner (~ = where the bot is)
- `<x2>`: the opposite corner
- `<y2>`: the opposite corner
- `<z2>`: the opposite corner
- `<exact>` (optional)
  - `exact`: exactly those blocks (without it, a box of whole chunks written with the chunk edges is fitted to the chunks)
- `chunk <y1> <y2>`: the whole 16×16 chunk the bot is in (or the one with block x z), from y1 to y2: the chunk edges are worked out for you
  - `<y1>`: from this height
  - `<y2>`: to this height

## /farm

`/farm`

Farm by the base: wheat field and sheep pen (brings sheep in, breeds and shears them) — until /stop.


## /fight

`/fight <player> | in <x> <z> [<x2> <z2>]`

Fight a player, or every player in an area.

- `<player>`: the player to fight
- `in <x> <z> [<x2>] [<z2>]`: every player inside an area: a point (radius 8) or two corners

## /follow

`/follow <player>`

Follow a player non-stop (until /stop)


## /foodchest

`/foodchest [add [<x> <y> <z>] | remove [<x> <y> <z>] | list | clear | now]`

Chests the bot may take food from (besides its storage and the chests by the base): add marks the nearest one (or <x> <y> <z>), remove/clear forget, list shows them, now goes for food now. With no food it goes there before hunting, and takes only what it needs.

- `add [<x>] [<y>] [<z>]`: mark a food chest
  - `<x>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
  - `<y>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
  - `<z>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
- `remove [<x>] [<y>] [<z>]`: forget a food chest
  - `<x>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
  - `<y>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
  - `<z>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
- `list`: show them
- `clear`: forget them all
- `now`: go for food now

## /goto

`/goto <player> | <x> <z>`

Go to a player or to some coordinates.

- `<player>`: <x> <z>: coordinates (~ = where the bot is)
- `<z>`: the z coordinate

## /guard

`/guard <player> [<minutes>]`

Bodyguard: follows a player and attacks whoever hits them (until /stop, or for the given minutes)

- `<minutes>` (optional): for how many minutes (without it, until /stop)

## /help

`/help`

This help.


## /inventory

`/inventory [<grid>]`

What the bot carries: equipment, hotbar slot by slot and the inventory; grid: like the game screen.

- `<view>` (optional)
  - `grid`: like the game screen (wide)

## /lang

`/lang <en|es>`

Language of the explanations (commands are always in English)

- `<language>`
  - `en`: English
  - `es`: Spanish

## /limits

`/limits [<what>] [<value>]`

What the bot never does: blocks it never breaks, the area it may dig in, PvP (saved in bot.json)

- `<what>` (optional)
  - `allow`: allow <block>: it may break it
  - `area`: area <x1> <z1> <x2> <z2> | area off
  - `deny`: deny <block>: never break it
  - `pvp`: pvp on|off
  - `reset`: the default limits

## /mem

`/mem`

Memory, GC, loaded chunks and entities (the same as --monitor)


## /mine

`/mine <ore[,ore…]> [<amount> | all] | chest [<x> <y> <z> | off | list] | list`

Mine ores by strip mining, each with its own strategy (Y level, tunnel spacing and length): /mine diamond, /mine iron,copper 200, /mine debris. /mine list shows the strategies. /mine chest marks the chest of its dimension (one per dimension): it takes pickaxes (without breaking any), food and armor from it and leaves what it mines there; if the ore's dimension has none it goes to the other one. With no usable pickaxe it makes its own.

- `<ores>`
  - `coal`: carbón: mundo normal, y=44, galerías cada 6
  - `copper`: cobre: mundo normal, y=48, galerías cada 6
  - `debris`: escombros ancestrales: Nether, y=15, galerías cada 3
  - `diamond`: diamantes: mundo normal, y=-53, galerías cada 3
  - `gold`: oro: mundo normal, y=-16, galerías cada 4
  - `iron`: hierro: mundo normal, y=16, galerías cada 5
  - `lapis`: lapislázuli: mundo normal, y=0, galerías cada 4
  - `quartz`: cuarzo: Nether, y=15, galerías cada 5
  - `redstone`: redstone: mundo normal, y=-53, galerías cada 3
- `<amount>` (optional): how many (without it, or all: until /stop)
  - `all`: until /stop
- `chest [<x>] [<y>] [<z>]`: mark the mining chest of this dimension (the nearest one, or <x> <y> <z>); off forgets it; list shows them
  - `<x>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
    - `list`: show the mining chests
    - `off`: forget the chest of this dimension
  - `<y>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
  - `<z>` (optional): <x> <y> <z> of the chest; without them, the nearest one (5 blocks)
- `list`: the strategy of every ore (Y level, spacing, why)

## /protected

`/protected list | here | clear`

Places where the server put back what the bot broke (spawn protection): the bot does not dig, build or look for trees within 16 blocks of them. list shows them, here forgets the ones around the bot, clear forgets them all.

- `list`: show them
- `here`: forget the ones around the bot
- `clear`: forget them all

## /queue

`/queue list | add <command> | insert <position> <command> | remove <n> | move <from> <to> | up <n> | down <n> | skip | clear | pause | resume`

The command queue (goto, guard, fight, chop, mine, beds, farm, build): list, add, remove, reorder, skip, pause.

- `list`: show the queue
- `add <command>`: add a command at the end
- `insert <position> <command>`: put a command at a position
  - `<position>`: where (1 = next)
- `remove <n>`: remove one
  - `<n>`: its position (see /queue)
- `move <from> <to>`: move one
  - `<from>`: its position
  - `<to>`: the new position
- `up <n>`: one place earlier
  - `<n>`: its position
- `down <n>`: one place later
  - `<n>`: its position
- `skip`: stop the running one and go to the next
- `clear`: remove all the pending ones
- `pause`: do not start the next ones
- `resume`: go on

## /quit

`/quit`

Disconnect and exit.


## /say

`/say <text>`

Write in the chat (or type without /)

- `<text>`: what to write

## /sprint

`/sprint <on|off>`

Allow or forbid sprinting.

- `<mode>`
  - `off`: it always walks
  - `on`: it may sprint

## /status

`/status`

Health, food, position and combat.


## /stop

`/stop`

Stop fighting and walking (and pause the queue)


