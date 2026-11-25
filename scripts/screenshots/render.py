#!/usr/bin/env python3
"""Render ANSI terminal frames (*.ans) to PNG screenshots in a macOS-style
terminal window. Usage: render.py <frames-dir> <out-dir> [--title TITLE]

Only needs Pillow. Handles 24-bit / 256 / 16-color SGR, bold, faint,
italic, underline and reverse; block elements are drawn as rectangles so
the LTS banner has no seams."""
import os, re, sys, unicodedata
from PIL import Image, ImageDraw, ImageFont, ImageFilter

SCALE = 2                     # retina-style output
FONT_PX = 14 * SCALE
LINE_H = int(FONT_PX * 1.32)
PAD = 18 * SCALE              # terminal inner padding
TITLE_H = 30 * SCALE
RADIUS = 12 * SCALE
SHADOW = 34 * SCALE
DEFAULT_FG = (230, 230, 230)
DEFAULT_BG = (0, 0, 0)
MENLO = "/System/Library/Fonts/Menlo.ttc"
fonts = {k: ImageFont.truetype(MENLO, FONT_PX, index=i) for k, i in
         {"r": 0, "b": 1, "i": 2, "bi": 3}.items()}
CELL_W = fonts["r"].getlength("M")

BASIC = [(0, 0, 0), (205, 49, 49), (13, 188, 121), (229, 229, 16), (36, 114, 200),
         (188, 63, 188), (17, 168, 205), (229, 229, 229), (102, 102, 102), (241, 76, 76),
         (35, 209, 139), (245, 245, 67), (59, 142, 234), (214, 112, 214), (41, 184, 219),
         (255, 255, 255)]

