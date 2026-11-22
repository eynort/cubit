#!/bin/sh
# Cubit: instalador para Linux (sin root). / Cubit installer for Linux (no root).
#
#   curl -fsSL https://github.com/eynort/cubit/releases/latest/download/install.sh | sh
#   sh install.sh --uninstall           # lo quita (los datos se conservan) / removes it (data is kept)
#   sh install.sh --uninstall --purge   # y borra también los datos / and deletes the data too
#
# Descarga el binario de la última versión, comprueba su SHA256 contra SHA256SUMS,
# lo instala en ~/.local/bin y añade el servicio de usuario de systemd y el lanzador.
# CUBIT_VERSION=v1.2.3 instala una versión concreta. / CUBIT_VERSION=v1.2.3 pins a version.
set -eu

REPO="eynort/cubit"
BIN_DIR="${HOME}/.local/bin"
DATA_HOME="${XDG_DATA_HOME:-${HOME}/.local/share}"
CONFIG_HOME="${XDG_CONFIG_HOME:-${HOME}/.config}"
APP_DIR="${DATA_HOME}/applications"
ICON_DIR="${DATA_HOME}/icons/hicolor/512x512/apps"
UNIT_DIR="${CONFIG_HOME}/systemd/user"
DATA_DIR="${DATA_HOME}/cubit"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

uninstall() {
	if have systemctl; then
		systemctl --user disable --now cubit >/dev/null 2>&1 || true
	fi
	rm -f "${BIN_DIR}/cubit" "${BIN_DIR}/cubit-panel" "${UNIT_DIR}/cubit.service" \
		"${APP_DIR}/cubit.desktop" "${ICON_DIR}/cubit.png" "${DATA_DIR}/install-method"
	if have systemctl; then
		systemctl --user daemon-reload >/dev/null 2>&1 || true
	fi
	say "Cubit uninstalled / Cubit desinstalado."
	if [ "${1:-}" = "--purge" ]; then
		rm -rf "${DATA_DIR}"
		say "Data deleted / Datos borrados: ${DATA_DIR}"
	else
		say "Your data is still in / Tus datos siguen en: ${DATA_DIR} (use --uninstall --purge to delete / para borrarlos)"
	fi
}

case "${1:-}" in
--uninstall)
	uninstall "${2:-}"
	exit 0
	;;
"") ;;
*) die "unknown option: $1 (only --uninstall [--purge])" ;;
esac

[ "$(uname -s)" = "Linux" ] || die "this installer is for Linux; on macOS download the binary from https://github.com/${REPO}/releases"

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

if have curl; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif have wget; then
	fetch() { wget -q -O "$2" "$1"; }
else
	die "curl or wget is required"
fi

if have sha256sum; then
	sha() { sha256sum "$1" | cut -d ' ' -f 1; }
elif have shasum; then
	sha() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	die "sha256sum is required"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

if [ -n "${CUBIT_VERSION:-}" ]; then
	base="https://github.com/${REPO}/releases/download/${CUBIT_VERSION}"
else
	base="https://github.com/${REPO}/releases/latest/download"
fi

say "Downloading checksums..."
fetch "${base}/SHA256SUMS" "${tmp}/SHA256SUMS" || die "could not download SHA256SUMS from ${base}"

# El nombre del binario lleva la versión: cubit-<versión>-linux-<arch>
name="$(awk -v suf="-linux-${arch}" '{ f = $2; sub(/^\*/, "", f); if (f ~ /^cubit-.*/ && substr(f, length(f) - length(suf) + 1) == suf) { print f; exit } }' "${tmp}/SHA256SUMS")"
[ -n "$name" ] || die "no linux-${arch} binary in this release"
want="$(awk -v n="$name" '{ f = $2; sub(/^\*/, "", f); if (f == n) { print $1; exit } }' "${tmp}/SHA256SUMS")"

say "Downloading ${name}..."
fetch "${base}/${name}" "${tmp}/cubit" || die "download failed"
got="$(sha "${tmp}/cubit")"
[ "$got" = "$want" ] || die "SHA256 mismatch for ${name} (expected ${want}, got ${got}); nothing was installed"

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR" "$UNIT_DIR" "$DATA_DIR"
install -m 0755 "${tmp}/cubit" "${BIN_DIR}/cubit"
# cómo se instaló (para el actualizador) / how it was installed (for the self-updater)
printf 'script\n' >"${DATA_DIR}/install-method"

cat >"${BIN_DIR}/cubit-panel" <<'EOF'
#!/bin/sh
# Abre el panel de Cubit en el navegador (arrancando el hub si no está en marcha).
CUBIT_DATA="${CUBIT_DATA:-${XDG_DATA_HOME:-$HOME/.local/share}/cubit}"
export CUBIT_DATA
exec "$HOME/.local/bin/cubit" --hub :8080 --open "$@"
EOF
chmod 0755 "${BIN_DIR}/cubit-panel"

cat >"${UNIT_DIR}/cubit.service" <<EOF
[Unit]
Description=Cubit (Minecraft bot panel)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=${BIN_DIR}/cubit --hub :8080 --data ${DATA_DIR}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF

# el icono es opcional: si falla, el lanzador sale sin él
if fetch "${base}/cubit-icon.png" "${tmp}/icon.png" 2>/dev/null; then
	icon_want="$(awk '{ f = $2; sub(/^\*/, "", f); if (f == "cubit-icon.png") { print $1; exit } }' "${tmp}/SHA256SUMS")"
	if [ -n "$icon_want" ] && [ "$(sha "${tmp}/icon.png")" = "$icon_want" ]; then
		install -m 0644 "${tmp}/icon.png" "${ICON_DIR}/cubit.png"
	fi
fi

cat >"${APP_DIR}/cubit.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Cubit
GenericName=Minecraft bot
Comment=A Minecraft bot that fights, digs and moves: opens the panel
Exec=${BIN_DIR}/cubit-panel
Icon=cubit
Terminal=false
Categories=Game;Utility;
Keywords=minecraft;bot;
EOF

if have update-desktop-database; then
	update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi
if have systemctl; then
	systemctl --user daemon-reload >/dev/null 2>&1 || true
fi

say ""
say "Cubit installed in ${BIN_DIR}/cubit (data: ${DATA_DIR})"
case ":${PATH}:" in
*":${BIN_DIR}:"*) ;;
*) say "Note: ${BIN_DIR} is not in your PATH; add it to run 'cubit' from a terminal." ;;
esac
say ""
say "Start the panel:"
say "  ${BIN_DIR}/cubit-panel                       # opens it in your browser (or find 'Cubit' in your app menu)"
say "  systemctl --user enable --now cubit          # or keep it always on, in the background"
say "  journalctl --user -u cubit | grep panel      # prints the panel URL with your access key"
if [ -f "${DATA_DIR}/hub.json" ]; then
	token="$(sed -n 's/.*"token": *"\([0-9a-f]*\)".*/\1/p' "${DATA_DIR}/hub.json" | head -n 1)"
	[ -z "$token" ] || say "Your panel: http://localhost:8080/?t=${token}"
else
	say "The panel URL (with your access key) is printed the first time it starts."
fi
say ""
say "Remove it with: sh install.sh --uninstall"
