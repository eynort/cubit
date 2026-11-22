#!/usr/bin/env bash
# Tests del CI: si un paquete falla, se repite una vez solo ese paquete.
# Algunos tests de combate simulan peleas en tiempo real y, con la máquina
# cargada, fallan de tarde en tarde (~1 de cada 50); un fallo de verdad
# falla también la segunda vez.
set -uo pipefail
out=$(mktemp)
go test -count=1 ./... 2>&1 | tee "$out"
status=${PIPESTATUS[0]}
[ "$status" -eq 0 ] && exit 0
failed=$(grep -E '^FAIL[[:space:]]+[^[:space:]]+[[:space:]]' "$out" | awk '{print $2}' | sort -u)
[ -n "$failed" ] || exit "$status"
echo "::warning::repito una vez los paquetes que fallaron: $failed"
# shellcheck disable=SC2086
go test -count=1 $failed
