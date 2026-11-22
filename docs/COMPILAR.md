# Compílalo tú mismo

[English](BUILD.md) · [Guía](GUIA.md)

## Requisitos

- **Go 1.27.4** o más nuevo (la versión exacta es la línea `go` de `go.mod`).
- **git**.

No hace falta compilador de C: todo se compila con `CGO_ENABLED=0`.

## Clonar, compilar, ejecutar

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
go build -o cubit .
./cubit --hub :8080 --open      # el panel, abierto en tu navegador
```

`--data <carpeta>` (o `CUBIT_DATA`) elige dónde viven `hub.json` y `bots/`; por defecto, la carpeta actual.

## Compilar para otros sistemas

Go compila para otros sistemas desde cualquiera. Es lo que hace el flujo de publicación:

```sh
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/cubit-linux-amd64 .
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-linux-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/cubit-windows-amd64.exe .
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-windows-arm64.exe .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-darwin-arm64 .
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-android-arm64 .   # Termux
```

## Construir los instaladores en local

**Instalador de Windows** (en Windows, con [Inno Setup 6.1+](https://jrsoftware.org/isinfo.php)):

```powershell
go build -o dist\cubit.exe .
iscc /DAppVersion=0.0.0-dev /DExePath=dist\cubit.exe packaging\windows\cubit.iss
```

Sale `dist\cubit-0.0.0-dev-windows-setup.exe`. El icono es `packaging/windows/cubit.ico`, hecho a partir de `brand/png/icon-512.png`:
`magick brand/png/icon-512.png -background none -define icon:auto-resize=256,128,64,48,32,16 packaging/windows/cubit.ico`.

**.deb y .rpm** (con [nfpm](https://nfpm.goreleaser.com); la publicación usa `v2.41.1`):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1
go build -o dist/cubit .
for fmt in deb rpm; do
  VERSION=0.0.0 GOARCH=amd64 CUBIT_BIN=dist/cubit nfpm package -f packaging/linux/nfpm.yaml -p $fmt -t dist/
done
```

`packaging/linux/install.sh` y `packaging/windows/install.ps1` son los instaladores de una línea que se publican con cada versión.

## Pruebas

```sh
go vet ./...
go test ./...
```

## Regenerar la documentación

`docs/COMMANDS.md` y `docs/ORDENES.md` se generan del código; no las edites a mano:

```sh
go run . --gen-docs docs
```

## Publicar una versión

Al subir una etiqueta `v*` se ejecuta `.github/workflows/release.yml`: pasa las pruebas, compila todos los binarios, el `.deb`/`.rpm`, el instalador de Windows, `install.sh`, `install.ps1` y `SHA256SUMS`, y lo adjunta todo a la versión.
