#!/usr/bin/env python3
"""Оформляет сырые скриншоты в кадры для витрин App Store и Google Play.

Сырой снимок экрана продаёт плохо: человек в поиске видит миниатюру шириной
в палец и должен за секунду понять, о чём приложение. Поэтому каждый кадр —
это заголовок, три коротких обещания и устройство под ними, срезанное нижней
кромкой: так видно и мысль, и живой интерфейс.

Рамка устройства рисуется CSS, а не берётся картинкой: PNG-мокапы весят,
устаревают вместе с моделями телефонов и тянут за собой лицензию.

    python3 marketing/frame-shots.py            # все языки
    python3 marketing/frame-shots.py ru         # только один

Вход:  metadata/screenshots/<lang>/NN-name.png  (1320×2868, снимает StoreShotsUITests)
Выход: metadata/screenshots-framed/<lang>/NN-name.png (1320×2868)
"""
import base64
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent
CAPTIONS = json.loads((ROOT / "marketing" / "captions.json").read_text())

# Два профиля, потому что у витрин разные требования к картинке.
#
# ios  — 1320×2868, размер App Store для 6.9".
# play — 1080×1920: Google Play не принимает кадр, у которого длинная сторона
#        больше удвоенной короткой, а сырой снимок Pixel (1280×2856) — это
#        1:2.23. Плюс исходники здесь андроидные: показывать iOS-интерфейс в
#        Play нечестно.
PROFILES = {
    "ios": {
        "raw": ROOT / "metadata" / "screenshots",
        "out": ROOT / "metadata" / "screenshots-framed",
        "W": 1320, "H": 2868,
        "pad": "132px 96px 0", "eyebrow": 40, "title": 104, "chip_gap": 40,
        "chip_ico": 118, "chip_font": 34, "chips_top": 76, "chip_w": 300,
        "device_w": 1060, "device_h": int(1060 * 2082 / 1022), "device_top": 800,
        "radius_out": 86, "radius_in": 72,
        "device_style": "ios", "island_w": 250, "island_h": 74, "island_top": 34,
    },
    "play": {
        "raw": ROOT / "metadata" / "screenshots-android",
        "out": ROOT / "metadata" / "screenshots-framed-android",
        "W": 1080, "H": 1920,
        "pad": "88px 76px 0", "eyebrow": 30, "title": 72, "chip_gap": 26,
        "chip_ico": 88, "chip_font": 25, "chips_top": 46, "chip_w": 240,
        "device_w": 820, "device_h": 1560, "device_top": 520,
        "radius_out": 64, "radius_in": 54,
        "device_style": "android", "island_w": 30, "island_h": 30, "island_top": 22,
    },
}

CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

# Готовый мокап iPhone с обмеренным экраном. Рисовать корпус CSS не вышло:
# скруглениями и градиентом получается «просто телефон», а не айфон —
# силуэт задают пропорции корпуса, кант и расположение кнопок.
MOCKUP = ROOT / "marketing" / "iphone-mockup.png"
MOCKUP_W, MOCKUP_H = 1022, 2082
SCREEN_L, SCREEN_T, SCREEN_W, SCREEN_H = 52, 46, 918, 1990
SCREEN_R = 126  # радиус скругления экрана в пикселях мокапа

