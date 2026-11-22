# Compile it yourself

[Español](COMPILAR.md) · [Guide](GUIDE.md)

## Prerequisites

- **Go 1.27.4** or newer (the exact version is the `go` line in `go.mod`).
- **git**.

No C compiler is needed: everything builds with `CGO_ENABLED=0`.

## Clone, build, run

```sh
git clone https://github.com/eynort/cubit.git
cd cubit
go build -o cubit .
./cubit --hub :8080 --open      # the panel, opened in your browser
```

`--data <folder>` (or `CUBIT_DATA`) picks where `hub.json` and `bots/` live; by default it is the current folder.

## Cross-compiling

Go cross-compiles from any system. This is what the release workflow does:

```sh
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/cubit-linux-amd64 .
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-linux-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/cubit-windows-amd64.exe .
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-windows-arm64.exe .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-darwin-arm64 .
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/cubit-android-arm64 .   # Termux
```

## Building the installers locally

**Windows installer** (on Windows, with [Inno Setup 6.1+](https://jrsoftware.org/isinfo.php)):

```powershell
go build -o dist\cubit.exe .
iscc /DAppVersion=0.0.0-dev /DExePath=dist\cubit.exe packaging\windows\cubit.iss
```

The result is `dist\cubit-0.0.0-dev-windows-setup.exe`. The icon is `packaging/windows/cubit.ico`, made from `brand/png/icon-512.png`:
`magick brand/png/icon-512.png -background none -define icon:auto-resize=256,128,64,48,32,16 packaging/windows/cubit.ico`.

**.deb and .rpm** (with [nfpm](https://nfpm.goreleaser.com); the release uses `v2.41.1`):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1
go build -o dist/cubit .
for fmt in deb rpm; do
  VERSION=0.0.0 GOARCH=amd64 CUBIT_BIN=dist/cubit nfpm package -f packaging/linux/nfpm.yaml -p $fmt -t dist/
done
```

`packaging/linux/install.sh` and `packaging/windows/install.ps1` are the one-line installers published with each release.

## Tests

```sh
go vet ./...
go test ./...
```

## Regenerating the docs

`docs/COMMANDS.md` and `docs/ORDENES.md` are generated from the code; don't edit them by hand:

```sh
go run . --gen-docs docs
```

## Releasing

Pushing a `v*` tag runs `.github/workflows/release.yml`: it tests, builds every binary, the `.deb`/`.rpm`, the Windows installer, `install.sh`, `install.ps1` and `SHA256SUMS`, and attaches them all to the release.
