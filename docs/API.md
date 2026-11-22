# Cubit HTTP API

The hub (`--hub :8080`, or the Docker setup) exposes a small HTTP API to list your bots, read their state, send them commands, read their log and follow their chat and tasks live. Everything is JSON except the event stream.

Spanish version: [API.es.md](API.es.md).

## Authentication

Every request needs the hub key, the same one in the panel URL (`http://host:8080/?t=<key>`, printed in the hub log and stored in `hub.json`).

Send it in a header (preferred: it does not end up in logs or browser history):

```
Authorization: Bearer <key>
```

`?t=<key>` also works. A wrong key gives `401`; more than 20 wrong keys in a minute from one IP gives `429` (with `Retry-After`) for a while, even for the right key.

The API has no CORS headers and no TLS: put it behind a reverse proxy with HTTPS if it is reachable from outside your network.

The examples use:

```sh
export HUB=http://localhost:8080
export KEY=your_key_here
```

Error messages are plain text, in English (send `X-Lang: es` for Spanish).

## Endpoints

| Method | Path | What |
|---|---|---|
| GET | `/api/bots` | list of bots |
| GET | `/api/bots/<id>` | one bot: state, task, queue |
| POST | `/api/bots/<id>/command` | send a command line to a bot |
| GET | `/api/bots/<id>/log?after=<n>` | recent log lines |
| GET | `/api/events` | live events (Server-Sent Events) |
| POST | `/api/bots/<id>/start` · `stop` | turn a bot on or off |

### List bots

```sh
curl -H "Authorization: Bearer $KEY" $HUB/api/bots
```

```json
[
  {
    "id": "mybot", "name": "MyBot", "server": "localhost:25565", "server_id": "test",
    "on_test": true, "owners": ["Steve"],
    "running": true, "connected": true, "health": 20,
    "task": "talar (12 árboles)",
    "queue": ["/chop oak 10 64 10"], "queue_paused": false,
    "avatar": "/avatar/MyBot"
  }
]
```

`running`: the process is on. `connected`: it is inside the server. `task` is empty when idle. `queue` lists the commands waiting after the current task (see `/queue` in [COMMANDS.md](COMMANDS.md)); `queue_paused` is true after a failure or `/stop`.

### One bot

```sh
curl -H "Authorization: Bearer $KEY" $HUB/api/bots/mybot
```

Same object as one element of the list. `404` if the id does not exist. The id is the `id` field of the list.

### Send a command

```sh
curl -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"line": "/chop oak ~ ~ ~"}' $HUB/api/bots/mybot/command
```

`line` is anything you could type in the bot's console: a command, a chain (`/mine iron ~ ~ ~ ; /chop oak 10 64 10`) or plain text for the game chat. The answer is `202 {"status":"sent"}` as soon as the bot has received it; it does not wait for the command to finish. Follow the result in the events stream or the log.

Errors: `400` empty, too long (over 1024 bytes) or with line breaks; `404` no such bot; `409` the bot is off.

### Recent log

```sh
curl -H "Authorization: Bearer $KEY" "$HUB/api/bots/mybot/log?after=0"
```

```json
{ "lines": ["dentro del servidor", "<Steve> hola"], "next": 57 }
```

The bot keeps its last 400 lines. Pass the previous `next` as `after` to get only the new ones. `409` if the bot is off.

### Turn on and off

```sh
curl -X POST -H "Authorization: Bearer $KEY" $HUB/api/bots/mybot/start
curl -X POST -H "Authorization: Bearer $KEY" $HUB/api/bots/mybot/stop
```

`204` on success.

### Live events

```sh
curl -N -H "Authorization: Bearer $KEY" "$HUB/api/events"
curl -N -H "Authorization: Bearer $KEY" "$HUB/api/events?bot=mybot&type=chat,task"
```

A Server-Sent Events stream (`text/event-stream`) with the events of all running bots. Filters, both optional and comma-separated: `bot` (ids) and `type`. A comment line `: keepalive` arrives every 15 seconds. Browsers can use `EventSource`, but it cannot set headers: use `?t=<key>` there.

Each event has the SSE name = its type, and a JSON `data`:

```
event: chat
data: {"bot":"mybot","name":"MyBot","time":"2026-11-21T18:30:02Z","type":"chat","from":"Steve","text":"hi!"}

event: task
data: {"bot":"mybot","name":"MyBot","time":"2026-11-21T18:31:40Z","type":"task","task":"talar","status":"finished"}
```

| type | when | fields |
|---|---|---|
| `chat` | a player writes in the chat | `from`, `text` |
| `whisper` | a player whispers to the bot | `from`, `text` |
| `system` | a server message (joins, deaths…) | `text` |
| `task` | a task starts, finishes, fails or is stopped | `task` (`excavar`, `talar`, `minar`…), `status` (`started`, `finished`, `failed`, `stopped`), `text` (why it failed) |
| `queue` | the queue changed | `data.queue` (list), `data.paused` |
| `bot` | the bot process starts or stops | `status` (`started`, `stopped`) |

Notes: events are not stored, a client sees only what happens while it is connected (there is no `Last-Event-ID` replay); a client that reads too slowly loses events instead of slowing the bots. Events are fetched from the bots once a second, so expect about a second of delay.

## Example: a small script

```sh
# send a chain, then watch what happens
curl -s -X POST -H "Authorization: Bearer $KEY" -d '{"line":"/mine iron ~ ~ ~ ; /chop oak 10 64 10"}' $HUB/api/bots/mybot/command
curl -sN -H "Authorization: Bearer $KEY" "$HUB/api/events?bot=mybot" | grep --line-buffered '^data:'
```

## For Go code inside the program

`internal/hub` exports small functions so other modules (a chat bridge, for example) do not need HTTP:

```go
hub.SendCommand("mybot", "/status")  // like POST .../command
ch, cancel := hub.Subscribe(64)      // hub.Event values from all bots
defer cancel()
hub.Bots()                           // like GET /api/bots
```

They work only while the program runs in hub mode (`hub.ErrNoHub` otherwise).

## A bot alone (`--monitor`, without the hub)

A single bot's panel has `/json` (state, with `task` and `queue`), `/log`, `POST /cmd` and `/events?after=<n>`, with the same key; the streaming endpoint `/api/events` exists only in the hub.