TEMPLATE = """<!doctype html>
<meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { width: %(W)dpx; height: %(H)dpx; overflow: hidden; }
  body {
    background:
      radial-gradient(120%% 60%% at 78%% -8%%, #35D89B 0%%, rgba(53,216,155,0) 62%%),
      linear-gradient(168deg, #0B8F63 0%%, #046B4C 46%%, #023B2C 100%%);
    font-family: -apple-system, BlinkMacSystemFont, "SF Pro Display", "Helvetica Neue", Arial, sans-serif;
    color: #fff;
    position: relative;
  }
  .pad { padding: %(PAD)s; text-align: center; }
  .eyebrow {
    font-size: %(EYEBROW_SIZE)dpx; font-weight: 800; letter-spacing: .18em;
    color: rgba(255,255,255,.62); margin-bottom: 26px;
  }
  h1 {
    font-size: %(TITLE_SIZE)dpx; line-height: 1.04; font-weight: 800; letter-spacing: -.028em;
    white-space: pre-line; text-wrap: balance;
  }
  /* Чипы по центру и в цвет: белые кружки сливались с текстом в один
     серо-белый ряд, а прижатые влево спорили с центрованным заголовком. */
  .chips {
    display: flex; gap: %(CHIP_GAP)dpx; margin-top: %(CHIPS_TOP)dpx;
    justify-content: center; align-items: flex-start;
  }
  .chip { display: flex; flex-direction: column; align-items: center; gap: 14px; width: %(CHIP_W)dpx; }
  .chip .ico {
    width: %(CHIP_ICO)dpx; height: %(CHIP_ICO)dpx; border-radius: 50%%;
    display: flex; align-items: center; justify-content: center;
    font-size: %(CHIP_ICO_FONT)dpx; font-weight: 700; color: #fff;
    box-shadow: 0 10px 26px rgba(0,0,0,.28);
  }
  .chip .txt { font-size: %(CHIP_FONT)dpx; font-weight: 700; line-height: 1.18; }

  /* Устройство срезано нижней кромкой: кадр показывает, что интерфейс
     продолжается, и не тратит высоту на пустую рамку. */
  .device {
    position: absolute; left: 50%%; transform: translateX(-50%%);
    top: %(DEVICE_TOP)dpx; width: %(DEVICE_W)dpx; height: %(DEVICE_H)dpx;
    filter: drop-shadow(0 44px 80px rgba(0,0,0,.42));
  }
  /* iPhone — корпус картинкой, снимок кладётся поверх чёрного экрана
     по обмеренным координатам мокапа. */
  .device .bezel { width: 100%%; height: 100%%; display: block; }
  .device.ios .screen {
    position: absolute;
    left: %(SCREEN_L)f%%; top: %(SCREEN_T)f%%;
    width: %(SCREEN_W)f%%; height: %(SCREEN_H)f%%;
    border-radius: %(SCREEN_RX)f%% / %(SCREEN_RY)f%%;
    overflow: hidden; background: #F3F4F7;
  }
  /* Android — своя плашка: у него другой силуэт, и подсовывать айфон нечестно. */
  .device.android {
    background: #1b1b1d; border-radius: %(RADIUS_OUT)dpx; padding: 16px;
  }
  .device.android .screen {
    width: 100%%; height: 100%%; border-radius: %(RADIUS_IN)dpx;
    overflow: hidden; background: #F3F4F7; position: relative;
  }
  .device.android .hole {
    position: absolute; left: 50%%; transform: translateX(-50%%);
    top: %(ISLAND_TOP)dpx; width: %(ISLAND_W)dpx; height: %(ISLAND_H)dpx;
    background: #000; border-radius: 50%%;
  }
  .screen img { width: 100%%; display: block; }
</style>
<div class="pad">
  <div class="eyebrow">%(EYEBROW)s</div>
  <h1>%(TITLE)s</h1>
  <div class="chips">%(CHIPS)s</div>
</div>
<div class="device %(DEVICE_STYLE)s">%(BEZEL)s
  <div class="screen"><img src="%(SHOT)s">%(HOLE)s</div>
</div>
"""

CHIP = ('<div class="chip"><div class="ico" style="background:%(color)s">%(icon)s</div>'
        '<div class="txt">%(line1)s<br>%(line2)s</div></div>')

# Свой цвет каждому чипу — из палитры приложения: зелёный акцент, синий
# Telegram, янтарный из аватарок.
CHIP_COLORS = ["#00A870", "#29A9EB", "#F2A93B"]




