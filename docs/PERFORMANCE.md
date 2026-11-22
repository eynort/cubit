# Performance

How much memory and CPU Cubit uses, where it goes, what was changed to lower it, and the plan for
running many bots in one process. Every number here was measured on the machine described below
with the tools in this repository; nothing is estimated unless it says so.

## Methodology

- **Machine**: AMD Ryzen 7 5700G (16 threads), Linux 7.2, Go 1.27.4. Release build:
  `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"` (as `.github/workflows/release.yml`).
- **Server**: the fake server of the kit (`go run ./tools/fakesrv -radius 6 -max 5 [-compress 256]`):
  a flat world, `-radius 6` sends 13×13 = 169 chunks around spawn — what a vanilla server sends a
  bot that asks for `--view 6` (the default). `-compress 256` compresses packets like a vanilla
  server (`network-compression-threshold=256`). No real server was used.
- **Bots**: N separate `cubit` processes, as the hub runs them
  (`--server <fakesrv> --name PerfN --reconnect=false --monitor 127.0.0.1:81xx`,
  `MCBOT_PANEL_TOKEN` set), sampled 45 s after start.
- **What is reported**:
  - **RSS** (`VmRSS`) counts the binary's pages in every process that maps them;
  - **PSS** (`/proc/PID/smaps_rollup`) splits those shared pages between the processes: with N bots
    on one machine, **N × PSS is what they really cost together**;
  - **anon** is the private memory (Go heap, GC metadata, stacks);
  - Go heap numbers come from `/debug/pprof/heap` on each bot's panel (`?gc=1` for live memory,
    `GODEBUG=memprofilerate=4096` for an exact breakdown);
  - CPU is `utime+stime` from `/proc/PID/stat` (ticks of 10 ms).
- **Real chunks**: `go run ./tools/chunkmem -region <r.x.z.mca>` loads the finished chunks of a
  region written by a vanilla 26.2 server, passes them through the packet form and stores them as
  `bot/world` does.
- **Many bots in one process**: `go run ./tools/swarmbench -n 10 -radius 6` (see below).

Runs vary by ±1-3 MB; the 5-bot rows average 3 runs (plain) and 2 runs (compressed) of each build, run one after the other on the same machine.

## Before and after

Before: `7ab36a9` (main when this work started). After: this branch.

| | before | after | change |
|---|---:|---:|---:|
| 5 bots, plain packets: average **PSS** per bot | 35.1 MB | 26.3 MB | −25 % |
| 5 bots, plain packets: average RSS per bot | 47.6 MB | 38.6 MB | −19 % |
| 5 bots, compressed packets: average **PSS** per bot | 35.9 MB | 25.6 MB | −29 % |
| hub alone, idle (RSS / anon) | 42.6 / 27.4 MB | 31.0 / 15.4 MB | −27 % / −44 % |
| start-up: package `init` (time / allocated) | 245 ms / 33.5 MB | 45 ms / 5.5 MB | −82 % / −84 % |
| joining (169 compressed chunks): allocated (before = before change 2 only) | 32.1 MB | 25.5 MB | −21 % |
| walking (5 `/goto` in 40 s): allocated (before = before change 5 only) | 11.3 MB | 3.0 MB | −74 % |
| real overworld chunk in memory | 29.8 KB | 24.5 KB | −18 % |
| one more bot in a shared process, live heap (`swarmbench`; before = before change 4 only) | 5.7 MB | 4.9 MB | −14 % |
| idle CPU per bot | 0.3 % of a core | 0.3 % of a core | — |
| binary (release flags, linux/amd64, uncompressed; the README says 17 MB) | 19.4 MB | 19.5 MB | — |

The README's "~70 MB per bot" was measured on a real server (more entities, players, compressed
traffic). It was **not re-measured** here, because the bots do not connect to real servers for
tests; on the fake server a bot now costs a quarter less (PSS), and the gain should be larger on a
real server, where most packets are compressed (change 2). The README keeps its number until a
real-server measurement replaces it.

## Where the memory goes

One bot, after joining (169 flat chunks), live Go heap ≈ 10 MB after a GC:

