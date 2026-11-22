# Logo sources

Everything in `brand/` (the SVGs and `png/`) is generated from this folder.
The logo is concept A, "badge · mascot": a glossy cube robot inside a sage
badge with a white ring, on a dark rounded tile. Edit the generator, not the
SVGs it writes.

| File | What it is |
|---|---|
| `gen.mjs` | Generator. All shapes are hand-written geometry (robot, badge, tile). |
| `wordmark.json` | "Cubit" (Fredoka 600) and "MINECRAFT BOTS" (Outfit 500) as SVG paths. |
| `outline.py` | Turns the text into `wordmark.json` from the font files. |
| `render.sh` | Renders the SVGs to `brand/png/` with headless Chromium. |
| `ico.py` | Packs PNGs into `brand/png/cubit.ico`. |
| `icon-small.svg` | Small-size variant (no ring, grid or antenna); used for the 48 px `.ico` entry. |
| `panel-mark.svg` | The 28 px badge of the panel header, for reference. |
| `avatar.svg` | Square avatar (GitHub org / user), rendered to `png/avatar-1024.png`. |

## What `gen.mjs` writes

| Output | Use |
|---|---|
| `brand/icon.svg` | App icon (tile + badge + robot), 1024. |
| `brand/symbol.svg` | The badge alone, without the tile (promo images). |
| `brand/favicon.svg` | Favicon. Drawn on a 32 grid with every edge on an even coordinate, so it is pixel-exact at 32 and at 16 px. |
| `brand/logo.svg`, `brand/logo-light.svg` | Lockup (icon + name + tagline) for dark and light backgrounds, 940×400. |
| `internal/web/panel.html` | The two `<symbol>`s of the header sprite (`#bm-cube`, 28 px grid; `#bm-word`, cap height 14 px on a 28 px box) are rewritten in place. |

`render.sh` then writes `brand/png/`: `icon-512` and `icon-180` (from `icon.svg`),
`icon-32` and `icon-16` (from `favicon.svg`, drawn at that size, not scaled down),
`logo-470` and `logo-1128` (from `logo.svg`, transparent), `avatar-1024` and `cubit.ico`
(16, 32, 48, 256). `brand/brand.go` embeds `favicon.svg`, `icon-32.png` and
`icon-180.png` for the panel.

## Rebuild

Needs Node, Chromium (`CHROME=/path/to/chrome` if it is not `chromium`),
ImageMagick 7 (`magick`) and Python 3.

```sh
cd brand/src
node gen.mjs        # SVGs + panel sprite
./render.sh         # brand/png/*
cd ../promo/src && node gen.mjs && ./render.sh   # promo images use symbol.svg + wordmark.json
```

Only if the name, the tagline, a font or the spacing changes, regenerate
`wordmark.json` first. The fonts are not kept in the repo; get the variable
TTFs from github.com/google/fonts (`ofl/fredoka/Fredoka[wdth,wght].ttf` and
`ofl/outfit/Outfit[wght].ttf`) and run:

```sh
python3 -m venv /tmp/ft && /tmp/ft/bin/pip install fonttools uharfbuzz
/tmp/ft/bin/python -I outline.py Fredoka.ttf Outfit.ttf
```

The text settings (size, weight, letter spacing) are at the top of `outline.py`.
Credits and the font license are in `brand/FONTS.md`.
