#!/usr/bin/env python3
"""ico.py salida.ico a.png b.png ...: junta varios PNG cuadrados en un .ico.

Cada tamaño va guardado como PNG dentro del .ico (Windows lo admite desde
Vista), así el de 256 ocupa decenas de KB y no cientos como en BMP.
"""
import struct
import sys


def size(png):
    if png[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit("no es un PNG")
    return struct.unpack(">II", png[16:24])


def main():
    out, files = sys.argv[1], sys.argv[2:]
    pngs = [open(f, "rb").read() for f in files]
    head = struct.pack("<HHH", 0, 1, len(pngs))
    off = 6 + 16 * len(pngs)
    dirs, data = b"", b""
    for p in pngs:
        w, h = size(p)
        dirs += struct.pack("<BBBBHHII", w % 256, h % 256, 0, 0, 1, 32, len(p), off + len(data))
        data += p
    with open(out, "wb") as fh:
        fh.write(head + dirs + data)


if __name__ == "__main__":
    main()