| what | live heap |
|---|---:|
| chunks (palette-packed sections, no light; 169 flat chunks) | 4.4 MB (≈3.6 MB after change 4) |
| block-state tables (`level/block`: `StateList`, `ToStateID`, ~32 000 states) | 2.7 MB |
| the server's registries, decoded per client (`registry.NewNetworkCodec`) | 1.1 MB |
| other tables built at start (tags, sounds, items, `bot/world`, `pathing`) | ~0.8 MB |
| everything else (entities, inventory, goroutine stacks, buffers) | ~1 MB |

The process holds more than that:

- **the binary** (~13-15 MB of text and read-only data, embedded panel included) is file-backed
  and shared by every process of the same binary: it shows in each RSS, but only once in the
  sum of PSS;
- **heap headroom**: Go lets the heap grow to (1 + GOGC/100) × live before collecting, and hands
  freed pages back to the OS slowly;
- **runtime**: GC metadata (~3.8 MB), stacks, ~20 OS threads.

Chunk storage was already compact (palette + bit-packed sections, light skipped): a real overworld
chunk is ~30 KB, ~25 KB now; at the default `--view 6` that is ~5 MB, ~13 MB at view 10.

## What was changed

In order of impact; each is its own commit.

1. **Block-state table decoded without reflection** (`third_party/go-mc26/level/block`). The
   `init` of `level/block` decoded the ~32 000 block states with the reflective NBT decoder:
   33 MB of garbage and ~160 ms in **every** process at start, the hub's and `--version`
   included, and the heap grown by that peak stayed fragmented. `decodeStates` reads the same
   NBT by hand: 39 ms, 4 MB. A test checks the result is identical to the NBT decoder's.
2. **zlib readers reused** (`third_party/go-mc26/net/packet`). Each compressed packet created a
   new `zlib.Reader` (~40 KB). Now they come from a `sync.Pool`. On a real server every chunk and
   most entity packets are compressed: this was most of what a client allocated there.
   Benchmark: 3.8 MB → 10 KB per 100 packets, 22 % faster.
