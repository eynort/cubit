# Commands

Type them in the bot's console, in the web panel, or whisper them with `/msg <bot> <command>` (owners only).

_Generated with `go run . --gen-docs docs`: don't edit by hand._

## /afk

`/afk [<x>] [<y>] [<z>]`

Stay at a spot 24/7 until /stop (here, or at the coordinates: it walks there first): small random moves every 1-4 minutes so the server does not kick it for idling, back to the spot if pushed, eats when hungry, fights back mobs that come close, sleeps in a bed within 16 blocks at night or in thunderstorms, and returns to the spot after dying or reconnecting.

- `<x>` (optional): the spot (~ = where the bot is), or here
- `<y>` (optional): the spot (~ = where the bot is), or here
- `<z>` (optional): the spot (~ = where the bot is), or here

**Examples**

```
/afk               # stay where it stands, until /stop
/afk here          # the same
/afk 120 64 -340   # walk to that block and stay there
/afk ~ ~ ~         # the block it stands on now
```

## /ask

`/ask <request>`

Ask the bot in plain words and it does it, using its commands as tools (needs the AI assistant on in the panel settings, and Ollama running): /ask mine 20 iron and bring it to me.

- `<request>`: what you want, in your own words

**Examples**

```
/ask mine 20 iron and bring it to me   # plain words: the local AI plans the commands
/ask ve a talar unos robles
/ask how are you doing?                # it just answers in the chat
```

## /chop

`/chop <woods> [<x>] [<y>] [<z>]`

Chop whole trees of the given woods (oak, birch, spruce, jungle, acacia, dark_oak, mangrove, cherry, pale_oak, and crimson/warped in the Nether), separated by commas (/chop oak,birch 100 64 200; ~ = where the bot is): it goes to the point and fells every tree of those woods within 24 blocks of it, all the logs (also the tall ones: the pathfinding places blocks to climb and they are removed after) and picks up what falls; no log is left floating. It only chops trees (natural leaves around, nothing built next to the logs): player-made log structures and stripped logs are left alone. It uses the best axe it carries, never breaking it (without one, by hand). At the end it logs how many trees and logs of each wood.

- `<woods>`: one or more woods, separated by commas (oak,birch)
- `<x>` (optional): the point to chop around (~ = where the bot is; without it, where it stands)
- `<y>` (optional): the point to chop around (~ = where the bot is; without it, where it stands)
- `<z>` (optional): the point to chop around (~ = where the bot is; without it, where it stands)

**Examples**

```
/chop oak ~ ~ ~                 # every oak within 24 blocks of where it stands
/chop oak,birch 100 64 200      # oaks and birches around that point (y = the ground)
/chop dark_oak,pale_oak ~ ~ ~   # big 2×2 trees too
/chop crimson,warped ~ ~ ~      # Nether stems
```

## /cmd

`/cmd <command>`

Run a server command as a player (even one named like a bot command: /cmd help, /cmd time set day)

- `<command>`: the server command, without /

**Examples**

```
/cmd time set day   # needs the bot to be op
/cmd tp Steve
```

## /drop

`/drop [<item> [<amount>] [<player>] | <player>]`

Throw on the ground everything it carries, or one item (all of it, or the amount given: /drop dirt 10); with a player, it goes to them and throws it to them (/drop Alex, /drop dirt 10 Alex)


**Examples**

```
/drop                   # everything, on the ground
/drop diamond           # all its diamonds
/drop cobblestone 32    # 32 cobblestone
/drop Steve             # everything, thrown to Steve
/drop diamond 5 Steve   # 5 diamonds, thrown to Steve
```

## /excavate

`/excavate <x> <y> <z> <x2> <y2> <z2> [<exact>] | chunk <y1> <y2> | chests [<x>] [<y>] [<z>]`

Dig out a whole box of blocks between two corners (/excavate 10 70 10 30 50 30), 4 blocks deep at a time: each 4-block pass through the whole box before going down to the next (the ground goes down evenly); in each pass it stands on the bottom and digs the 4 blocks high it reaches around it, then moves on, and picks up what fell when the pass is done. It uses the pickaxes it carries, never breaking one (with none left, it asks for one in chat). When full it throws away the junk (dirt, stone, gravel…) and keeps what is valuable; with chests given (/excavate chests <x> <y> <z>), it takes the valuables there.

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
- `chests [<x>] [<y>] [<z>]`: where /excavate (only /excavate) keeps what is valuable: <x> <y> <z> adds a chest (or barrel, or shulker box; either half of a double chest), list shows them, remove <x> <y> <z> forgets one, clear forgets them all. Remembered across restarts. When full it first throws the junk; if still full, it walks to the nearest chest and stores the valuables (never its tools, armor, food or fighting gear), the next chest when one is full, and again when the excavation ends
  - `<x>` (optional): the chest (~ = where the bot is), or list / clear / remove
  - `<y>` (optional): the chest (~ = where the bot is), or list / clear / remove
  - `<z>` (optional): the chest (~ = where the bot is), or list / clear / remove

**Examples**

```
/excavate 10 70 10 30 50 30            # the box between those two corners
/excavate ~ ~ ~ ~15 ~-20 ~15           # a 16×21×16 hole from where it stands
/excavate 0 60 0 15 40 15 exact        # exactly those blocks (no chunk fitting)
/excavate chunk 0 64                   # the whole chunk it stands in, y 0 to 64
/excavate chests 104 103 -224          # store the loot there (a whole wall of chests counts)
/excavate chests list                  # show the chests
/excavate chests remove 104 103 -224
/excavate chests clear                 # forget them all
```

