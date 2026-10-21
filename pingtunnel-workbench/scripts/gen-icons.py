"""Draws the app icon and the tray icon from one waveform mark.

Run from the project root: python scripts/gen-icons.py
"""

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
RES = ROOT / "resources"

# The mark in a 20x20 box, the same polyline the page draws in its title bar.
MARK = [
    (1.5, 10.0),
    (4.5, 10.0),
    (6.5, 4.5),
    (9.5, 15.5),
    (12.0, 7.5),
    (14.0, 12.0),
    (15.5, 10.0),
    (18.5, 10.0),
]


def scaled(points, size, pad_ratio=0.0):
    span = 20.0
    usable = size * (1 - 2 * pad_ratio)
    k = usable / span
    off = size * pad_ratio
    return [(off + x * k, off + y * k) for x, y in points]


def draw_mark(draw, points, width, color):
    draw.line(points, fill=color, width=int(round(width)), joint="curve")
    # Round caps: Pillow's line has butt ends, so cap them by hand.
    r = width / 2
    for x, y in (points[0], points[-1]):
        draw.ellipse([x - r, y - r, x + r, y + r], fill=color)


def tray_icon(size):
    scale = 4  # draw big, then downsample for smooth edges
    img = Image.new("RGBA", (size * scale, size * scale), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    draw_mark(draw, scaled(MARK, size * scale, 0.10), 1.9 * scale, (0, 0, 0, 255))
    return img.resize((size, size), Image.LANCZOS)


def app_icon(size=1024):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    # macOS-style squircle-ish rounded square, dark graphite.
    radius = int(size * 0.225)
    draw.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=(18, 21, 24, 255))
    # A hairline of light along the top edge, so the tile reads as a surface.
    draw.rounded_rectangle(
        [1, 1, size - 2, size - 2], radius=radius, outline=(46, 52, 58, 255), width=max(2, size // 340)
    )
    draw_mark(draw, scaled(MARK, size, 0.255), size * 0.034, (63, 220, 154, 255))
    return img


def main():
    RES.mkdir(parents=True, exist_ok=True)
    tray_icon(32).save(RES / "tray.png")
    app_icon(1024).save(RES / "icon.png")
    print("wrote", RES / "tray.png", RES / "icon.png")


if __name__ == "__main__":
    main()