3. **GC tuned** (`internal/memtune`). `GOGC=50` by default (Go's is 100), and
   `debug.FreeOSMemory()` once after the peak (30 s after joining, 15 s after the hub starts),
   only when more than 4 MB are free and not yet returned. Allocation is low (~30 KB/s idle,
   ~75 KB/s walking), so the extra collections cost nothing measurable. `--memlimit` (150 MB by
   default), `GOGC` and `GOMEMLIMIT` still apply.
4. **No heightmaps or biomes kept** (`World.SkipHeightmaps`, `World.SkipBiomes`, like
   `SkipLight`): Cubit reads neither. −18 % per chunk.
5. **Path search without per-cell allocation** (`bot/path.Graph.boxes` appends to a buffer on
   the caller's stack): −74 % allocation while walking.

Looked at and left alone, with the reason:

- **Idle CPU** is 0.27 % of a core per bot (30 s CPU profile): the 20 Hz movement tick that a
  vanilla client also runs and servers expect. Nothing spins.
- **Hub polling**: `pollEvents` only polls while a panel is listening; the Discord bridge polls
  `/log` every 2 s only while Discord is connected. The idle hub used 0 CPU ticks in 20 s.
  Making these event-driven would need a push channel from every bot process; not worth it now.
- **Ring buffers** are already capped (the panel log and the hub's 30-line tail per bot).
- **Binary size**: Go code itself; the biggest Cubit-side pieces are `level/block` (0.8 MB, the
  block behaviour tables), `data/lang` (0.5 MB, translations for chat) and the embedded panel
  (0.27 MB). `discordgo` is 0.14 MB. No dead dependency to drop.

## Many bots in one process (not done yet: the plan)

The hub starts **one process per bot** (`internal/hub/hub.go`, `exec.Command`). Each pays for the
runtime, the block tables, the start-up tables and its own heap headroom. `tools/swarmbench`
measures what a bot would cost if it ran as goroutines in a shared process — kit clients with the
same pieces a Cubit bot builds (world, entities, inventory, controller, physics, `pathing`
walker), 10 in one process, radius 6:

| | RSS | live heap |
|---|---:|---:|
| process, no clients | 23.0 MB | 4.9 MB |
| 1 client | 32.1 MB | 9.9 MB |
| 10 clients | 84.3 MB | 54.1 MB |
| **each extra client** | **5.8 MB** | **4.9 MB** |

So ten bots would take ~90 MB in one process instead of 10 × 26 MB (PSS) ≈ 260 MB as processes:
roughly **4-5× less per extra bot**. (The combat and task modules and each bot's panel are not in
swarmbench; they are small structures and a few goroutines.) Azalea's ~1 MB per extra bot comes
from sharing *one* world between the bots of a swarm on the same server; here each client keeps
its own chunks (the 4.9 MB above is mostly that), which is the next step after a shared process.

### Why it is not in this change

The `main` package keeps one bot's state in package-level variables: **88 variables, 466 uses**
(counted with `go/types`). About 40 are per-bot state that would have to move into the `Bot` or a
per-bot environment — the most used: `cfg` (49 uses), `botName` (31), `workQueue` (30), `llmDL`
(26), `cfgMu` (25), `travel` (23), `serverAddr` (16), `orderFrom` (15), `session` (15). On top of
that:

- **logging** is the global `log` package (176 calls in `main`, 37 in `pathing`/`automation`/
  `combat`), and each bot's panel log is fed by `log.SetOutput`: in one process every bot's log
  would be mixed;
- **files** are relative to the process's working directory (`bots/<id>/`): `bot.json`,
  `caminos.json`, `almacen.json`, `logs/`;
- **process-wide actions**: `log.Fatal`/`os.Exit` (11 places), `restartSelf`, the console,
  signal handling, `rcon.OpOwners`, `hub.ViaProxy`.

Doing all of that at once touches nearly every file of `main` while other work changes the same
files; the risk of breaking a bot in a way the tests do not catch is high. It should be done in
steps that each keep the default (one process per bot) working:

1. **A per-bot environment.** `type botEnv struct { dir string; cfg *config; cfgMu sync.Mutex;
   log *log.Logger; name, server string; ... }`, hung from `Bot` and passed where a free function
   now reads a global. `dataFile(name)` becomes `env.file(name)` (it already goes through
   `dataDir`). One process creates one env from the flags: behaviour unchanged.
2. **Logging through the env.** Replace `log.Printf` in `main` with `b.env.log.Printf` and give
   `pathing`/`automation`/`combat` a `Logf` like `safety` already has. The panel's log ring reads
   the env's writer instead of `log.SetOutput`.
3. **Move the mutable globals** into the env or `Bot`, file by file (`queue.go`, `travel.go`,
   `work.go`, `drop.go`, `llm*.go`, `players.go`, `friends.go` …), each step with its tests.
   Flags are read once into the env; nothing below `main()` reads a flag.
4. **Process-wide actions** become per-bot: `log.Fatal` → return an error, `restartSelf` → ask
   the hub, `appQuit` → cancel the bot's context.
5. **Hub mode `shared`** (opt-in: `"shared": true` in `hub.json`): `startBot` calls
   `runBot(ctx, env)` in a goroutine instead of `exec.Command`. Each bot still serves its panel on
   its own `127.0.0.1` port, so the hub's proxy, `/events`, `/log` and the Discord bridge do not
   change. A panic in one bot is recovered in its goroutine (`recover`) and reported like a
   process exit; `--memlimit` becomes the hub's. Separate processes stay the default and the
   fallback (a bot that crashes the runtime takes only itself down).
6. **Share across bots** what is immutable per server: the decoded registries (1.1 MB per client
   now) and, later, one chunk store per server and dimension for all the bots on it (the
   Azalea approach), which would bring the extra bot from ~5 MB to ~1-2 MB.

`internal/memtune` and `tools/swarmbench` are already written so that the shared mode can use
them unchanged.

## Reproducing

```sh
go build -o /tmp/fakesrv ./tools/fakesrv
/tmp/fakesrv -radius 6 -max 5 -compress 256          # prints its address
MCBOT_PANEL_TOKEN=perf ./cubit --server <address> --name Perf1 --reconnect=false \
    --monitor 127.0.0.1:8171 </dev/null &              # one per bot, own folder and port
curl -H 'Authorization: Bearer perf' 'http://127.0.0.1:8171/debug/pprof/heap?gc=1&debug=1'
grep -E 'Pss|Anonymous' /proc/<pid>/smaps_rollup
GODEBUG=inittrace=1 ./cubit --version                 # start-up cost per package
go run ./tools/swarmbench -n 10 -radius 6             # bots in one process
go run ./tools/chunkmem -region <world>/region/r.0.0.mca
go test ./third_party/go-mc26/net/packet -bench UnpackCompressed   # (from that module's folder)
```
