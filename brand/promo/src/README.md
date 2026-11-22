# Promo image sources

The promotional images in `brand/promo/` (except the `panel-*.png` screenshots and
`title-mc-style.png`) are HTML pages rendered to PNG with headless Chromium.

| File | What it is |
|---|---|
| `gen.mjs` | Generator: texts (EN/ES), pixel icons, styles and layout. Edit this, not the HTML. |
| `<name>.html`, `<name>-es.html` | Generated pages, one per image (committed so they can be opened directly). |
| `list.txt` | `name width height scale` for each page (generated). |
| `shots/` | Panel screenshots used inside `hero` and `social-preview`. |
| `render.sh` | Renders every page to `../<name>.png` at its exact size. |

## Render

Needs Node and Chromium (set `CHROME=/path/to/chrome` if it is not `chromium`).

```sh
cd brand/promo/src
node gen.mjs          # writes the .html pages and list.txt
./render.sh           # all images
./render.sh hero      # only the ones whose name contains "hero"
```

Each page is a CSS-pixel layout (1280 wide) captured with
`--force-device-scale-factor` 1 or 2, so `hero`, `features`, `install` and
`runs-on` come out at 2560 px wide, and `social-preview` and `banner-mc` at 1280.
The equivalent single command is:

```sh
chromium --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 \
  --window-size=1280,600 --virtual-time-budget=3000 --screenshot=../hero.png "file://$PWD/hero.html"
```

## Fonts

Only system fonts, through the same stacks as the panel: the condensed headings
fall back to *Nimbus Sans Narrow*, body text to the system sans (*Liberation Sans*
on Linux) and code to *JetBrains Mono*. The images in the repo were rendered on
Linux with those three; another system will pick its own fonts from the stacks
and the result will differ slightly. No Minecraft fonts, textures or logos are used.

The Cubit logo is not text: the badge comes from `brand/symbol.svg` and the name
from the outlines in `brand/src/wordmark.json` (Fredoka, see `brand/FONTS.md`), so
it looks the same everywhere. Regenerate those first (`brand/src/`) if the logo changes.