def xterm256(n):
    if n < 16:
        return BASIC[n]
    if n < 232:
        n -= 16
        lv = [0, 95, 135, 175, 215, 255]
        return (lv[n // 36], lv[(n // 6) % 6], lv[n % 6])
    v = 8 + (n - 232) * 10
    return (v, v, v)

SGR = re.compile(r"\x1b\[([0-9;]*)m")
OTHER = re.compile(r"\x1b\[[0-9;?]*[A-Za-ln-z]|\x1b\][^\x07]*\x07")

def parse(frame):
    """→ list of rows; each row a list of (char, style) cells."""
    rows = []
    st = {"fg": None, "bg": None, "b": False, "f": False, "i": False, "u": False, "rev": False}
    for line in frame.split("\n"):
        line = OTHER.sub("", line)
        cells, pos = [], 0
        for m in SGR.finditer(line + "\x1b[m"):
            for ch in line[pos:m.start()]:
                w = 2 if unicodedata.east_asian_width(ch) in "WF" else 1
                cells.append((ch, dict(st)))
                if w == 2:
                    cells.append(("", dict(st)))
            pos = m.end()
            params = [int(p) if p else 0 for p in m.group(1).split(";")] if m.group(1) else [0]
            i = 0
            while i < len(params):
                p = params[i]
                if p == 0:
                    st.update(fg=None, bg=None, b=False, f=False, i=False, u=False, rev=False)
                elif p == 1: st["b"] = True
                elif p == 2: st["f"] = True
                elif p == 3: st["i"] = True
                elif p == 4: st["u"] = True
                elif p == 7: st["rev"] = True
                elif p == 22: st["b"] = st["f"] = False
                elif p == 23: st["i"] = False
                elif p == 24: st["u"] = False
                elif p == 27: st["rev"] = False
                elif p in (38, 48) and i + 1 < len(params):
                    key = "fg" if p == 38 else "bg"
                    if params[i + 1] == 2 and i + 4 < len(params):
                        st[key] = tuple(params[i + 2:i + 5]); i += 4
                    elif params[i + 1] == 5 and i + 2 < len(params):
                        st[key] = xterm256(params[i + 2]); i += 2
                elif p == 39: st["fg"] = None
                elif p == 49: st["bg"] = None
                elif 30 <= p <= 37: st["fg"] = BASIC[p - 30]
                elif 90 <= p <= 97: st["fg"] = BASIC[p - 90 + 8]
                elif 40 <= p <= 47: st["bg"] = BASIC[p - 40]
                elif 100 <= p <= 107: st["bg"] = BASIC[p - 100 + 8]
                i += 1
        rows.append(cells)
    while rows and not any(c.strip() for c, _ in rows[-1]):
        rows.pop()
    return rows

def faint(c, bg):
    return tuple(int(a * 0.6 + b * 0.4) for a, b in zip(c, bg))

# Box drawing: (up, down, left, right) arm weights, 1 = light, 2 = double.
# Drawn as lines so borders are seamless whatever the font supports.
BOX = {
    "─": (0, 0, 1, 1), "│": (1, 1, 0, 0), "┌": (0, 1, 0, 1), "┐": (0, 1, 1, 0),
    "└": (1, 0, 0, 1), "┘": (1, 0, 1, 0), "├": (1, 1, 0, 1), "┤": (1, 1, 1, 0),
    "┬": (0, 1, 1, 1), "┴": (1, 0, 1, 1), "┼": (1, 1, 1, 1),
    "╭": (0, 1, 0, 1), "╮": (0, 1, 1, 0), "╰": (1, 0, 0, 1), "╯": (1, 0, 1, 0),
    "═": (0, 0, 2, 2), "║": (2, 2, 0, 0), "╔": (0, 2, 0, 2), "╗": (0, 2, 2, 0),
    "╚": (2, 0, 0, 2), "╝": (2, 0, 2, 0), "╠": (2, 2, 0, 2), "╣": (2, 2, 2, 0),
    "╦": (0, 2, 2, 2), "╩": (2, 0, 2, 2), "╬": (2, 2, 2, 2),
}

def draw_box(d, ch, x0, x1, y0, y1, color):
    """Draw a box-drawing char in the integer cell [x0,x1)×[y0,y1)."""
    up, down, left, right = BOX[ch]
    lw = max(2, round(1.2 * SCALE))
    cx, cy = (x0 + x1) // 2, (y0 + y1) // 2
    g = round(2.5 * SCALE)
    def h(y, xa, xb):
        d.rectangle([min(xa, xb), y - lw // 2, max(xa, xb), y - lw // 2 + lw - 1], fill=color)
    def v(x, ya, yb):
        d.rectangle([x - lw // 2, min(ya, yb), x - lw // 2 + lw - 1, max(ya, yb)], fill=color)
    if 2 not in (up, down, left, right):
        if left:  h(cy, x0, cx)
        if right: h(cy, cx, x1)
        if up:    v(cx, y0, cy)
        if down:  v(cx, cy, y1)
        return
    if (left, right) == (2, 2) and not (up or down):
        h(cy - g, x0, x1); h(cy + g, x0, x1); return
    if (up, down) == (2, 2) and not (left or right):
        v(cx - g, y0, y1); v(cx + g, y0, y1); return
    # double corners/tees: draw each quadrant pair as outer + inner rails
    sx = 1 if right else -1
    sy = 1 if down else -1
    edge_x = x1 if sx > 0 else x0
    edge_y = y1 if sy > 0 else y0
    for k in (-1, 1):  # outer (k=-1) and inner (k=+1) rail
        px, py = cx + k * sx * g, cy + k * sy * g
        h(py, px, edge_x)
        v(px, py, edge_y)

BLOCKS = {"█": (0, 0, 1, 1), "▀": (0, 0, 1, .5), "▄": (0, .5, 1, 1), "▌": (0, 0, .5, 1), "▐": (.5, 0, 1, 1)}

def dominant_bg(rows):
    counts = {}
    for cells in rows:
        for _, st in cells:
            bg = st["fg"] if st["rev"] else st["bg"]
            if bg:
                counts[bg] = counts.get(bg, 0) + 1
    return max(counts, key=counts.get) if counts else DEFAULT_BG

def render(rows, cols, title):
    global DEFAULT_BG
    DEFAULT_BG = dominant_bg(rows)  # fill unpainted cells like the frame's own background
    w = int(cols * CELL_W) + 2 * PAD
    h = len(rows) * LINE_H + 2 * PAD + TITLE_H
    term = Image.new("RGB", (w, h), DEFAULT_BG)
    d = ImageDraw.Draw(term)
    # title bar
    d.rectangle([0, 0, w, TITLE_H], fill=(28, 28, 30))
    for k, col in enumerate([(255, 95, 86), (255, 189, 46), (39, 201, 63)]):
        cx, cy, r = PAD + k * 20 * SCALE, TITLE_H // 2, 6 * SCALE
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=col)
    tf = ImageFont.truetype(MENLO, 12 * SCALE)
    tw = d.textlength(title, font=tf)
    d.text(((w - tw) / 2, TITLE_H / 2), title, font=tf, fill=(150, 150, 155), anchor="lm")
    y0 = TITLE_H + PAD
    for r, cells in enumerate(rows):
        y = y0 + r * LINE_H
        for c, (ch, st) in enumerate(cells[:cols]):
            x = round(PAD + c * CELL_W)
            xn = round(PAD + (c + 1) * CELL_W)
            fg, bg = st["fg"] or DEFAULT_FG, st["bg"] or DEFAULT_BG
            if st["rev"]:
                fg, bg = bg, fg
            if bg != DEFAULT_BG:
                d.rectangle([x, y, xn, y + LINE_H], fill=bg)
            if not ch or ch == " ":
                continue
            if st["f"]:
                fg = faint(fg, bg)
            if ch in BOX:
                draw_box(d, ch, x, xn, y, y + LINE_H, fg)
                continue
            if ch in BLOCKS:
                a, b, cc, dd = BLOCKS[ch]
                d.rectangle([x + round(a * (xn - x)), y + round(b * LINE_H), x + round(cc * (xn - x)), y + round(dd * LINE_H)], fill=fg)
                continue
            f = fonts[("b" if st["b"] else "") + ("i" if st["i"] else "") or "r"]
            d.text((x, y + LINE_H / 2), ch, font=f, fill=fg, anchor="lm")
            if st["u"]:
                d.line([x, y + LINE_H - 3 * SCALE, x + CELL_W, y + LINE_H - 3 * SCALE], fill=fg, width=SCALE)
    # rounded corners + shadow on a transparent canvas
    mask = Image.new("L", (w, h), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, w - 1, h - 1], RADIUS, fill=255)
    out = Image.new("RGBA", (w + 2 * SHADOW, h + 2 * SHADOW), (0, 0, 0, 0))
    sh = Image.new("RGBA", out.size, (0, 0, 0, 0))
    ImageDraw.Draw(sh).rounded_rectangle([SHADOW, SHADOW + 8 * SCALE, SHADOW + w, SHADOW + h + 8 * SCALE], RADIUS, fill=(0, 0, 0, 110))
    out = Image.alpha_composite(out, sh.filter(ImageFilter.GaussianBlur(14 * SCALE)))
    out.paste(term, (SHADOW, SHADOW), mask)
    ImageDraw.Draw(out).rounded_rectangle([SHADOW, SHADOW, SHADOW + w - 1, SHADOW + h - 1], RADIUS, outline=(70, 70, 75), width=SCALE)
    return out

def main():
    src, dst = sys.argv[1], sys.argv[2]
    title = sys.argv[sys.argv.index("--title") + 1] if "--title" in sys.argv else "lts"
    os.makedirs(dst, exist_ok=True)
    for name in sorted(os.listdir(src)):
        if not name.endswith(".ans"):
            continue
        rows = parse(open(os.path.join(src, name), encoding="utf-8").read())
        cols = max((len(r) for r in rows), default=80)
        if "--cols" in sys.argv:
            cols = min(cols, int(sys.argv[sys.argv.index("--cols") + 1]))
        img = render(rows, cols, title)
        path = os.path.join(dst, name[:-4] + ".png")
        img.save(path, optimize=True)
        print(path, img.size)

if __name__ == "__main__":
    main()
