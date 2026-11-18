#!/usr/bin/env bash
# Todo de una vez: el servidor de pruebas (Minecraft vanilla) y el hub con los bots.
#
#   ./up.sh [tu_nombre]    # lo levanta todo y te da la URL del panel
#   ./up.sh down           # lo para todo
#   ./up.sh logs           # lo que pasa (el hub, sus bots y el servidor)
#
# Solo hace falta Docker (con compose). La configuración, en .env.
set -euo pipefail
cd "$(dirname "$0")"

dc() { docker compose "$@"; }

case "${1:-}" in
down) dc down; exit ;;
logs) dc logs -f --tail 50 bot minecraft; exit ;;
esac

if [ ! -f .env ]; then
  cp .env.example .env
  owner="${1:-${USER:-}}"
  sed -i "s/^OWNER=.*/OWNER=${owner}/" .env
  echo "creado .env (dueño: $owner). Cámbialo si quieres y vuelve a lanzar ./up.sh"
elif [ -n "${1:-}" ]; then
  sed -i "s/^OWNER=.*/OWNER=$1/" .env
fi
# la consola del servidor (RCON) para el panel: una contraseña al azar
if ! grep -q '^RCON_PASSWORD=.' .env; then
  pw=$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9')
  if grep -q '^RCON_PASSWORD=' .env; then sed -i "s/^RCON_PASSWORD=.*/RCON_PASSWORD=$pw/" .env; else echo "RCON_PASSWORD=$pw" >> .env; fi
fi
mkdir -p bot-data mc-data
rm -f mc-data/.mcbot-apagado # (levantarlo todo es encender también el servidor de pruebas)

dc up -d --build --remove-orphans
set -a; . ./.env; set +a
sleep 2
tok=$(dc exec -T bot sh -c 'sed -n "s/.*\"token\": \"\(.*\)\".*/\1/p" /data/hub.json' 2>/dev/null | tr -d '\r')
echo
echo "  Panel:      http://localhost:${PANEL_PORT:-8080}/?t=${tok}   (los bots: crear, arrancar, parar, ajustes)"
echo "  Minecraft:  localhost:${MC_PORT:-25565}   (el servidor de pruebas: Multijugador → Conexión directa)"
echo
echo "  ./up.sh logs para ver qué pasa · ./up.sh down para pararlo todo"