## /fight

`/fight <player> | in <x> <z> [<x2> <z2>]`

Fight a player, or every player in an area.

- `<player>`: the player to fight
- `in <x> <z> [<x2>] [<z2>]`: every player inside an area: a point (radius 8) or two corners

**Examples**

```
/fight Alex           # fight Alex until one of them dies
/fight in 100 200     # everyone within 8 blocks of 100 200
/fight in 0 0 50 50   # everyone inside that square
```

## /follow

`/follow <player>`

Follow a player non-stop (until /stop)


**Examples**

```
/follow Steve
```

## /goto

`/goto <player> | <x> <y> <z>`

Go to a player or to the block at some coordinates (if that height is air or solid, the nearest standable spot in the column)

- `<player>`: <x> <y> <z>: coordinates (~ = where the bot is)
- `<y>`: the y coordinate (the height of the cell to stand in)
- `<z>`: the z coordinate

**Examples**

```
/goto Steve         # walk to Steve
/goto 120 64 -340   # walk to that block
/goto ~50 ~ ~       # 50 blocks east of where it is
```

## /guard

`/guard <player> [<minutes>]`

Bodyguard: follows a player and attacks whoever hits them (until /stop, or for the given minutes)

- `<minutes>` (optional): for how many minutes (without it, until /stop)

**Examples**

```
/guard Steve      # until /stop
/guard Steve 30   # for 30 minutes
```

## /help

`/help`

This help.


**Examples**

```
/help
```

## /inventory

`/inventory [<grid>]`

What the bot carries: equipment, hotbar slot by slot and the inventory; grid: like the game screen.

- `<view>` (optional)
  - `grid`: like the game screen (wide)

**Examples**

```
/inventory
/inventory grid   # laid out like the game screen
```

## /lang

`/lang <en|es>`

Language of the explanations (commands are always in English)

- `<language>`
  - `en`: English
  - `es`: Spanish

**Examples**

```
/lang es
/lang en
```

## /mine

`/mine <ores> [<x> <y> <z>] [every <n>] [total <n>]`

Mine ores like a real player (strip mining, no xray): from where it stands, it goes down a staircase to the ore's best layer, digs a 1×2 main tunnel with a branch to each side every 3 blocks, and only mines what a player would see (ore touching the air of the tunnel, and the rest of a vein once it shows). It never breaks a pickaxe: it takes them from the chest and, with none left, goes back for more (or asks in chat). Every 8 ores (every <n>) it walks back to the chest, leaves the ores and valuables (never tools or food), and returns to the mine. /stop stops it; /status says what it is doing. When it ends it prints a summary.

- `<ores>`: what to mine: one, or several separated by commas (diamond,iron); in the Nether, ancient_debris and quartz
- `<x>` (optional): the chest (or a wall of chests: any one of them) where it takes pickaxes from and leaves the ores (~ = where the bot is; without it, where it stands)
- `<y>` (optional): the chest (or a wall of chests: any one of them) where it takes pickaxes from and leaves the ores (~ = where the bot is; without it, where it stands)
- `<z>` (optional): the chest (or a wall of chests: any one of them) where it takes pickaxes from and leaves the ores (~ = where the bot is; without it, where it stands)
- `<option>` (optional)
  - `every`: leave the ores in the chest every <n> (default 8)
  - `total`: stop after <n> ores (default: until /stop)
- `<n>` (optional): how many

**Examples**

```
/mine diamond 104 103 -224              # mine diamonds, using the chest there (leaves them every 8)
/mine iron,coal 104 103 -224 every 16   # iron and coal at once, leaving them every 16
/mine diamond ~ ~ ~ total 12            # the chest is where it stands; stops at 12 diamonds
/mine ancient_debris 104 103 -224       # in the Nether: ancient debris at y=15
```

## /queue

`/queue <command> | list | clear | remove <n> | resume | pause`

Task queue: queue a command to run when the current task ends (/queue /chop oak ~ ~ ~), or chain commands with ' ; ' in any line (the first runs now, the rest wait). A failed task pauses the queue; /stop pauses it too.

- `<command>`: the command to queue (chop, mine, excavate or goto x y z)
- `list`: show what is waiting
- `clear`: forget everything waiting
- `remove <n>`: forget one by its number (see list)
- `resume`: carry on after a pause
- `pause`: hold the queue (the current task goes on)

**Examples**

```
/queue /chop oak ~ ~ ~                  # chop after the current task ends
/queue list                             # what is waiting, numbered
/queue remove 2                         # forget the second one
/queue clear                            # forget them all
/queue resume                           # carry on after a failure or /stop
/mine iron ~ ~ ~ ; /chop oak 10 64 10   # the same with a chain: the first runs now, the rest wait
```

## /quit

`/quit`

Disconnect and exit.


**Examples**

```
/quit   # disconnect and close the bot
```

## /say

`/say <text>`

Write in the chat (or type without /)

- `<text>`: what to write

**Examples**

```
/say hello everyone
hello everyone        # the same: text without / goes to the chat
```

## /sprint

`/sprint <on|off>`

Allow or forbid sprinting.

- `<mode>`
  - `off`: it always walks
  - `on`: it may sprint

**Examples**

```
/sprint off   # always walk
/sprint on
```

## /status

`/status`

Health, food, position and combat.


**Examples**

```
/status
```

## /stop

`/stop`

Stop fighting and walking (and pause the queue)


**Examples**

```
/stop
```

