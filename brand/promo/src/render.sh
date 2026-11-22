#!/bin/sh
# render.sh [filtro]: pasa las páginas de esta carpeta a ../<nombre>.png con su
# tamaño y escala (list.txt: nombre ancho alto escala). Hace falta chromium.
set -e
D=$(cd "$(dirname "$0")" && pwd)
CHROME=${CHROME:-chromium}
while read -r n w h s; do
	[ -n "$1" ] && case "$n" in *"$1"*) ;; *) continue ;; esac
	U=$(mktemp -d)
	"$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor="$s" \
		--window-size="$w,$h" --virtual-time-budget=3000 --user-data-dir="$U" \
		--screenshot="$D/../$n.png" "file://$D/$n.html" >/dev/null 2>&1
	rm -rf "$U"
	echo "$n.png"
done <"$D/list.txt"
