#!/usr/bin/env bash
# Todo de una vez: servidor de Minecraft + IA (Ollama, con la GPU si hay) + el bot.
#
#   ./up.sh [tu_nombre]    # levanta todo (el servidor de pruebas, la IA y el hub con sus bots)
#   ./up.sh down           # lo para todo
#   ./up.sh logs           # los logs del servidor y la IA
#
# Solo hace falta Docker (con compose). La configuración, en .env.
set -euo pipefail
cd "$(dirname "$0")"

files=(-f compose.yaml)
if [ -e /dev/kfd ] && [ "${GPU:-}" != cpu ]; then
  files+=(-f compose.amd.yaml); gpu="AMD (ROCm)"
elif command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1 && [ "${GPU:-}" != cpu ]; then
  files+=(-f compose.nvidia.yaml); gpu="NVIDIA"
else
  gpu="CPU (lento: la IA tarda en contestar)"
fi
dc() { docker compose "${files[@]}" "$@"; }

case "${1:-}" in
down) dc down; exit ;;
logs) dc logs -f --tail 50 bot minecraft ollama; exit ;;
esac

if [ ! -f .env ]; then
  cp .env.example .env
  owner="${1:-${USER:-}}"
  sed -i "s/^OWNER=.*/OWNER=${owner}/" .env
  # los modelos que ya tengas de Ollama, sin volver a bajarlos
  for d in "$HOME/.ollama/models" /usr/share/ollama/.ollama/models; do
    if [ -d "$d" ]; then sed -i "s|^OLLAMA_MODELS=.*|OLLAMA_MODELS=$d|" .env; break; fi
  done
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

echo "IA en: $gpu"
dc up -d --build
set -a; . ./.env; set +a
sleep 2
tok=$(dc exec -T bot sh -c 'sed -n "s/.*\"token\": \"\(.*\)\".*/\1/p" /data/hub.json' 2>/dev/null | tr -d '\r')
echo
echo "  Panel:      http://localhost:${PANEL_PORT:-8080}/?t=${tok}   (los bots: crear, arrancar, parar, ajustes)"
echo "  Minecraft:  localhost:${MC_PORT:-25565}   (el servidor de pruebas: Multijugador → Conexión directa)"
echo
echo "  ./up.sh logs para ver qué pasa · ./up.sh down para pararlo todo"
