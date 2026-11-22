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

**Windows zip** (what Scoop and the release use): just build `cubit.exe` and zip it with `packaging/windows/README.txt` and `LICENSE`. The Scoop manifest is `bucket/cubit.json` and the winget templates are in `packaging/winget/`.

**Install method.** Release builds set `-ldflags "-X cubit/internal/update.Method=<zip|scoop|winget|deb|rpm>"`; the plain binaries and `install.sh` installs leave it empty and `install.sh` writes `install-method` (`script`) in the data folder. Dev builds leave it empty.

**.deb and .rpm** (with [nfpm](https://nfpm.goreleaser.com); the release uses `v2.41.1`):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.1
go build -o dist/cubit .
for fmt in deb rpm; do
  VERSION=0.0.0 GOARCH=amd64 CUBIT_BIN=dist/cubit nfpm package -f packaging/linux/nfpm.yaml -p $fmt -t dist/
done
```

`packaging/linux/install.sh` is the one-line installer published with each release.

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

Pushing a `v*` tag runs `.github/workflows/release.yml`: it tests, builds every binary, the `.deb`/`.rpm`, the Windows zips, `install.sh` and `SHA256SUMS`, and attaches them all to the release.