def render(html: str, out: pathlib.Path, W: int, H: int) -> None:
    """Снимает страницу headless-хромом.

    Ждём появления ФАЙЛА, а не выхода процесса: headless Chrome записывает
    скриншот и остаётся висеть, так что subprocess.run упирается в таймаут,
    хотя картинка давно готова.
    """
    out.unlink(missing_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        page = pathlib.Path(tmp) / "shot.html"
        page.write_text(html)
        profile = pathlib.Path(tmp) / "profile"
        proc = subprocess.Popen(
            [CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars",
             f"--user-data-dir={profile}", "--force-device-scale-factor=1",
             f"--window-size={W},{H}", f"--screenshot={out}", f"file://{page}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        try:
            deadline = time.time() + 90
            size = -1
            while time.time() < deadline:
                if out.exists():
                    # Ещё и стабилизация размера: файл появляется до того,
                    # как в него дописан последний блок.
                    current = out.stat().st_size
                    if current > 0 and current == size:
                        return
                    size = current
                time.sleep(0.4)
            raise RuntimeError(f"хром не отдал кадр за 90 с: {out.name}")
        finally:
            proc.kill()
            proc.wait(timeout=10)


def frame(lang: str, profile: str) -> None:
    cfg = PROFILES[profile]
    src_dir = cfg["raw"] / lang
    if not src_dir.is_dir():
        sys.exit(f"нет сырых кадров: {src_dir}")
    spec = CAPTIONS[lang]
    dst_dir = cfg["out"] / lang
    if dst_dir.exists():
        shutil.rmtree(dst_dir)
    dst_dir.mkdir(parents=True)

    bezel_tag = ""
    if cfg["device_style"] == "ios":
        raw = base64.b64encode(MOCKUP.read_bytes()).decode()
        bezel_tag = f'<img class="bezel" src="data:image/png;base64,{raw}">'

    chips = "".join(
        CHIP % dict(c, color=CHIP_COLORS[i % len(CHIP_COLORS)])
        for i, c in enumerate(spec["chips"])
    )
    # Кадр ищется по ИМЕНИ ЭКРАНА, а не по имени файла: у iOS и Android разные
    # наборы (в Android-съёмке нет «Записать платёж» и голосового композера) и
    # своя нумерация. Отсутствие кадра — не ошибка, просто его нет.
    for order, shot in enumerate(spec["shots"], start=1):
        matches = sorted(src_dir.glob(f"*-{shot['screen']}.png"))
        if not matches:
            print(f"  {shot['screen']}: нет кадра — пропускаю")
            continue
        src = matches[0]
        data = base64.b64encode(src.read_bytes()).decode()
        html = TEMPLATE % {
            "W": cfg["W"], "H": cfg["H"], "PAD": cfg["pad"],
            "EYEBROW_SIZE": cfg["eyebrow"], "TITLE_SIZE": cfg["title"],
            "CHIP_GAP": cfg["chip_gap"], "CHIPS_TOP": cfg["chips_top"],
            "CHIP_ICO": cfg["chip_ico"], "CHIP_ICO_FONT": int(cfg["chip_ico"] * 0.5),
            "CHIP_FONT": cfg["chip_font"], "CHIP_W": cfg["chip_w"],
            "DEVICE_STYLE": cfg["device_style"],
            "BEZEL": bezel_tag if cfg["device_style"] == "ios" else "",
            "HOLE": "" if cfg["device_style"] == "ios" else '<div class="hole"></div>',
            "SCREEN_L": SCREEN_L / MOCKUP_W * 100, "SCREEN_T": SCREEN_T / MOCKUP_H * 100,
            "SCREEN_W": SCREEN_W / MOCKUP_W * 100, "SCREEN_H": SCREEN_H / MOCKUP_H * 100,
            "SCREEN_RX": SCREEN_R / SCREEN_W * 100, "SCREEN_RY": SCREEN_R / SCREEN_H * 100,
            "ISLAND_W": cfg["island_w"], "ISLAND_H": cfg["island_h"],
            "ISLAND_TOP": cfg["island_top"],
            "RADIUS_OUT": cfg["radius_out"], "RADIUS_IN": cfg["radius_in"],
            # Ширина устройства и отступ сверху подобраны так, чтобы под
            # текстом осталась ровно верхняя половина экрана телефона.
            "DEVICE_W": cfg["device_w"], "DEVICE_H": cfg["device_h"],
            "DEVICE_TOP": cfg["device_top"],
            "EYEBROW": shot["eyebrow"], "TITLE": shot["title"],
            "CHIPS": chips, "SHOT": f"data:image/png;base64,{data}",
        }
        out = dst_dir / f"{order:02d}-{shot['screen']}.png"
        render(html, out, cfg["W"], cfg["H"])
        print(f"  {out.relative_to(ROOT)}")


if __name__ == "__main__":
    args = sys.argv[1:]
    profile = "ios"
    if args and args[0] in PROFILES:
        profile = args.pop(0)
    langs = args or sorted(CAPTIONS)
    for lang in langs:
        if lang not in CAPTIONS:
            sys.exit(f"нет подписей для «{lang}»")
        print(f"[{profile}/{lang}]")
        frame(lang, profile)
