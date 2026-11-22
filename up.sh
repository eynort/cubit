#!/usr/bin/env bash
# Cubit, todo de una vez: el servidor de pruebas (Minecraft vanilla) y el panel con los bots.
# Cubit, all at once: the test server (vanilla Minecraft) and the panel with the bots.
#
#   ./up.sh [tu_nombre]    # lo levanta todo y te da la URL del panel / starts everything, prints the panel URL
#   ./up.sh down           # lo para todo / stops everything
#   ./up.sh logs           # lo que pasa / what is going on
#   ./up.sh update         # actualiza Cubit (git pull + reconstruye solo el bot) / updates Cubit (pull + rebuild the bot only)
#
# Solo hace falta Docker (con compose). La configuración, en .env. / Only Docker (with compose) is needed.
set -euo pipefail
cd "$(dirname "$0")"

dc() { docker compose "$@"; }
# sed -i que vale en Linux y en macOS (BSD sed pide un sufijo) / works with GNU and BSD sed
sedi() { sed -i.bak "$@" && rm -f "${@: -1}.bak"; }

case "${1:-}" in
down) dc down; exit ;;
logs) shift; [ $# -gt 0 ] || set -- bot minecraft ollama-pull; dc logs -f --tail 50 "$@"; exit ;;
update)
  # Solo el servicio "bot" (el hub y sus bots): el servidor de Minecraft y su mundo (mc-data) no se tocan.
  # Only the "bot" service (hub + bots): the Minecraft server and its world (mc-data) are left alone.
  echo "Actualizando Cubit… los bots se desconectan unos segundos y vuelven solos."
  echo "Updating Cubit… the bots disconnect for a few seconds and reconnect by themselves."
  git pull --ff-only
  dc build bot
  dc up -d --no-deps bot
  echo "Hecho / Done. ./up.sh logs"
  exit ;;
esac

if [ ! -f .env ]; then
  cp .env.example .env
  owner="${1:-${USER:-}}"
  sedi "s/^OWNER=.*/OWNER=${owner}/" .env
  echo "creado .env (dueño: $owner) / created .env (owner: $owner)"
elif [ -n "${1:-}" ]; then
  sedi "s/^OWNER=.*/OWNER=$1/" .env
fi
# la consola del servidor (RCON) para el panel: una contraseña al azar
if ! grep -q '^RCON_PASSWORD=.' .env; then
  pw=$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9')
  if grep -q '^RCON_PASSWORD=' .env; then sedi "s/^RCON_PASSWORD=.*/RCON_PASSWORD=$pw/" .env; else echo "RCON_PASSWORD=$pw" >> .env; fi
fi
mkdir -p bot-data mc-data
rm -f mc-data/.mcbot-apagado # (levantarlo todo es encender también el servidor de pruebas)

echo "IA local: la primera vez se baja el modelo (~4,6 GB, solo una vez; ./up.sh logs ollama-pull) / local AI: first run downloads the model (~4.6 GB, once)"
dc up -d --build --remove-orphans
set -a; . ./.env; set +a
sleep 2
tok=$(dc exec -T bot sh -c 'sed -n "s/.*\"token\": \"\(.*\)\".*/\1/p" /data/hub.json' 2>/dev/null | tr -d '\r')
echo
echo "  Panel:      http://localhost:${PANEL_PORT:-8080}/?t=${tok}"
echo "  Minecraft:  localhost:${MC_PORT:-25565}   (Java 26.2 · Multiplayer → Direct Connection)"
echo
echo "  ./up.sh logs · ./up.sh down"
