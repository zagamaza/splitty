#!/usr/bin/env python3
"""Рисует feature graphic 1024×500 для Google Play — по одному на язык.

Play без него не даёт опубликовать листинг, и он же показывается шапкой
карточки. Формат жёсткий: 1024×500, без прозрачности, и важное нельзя
класть по краям — на витрине графику подрезают.

    python3 marketing/feature-graphic.py

Выход: metadata/feature-graphic/<lang>.png
"""
import base64
import json
import pathlib
import subprocess
import sys
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "metadata" / "feature-graphic"
ICON = ROOT / "ios" / "Splitty" / "Assets.xcassets" / "AppIcon.appiconset" / "icon-1024.png"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
W, H = 1024, 500

TEXT = {
    "en-US": ("Split expenses with friends", "Who owes whom — sorted"),
    "ru":    ("Делите расходы с друзьями",   "Кто кому должен — сразу видно"),
    "es-ES": ("Divide gastos con amigos",    "Quién debe a quién, claro"),
    "de-DE": ("Kosten mit Freunden teilen",  "Wer wem was schuldet"),
    "fr-FR": ("Partagez vos frais",          "Qui doit quoi, tout de suite"),
}

TEMPLATE = """<!doctype html>
<meta charset="utf-8">
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html, body { width:%(W)dpx; height:%(H)dpx; overflow:hidden; }
  body {
    background:
      radial-gradient(90%% 120%% at 88%% 8%%, #35D89B 0%%, rgba(53,216,155,0) 60%%),
      linear-gradient(120deg, #0B8F63 0%%, #046B4C 52%%, #023B2C 100%%);
    font-family: -apple-system, BlinkMacSystemFont, "SF Pro Display", "Helvetica Neue", Arial, sans-serif;
    color:#fff; display:flex; align-items:center; gap:52px;
    /* Поля с запасом: витрина Play подрезает графику по краям. */
    padding: 0 92px;
  }
  .icon { width:150px; height:150px; border-radius:34px; flex:0 0 150px;
          box-shadow:0 18px 40px rgba(0,0,0,.32); }
  h1 { font-size:56px; font-weight:800; letter-spacing:-.02em; line-height:1.1; }
  p  { font-size:29px; font-weight:600; color:rgba(255,255,255,.78); margin-top:14px; }
</style>
<img class="icon" src="%(ICON)s">
<div><h1>%(TITLE)s</h1><p>%(SUB)s</p></div>
"""


def render(html: str, out: pathlib.Path) -> None:
    out.unlink(missing_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        page = pathlib.Path(tmp) / "fg.html"
        page.write_text(html)
        proc = subprocess.Popen(
            [CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars",
             f"--user-data-dir={pathlib.Path(tmp)/'p'}", "--force-device-scale-factor=1",
             f"--window-size={W},{H}", f"--screenshot={out}", f"file://{page}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            # Ждём файл, а не выход процесса: headless Chrome не завершается.
            deadline, size = time.time() + 90, -1
            while time.time() < deadline:
                if out.exists():
                    cur = out.stat().st_size
                    if cur > 0 and cur == size:
                        return
                    size = cur
                time.sleep(0.4)
            raise RuntimeError(f"хром не отдал {out.name}")
        finally:
            proc.kill(); proc.wait(timeout=10)


if __name__ == "__main__":
    if not ICON.exists():
        sys.exit(f"нет иконки: {ICON}")
    OUT.mkdir(parents=True, exist_ok=True)
    icon = "data:image/png;base64," + base64.b64encode(ICON.read_bytes()).decode()
    for lang, (title, sub) in TEXT.items():
        dst = OUT / f"{lang}.png"
        render(TEMPLATE % {"W": W, "H": H, "ICON": icon, "TITLE": title, "SUB": sub}, dst)
        print(f"  {dst.relative_to(ROOT)}")
