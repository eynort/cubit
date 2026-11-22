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

**Zip de Windows** (el que usan Scoop y la publicación): compila `cubit.exe` y comprímelo con `packaging/windows/README.txt` y `LICENSE`. El manifiesto de Scoop es `bucket/cubit.json` y las plantillas de winget están en `packaging/winget/`.

**Método de instalación.** Las compilaciones de publicación ponen `-ldflags "-X cubit/internal/update.Method=<zip|scoop|winget|deb|rpm>"`; los binarios sueltos y `install.sh` lo dejan vacío y `install.sh` escribe `install-method` (`script`) en la carpeta de datos. Las compilaciones de desarrollo lo dejan vacío.

**.deb y .rpm** (con [nfpm](https://nfpm.goreleaser.com); la publicación usa `v2.41.1`):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1
go build -o dist/cubit .
for fmt in deb rpm; do
  VERSION=0.0.0 GOARCH=amd64 CUBIT_BIN=dist/cubit nfpm package -f packaging/linux/nfpm.yaml -p $fmt -t dist/
done
```

`packaging/linux/install.sh` es el instalador de una línea que se publica con cada versión.

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

Al subir una etiqueta `v*` se ejecuta `.github/workflows/release.yml`: pasa las pruebas, compila todos los binarios, el `.deb`/`.rpm`, los zips de Windows, `install.sh` y `SHA256SUMS`, y lo adjunta todo a la versión.
