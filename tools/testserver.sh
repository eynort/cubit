#!/usr/bin/env bash
# Un servidor de Minecraft de verdad en local, para probar el bot sin
# molestar a nadie: vanilla 26.2, modo offline (entra cualquier cliente,
# también el tuyo), en Docker (no hace falta instalar Java).
#
#   tools/testserver.sh start [tu_nombre]   # arranca (y te hace op)
#   tools/testserver.sh logs                # ver la consola
#   tools/testserver.sh cmd "time set day"  # un comando de consola
#   tools/testserver.sh stop | reset        # parar | borrar el mundo
#
# Luego:  ./cubit --name Test_Bot --owner <tu_nombre> localhost
# y tú entras en Minecraft (multijugador) a localhost:25565.
set -euo pipefail
NAME=mcbot-testserver
DATA="$(cd "$(dirname "$0")/.." && pwd)/testserver"
VERSION="${MC_VERSION:-26.2}"
PORT="${MC_PORT:-25565}"

case "${1:-start}" in
start)
  if docker ps --format '{{.Names}}' | grep -qx "$NAME"; then
    echo "ya está en marcha: localhost:$PORT"; exit 0
  fi
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  mkdir -p "$DATA"
  docker run -d --name "$NAME" -p "$PORT:25565" -v "$DATA:/data" \
    -e EULA=TRUE -e TYPE=VANILLA -e VERSION="$VERSION" \
    -e ONLINE_MODE=FALSE -e ENFORCE_SECURE_PROFILE=FALSE \
    -e DIFFICULTY="${MC_DIFFICULTY:-easy}" -e MODE=survival \
    -e VIEW_DISTANCE=8 -e SPAWN_PROTECTION=0 -e MEMORY=2G \
    -e MOTD="Cubit test" ${2:+-e OPS="$2"} \
    itzg/minecraft-server >/dev/null
  echo "arrancando Minecraft $VERSION (la primera vez descarga el servidor y genera el mundo: 1-2 min)…"
  for _ in $(seq 1 180); do
    if docker logs "$NAME" 2>&1 | grep -q 'Done ('; then
      echo "listo: localhost:$PORT (mundo en $DATA)"; exit 0
    fi
    if ! docker ps --format '{{.Names}}' | grep -qx "$NAME"; then
      echo "el servidor se paró:"; docker logs --tail 30 "$NAME"; exit 1
    fi
    sleep 2
  done
  echo "tarda demasiado; mira: $0 logs"; exit 1 ;;
logs) docker logs -f --tail 50 "$NAME" ;;
cmd) shift; docker exec "$NAME" rcon-cli "$@" ;;
stop) docker stop "$NAME" >/dev/null && echo parado ;;
reset) docker rm -f "$NAME" >/dev/null 2>&1 || true; rm -rf "$DATA"; echo "mundo borrado" ;;
*) sed -n '2,13p' "$0"; exit 1 ;;
esac
