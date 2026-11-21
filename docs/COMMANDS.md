# Commands

Type them in the bot's console, in the web panel, or whisper them with `/msg <bot> <command>` (owners only).

_Generated with `go run . --gen-docs docs`: don't edit by hand._

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

`/goto <player> | <x> <z>`

Go to a player or to some coordinates.

- `<player>`: <x> <z>: coordinates (~ = where the bot is)
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

