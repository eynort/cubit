#!/usr/bin/env python3
"""outline.py: pasa el nombre y el lema del logotipo a trazados (wordmark.json).

Así los SVG no dependen de que el sistema tenga las fuentes. Solo hace falta
volver a ejecutarlo si cambia el texto, la fuente, el peso o el espaciado.

    python3 -I outline.py Fredoka.ttf Outfit.ttf

Las fuentes son las variables de github.com/google/fonts (ofl/fredoka y
ofl/outfit, licencia OFL); no se guardan en el repositorio. Necesita fonttools
y uharfbuzz (pip install fonttools uharfbuzz).
"""
import io
import json
import os
import sys

import uharfbuzz as hb
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.pens.boundsPen import BoundsPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

# texto, fuente, ejes, tamaño en px y espaciado entre letras en px (como CSS)
ITEMS = {
    "name": dict(text="Cubit", font=0, axes={"wght": 600, "wdth": 100}, size=214, ls=-2),
    "tag": dict(text="MINECRAFT BOTS", font=1, axes={"wght": 500}, size=30, ls=9),
}


def instance(path, axes):
    f = TTFont(path)
    axes = {k: v for k, v in axes.items() if k in [a.axisTag for a in f["fvar"].axes]}
    f = instantiateVariableFont(f, axes, inplace=False)
    buf = io.BytesIO()
    f.save(buf)
    return TTFont(io.BytesIO(buf.getvalue())), buf.getvalue()


def shape(data, text):
    face = hb.Face(data)
    font = hb.Font(face)
    b = hb.Buffer()
    b.add_str(text)
    b.guess_segment_properties()
    hb.shape(font, b, {"kern": True, "liga": False})
    return b.glyph_infos, b.glyph_positions


def outline(path, it):
    f, data = instance(path, it["axes"])
    upm = f["head"].unitsPerEm
    k = it["size"] / upm
    gs = f.getGlyphSet()
    order = f.getGlyphOrder()
    infos, pos = shape(data, it["text"])
    pen = SVGPathPen(gs, lambda v: f"{round(v, 2):g}")
    bpen = BoundsPen(gs)
    x = 0.0
    n = len(infos)
    for i, (gi, gp) in enumerate(zip(infos, pos)):
        name = order[gi.codepoint]
        # escala, invierte y (SVG crece hacia abajo) y coloca la letra
        t = (k, 0, 0, -k, x + gp.x_offset * k, -gp.y_offset * k)
        gs[name].draw(TransformPen(pen, t))
        gs[name].draw(TransformPen(bpen, t))
        x += gp.x_advance * k
        if i < n - 1:
            x += it["ls"]
    os2 = f["OS/2"]
    return {
        "text": it["text"],
        "size": it["size"],
        "ls": it["ls"],
        "d": pen.getCommands(),
        "advance": round(x, 2),
        "bbox": [round(v, 2) for v in bpen.bounds],  # x0 y0 x1 y1, y hacia abajo
        "capHeight": round(os2.sCapHeight * k, 2),
        "xHeight": round(os2.sxHeight * k, 2),
    }


def main():
    if len(sys.argv) != 3:
        sys.exit("uso: outline.py Fredoka.ttf Outfit.ttf")
    fonts = sys.argv[1:]
    out = {key: outline(fonts[it["font"]], it) for key, it in ITEMS.items()}
    dst = os.path.join(os.path.dirname(os.path.abspath(__file__)), "wordmark.json")
    with open(dst, "w") as fh:
        json.dump(out, fh, indent=1)
        fh.write("\n")
    for key, v in out.items():
        print(key, v["text"], "advance", v["advance"], "bbox", v["bbox"])


if __name__ == "__main__":
    main()
