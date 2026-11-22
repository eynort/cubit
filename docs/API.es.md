# API HTTP de Cubit

El hub (`--hub :8080`, o la instalación con Docker) ofrece una API HTTP pequeña para listar tus bots, ver su estado, mandarles órdenes, leer su log y seguir en directo su chat y sus tareas. Todo es JSON menos el flujo de eventos.

Versión en inglés: [API.md](API.md).

## Autenticación

Cada petición lleva la clave del hub, la misma de la URL del panel (`http://host:8080/?t=<clave>`, sale en el log del hub y se guarda en `hub.json`).

Mándala en una cabecera (mejor: así no queda en logs ni en el historial del navegador):

```
Authorization: Bearer <clave>
```

También vale `?t=<clave>`. Una clave mala da `401`; más de 20 claves malas en un minuto desde una IP dan `429` (con `Retry-After`) durante un rato, aunque luego acierte.

La API no lleva cabeceras CORS ni TLS: si se llega desde fuera de tu red, ponla detrás de un proxy inverso con HTTPS.

Los ejemplos usan:

```sh
export HUB=http://localhost:8080
export KEY=tu_clave
```

Los mensajes de error son texto plano en inglés (manda `X-Lang: es` para verlos en español).

## Rutas

| Método | Ruta | Qué |
|---|---|---|
| GET | `/api/bots` | lista de bots |
| GET | `/api/bots/<id>` | un bot: estado, tarea, cola |
| POST | `/api/bots/<id>/command` | mandar una línea de consola a un bot |
| GET | `/api/bots/<id>/log?after=<n>` | las últimas líneas del log |
| GET | `/api/events` | eventos en directo (Server-Sent Events) |
| POST | `/api/bots/<id>/start` · `stop` | encender o apagar un bot |

### Listar los bots

```sh
curl -H "Authorization: Bearer $KEY" $HUB/api/bots
```

```json
[
  {
    "id": "mibot", "name": "MiBot", "server": "localhost:25565", "server_id": "test",
    "on_test": true, "owners": ["Steve"],
    "running": true, "connected": true, "health": 20,
    "task": "talar (12 árboles)",
    "queue": ["/chop oak 10 64 10"], "queue_paused": false,
    "avatar": "/avatar/MiBot"
  }
]
```

`running`: el proceso está en marcha. `connected`: está dentro del servidor. `task` va vacío si no hace nada. `queue` son las órdenes que esperan tras la tarea actual (mira `/queue` en [ORDENES.md](ORDENES.md)); `queue_paused` es true tras un fallo o un `/stop`.

### Un bot

```sh
curl -H "Authorization: Bearer $KEY" $HUB/api/bots/mibot
```

El mismo objeto que un elemento de la lista. `404` si el id no existe. El id es el campo `id` de la lista.

### Mandar una orden

```sh
curl -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"line": "/chop oak ~ ~ ~"}' $HUB/api/bots/mibot/command
```

`line` es cualquier cosa que escribirías en la consola del bot: una orden, una cadena (`/mine iron ~ ~ ~ ; /chop oak 10 64 10`) o texto para el chat del juego. Contesta `202 {"status":"sent"}` en cuanto el bot la recibe; no espera a que acabe. El resultado lo ves en el flujo de eventos o en el log.

Errores: `400` vacía, demasiado larga (más de 1024 bytes) o con saltos de línea; `404` no hay ese bot; `409` el bot está apagado.

### Log reciente

```sh
curl -H "Authorization: Bearer $KEY" "$HUB/api/bots/mibot/log?after=0"
```

```json
{ "lines": ["dentro del servidor", "<Steve> hola"], "next": 57 }
```

El bot guarda sus últimas 400 líneas. Pasa el `next` anterior como `after` para recibir solo las nuevas. `409` si el bot está apagado.

### Encender y apagar

```sh
curl -X POST -H "Authorization: Bearer $KEY" $HUB/api/bots/mibot/start
curl -X POST -H "Authorization: Bearer $KEY" $HUB/api/bots/mibot/stop
```

`204` si sale bien.

### Eventos en directo

```sh
curl -N -H "Authorization: Bearer $KEY" "$HUB/api/events"
curl -N -H "Authorization: Bearer $KEY" "$HUB/api/events?bot=mibot&type=chat,task"
```

Un flujo Server-Sent Events (`text/event-stream`) con los eventos de todos los bots en marcha. Filtros, ambos opcionales y separados por comas: `bot` (ids) y `type`. Cada 15 segundos llega una línea de comentario `: keepalive`. En un navegador sirve `EventSource`, pero no puede poner cabeceras: ahí usa `?t=<clave>`.

Cada evento tiene como nombre SSE su tipo, y un `data` JSON:

```
event: chat
data: {"bot":"mibot","name":"MiBot","time":"2026-11-21T18:30:02Z","type":"chat","from":"Steve","text":"¡hola!"}

event: task
data: {"bot":"mibot","name":"MiBot","time":"2026-11-21T18:31:40Z","type":"task","task":"talar","status":"finished"}
```

| tipo | cuándo | campos |
|---|---|---|
| `chat` | un jugador escribe en el chat | `from`, `text` |
| `whisper` | un jugador le susurra al bot | `from`, `text` |
| `system` | un mensaje del servidor (entradas, muertes…) | `text` |
| `task` | una tarea empieza, acaba, falla o se para | `task` (`excavar`, `talar`, `minar`…), `status` (`started`, `finished`, `failed`, `stopped`), `text` (por qué falló) |
| `queue` | cambia la cola | `data.queue` (lista), `data.paused` |
| `bot` | el proceso del bot arranca o se para | `status` (`started`, `stopped`) |

Notas: los eventos no se guardan, un cliente solo ve lo que pasa mientras está conectado (no hay repetición con `Last-Event-ID`); un cliente que lee muy despacio pierde eventos en vez de frenar a los bots. Los eventos se piden a los bots una vez por segundo, así que cuenta con un segundo de retraso.

## Ejemplo: un script pequeño

```sh
# mandar una cadena y mirar qué pasa
curl -s -X POST -H "Authorization: Bearer $KEY" -d '{"line":"/mine iron ~ ~ ~ ; /chop oak 10 64 10"}' $HUB/api/bots/mibot/command
curl -sN -H "Authorization: Bearer $KEY" "$HUB/api/events?bot=mibot" | grep --line-buffered '^data:'
```

## Para código Go dentro del programa

`internal/hub` exporta funciones pequeñas para que otros módulos (un puente de chat, por ejemplo) no necesiten HTTP:

```go
hub.SendCommand("mibot", "/status")  // como POST .../command
ch, cancel := hub.Subscribe(64)      // valores hub.Event de todos los bots
defer cancel()
hub.Bots()                           // como GET /api/bots
```

Solo funcionan mientras el programa corre en modo hub (si no, `hub.ErrNoHub`).

## Un bot solo (`--monitor`, sin hub)

El panel de un bot solo tiene `/json` (estado, con `task` y `queue`), `/log`, `POST /cmd` y `/events?after=<n>`, con la misma clave; el flujo `/api/events` solo existe en el hub.
