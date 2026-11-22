#!/bin/sh
# render.sh: pasa los SVG del logo a brand/png/ con Chromium (sin cabecera),
# ImageMagick y Python 3 (para el .ico). Cada PNG se dibuja a su tamaño final, sin reescalar.
#   node gen.mjs && ./render.sh
set -e
D=$(cd "$(dirname "$0")" && pwd)
B=$(dirname "$D")
O="$B/png"
CHROME=${CHROME:-chromium}
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

# shot svg salida ancho alto: dibuja el SVG en un <img> de ese tamaño
shot() {
	printf '<!doctype html><html><body style="margin:0;background:transparent"><img src="file://%s" width="%s" height="%s" style="display:block"></body></html>' "$1" "$3" "$4" >"$T/p.html"
	w=$(($3 < 400 ? 400 : $3)) h=$(($4 < 400 ? 400 : $4))
	"$CHROME" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
		--force-device-scale-factor=1 --window-size="$w,$h" --virtual-time-budget=3000 \
		--user-data-dir="$T/u" --screenshot="$T/s.png" "file://$T/p.html" >/dev/null 2>&1
	magick "$T/s.png" -crop "$3x$4+0+0" +repage -strip "$2"
	case "$2" in "$B"/*) echo "${2#"$B"/}" ;; esac
}

shot "$B/icon.svg" "$O/icon-512.png" 512 512
shot "$B/icon.svg" "$O/icon-180.png" 180 180
shot "$B/favicon.svg" "$O/icon-32.png" 32 32
shot "$B/favicon.svg" "$O/icon-16.png" 16 16
shot "$B/logo.svg" "$O/logo-470.png" 470 200
shot "$B/logo.svg" "$O/logo-1128.png" 1128 480
shot "$D/avatar.svg" "$O/avatar-1024.png" 1024 1024

# .ico de Windows: 16 y 32 del favicon, 48 de la variante pequeña y 256 del icono
shot "$D/icon-small.svg" "$T/48.png" 48 48
shot "$B/icon.svg" "$T/256.png" 256 256
python3 -I "$D/ico.py" "$O/cubit.ico" "$O/icon-16.png" "$O/icon-32.png" "$T/48.png" "$T/256.png"
echo png/cubit.ico
