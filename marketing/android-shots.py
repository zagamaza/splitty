#!/usr/bin/env python3
"""Снимает экраны Android-приложения для витрины Google Play.

Тапы идут не по угаданным координатам, а по РЕАЛЬНЫМ границам элементов:
`uiautomator dump` отдаёт дерево с bounds, отсюда и берётся центр нужной
кнопки. Иначе любая правка вёрстки молча ломает съёмку и даёт кадры
не тех экранов.

Требует: поднятый эмулятор, установленный debug-APK, локальный бэкенд с
демо-данными (scripts/seed-store-shots.py).

    python3 marketing/android-shots.py ru
"""
import pathlib
import re
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

ROOT = pathlib.Path(__file__).resolve().parent.parent
ADB = str(pathlib.Path.home() / "Library/Android/sdk/platform-tools/adb")
PKG = "com.zagir.splitty"

LABELS = {
    "ru": {"locale": "ru", "email": "shots-ru@splitty.test", "room": "Поездка в Стамбул",
           "groups": "Группы", "friends": "Друзья", "totals": "Итоги",
           "balances": "Балансы", "disclosure": "войдите по email"},
    "en": {"locale": "en", "email": "shots-en@splitty.test", "room": "Trip to Lisbon",
           "groups": "Groups", "friends": "Friends", "totals": "Totals",
           "balances": "Balances", "disclosure": "sign in with email"},
}
PASSWORD = "20260806"


def sh(*args, **kw):
    return subprocess.run([ADB, *args], capture_output=True, text=True, timeout=120, **kw)


def dump() -> ET.Element:
    """Дерево экрана. Пара попыток: дамп падает, пока идёт анимация."""
    for _ in range(6):
        sh("shell", "rm", "-f", "/sdcard/ui.xml")
        sh("shell", "uiautomator", "dump", "/sdcard/ui.xml")
        raw = sh("shell", "cat", "/sdcard/ui.xml").stdout
        if raw.strip().startswith("<?xml"):
            try:
                return ET.fromstring(raw)
            except ET.ParseError:
                pass
        time.sleep(1.0)
    raise RuntimeError("uiautomator не отдал дерево экрана")


def center(node) -> tuple[int, int]:
    x1, y1, x2, y2 = map(int, re.findall(r"\d+", node.get("bounds")))
    return (x1 + x2) // 2, (y1 + y2) // 2


def find(text: str, exact: bool = False):
    for node in dump().iter("node"):
        label = node.get("text") or ""
        desc = node.get("content-desc") or ""
        hit = (label == text or desc == text) if exact else (text in label or text in desc)
        if hit and node.get("bounds"):
            return node
    return None


def tap_text(text: str, exact: bool = False, required: bool = True) -> bool:
    node = find(text, exact)
    if node is None:
        if required:
            raise RuntimeError(f"на экране нет «{text}»")
        return False
    x, y = center(node)
    sh("shell", "input", "tap", str(x), str(y))
    time.sleep(1.2)
    return True


def shoot(dst: pathlib.Path, name: str, index: list):
    index[0] += 1
    out = dst / f"{index[0]:02d}-{name}.png"
    with out.open("wb") as f:
        subprocess.run([ADB, "exec-out", "screencap", "-p"], stdout=f, timeout=120, check=True)
    print(f"  {out.name}")


def clear_focused_field(presses: int = 60):
    """Опустошает поле в фокусе.

    Ctrl+A + Delete: поле «Сервер» приходит НЕ пустым, там уже стоит адрес
    прода, и `input text` дописывался в середину строки, давая мусор вроде
    «https://splitor.zagirnur.dhttp://10.0.2.2:7171ev».
    """
    sh("shell", "input", "keycombination", "113", "29")
    time.sleep(0.4)
    for _ in range(presses):
        sh("shell", "input", "keyevent", "67")


def point_at_local_backend():
    """Переключает приложение на локальный бэкенд с демо-данными.

    Эмулятор видит хост как 10.0.2.2, а поле «Сервер» на экране входа спрятано
    за пятью тапами по иконке (только DEBUG) — тот же жест, что и у человека.
    """
    # Жест повторяем до результата: счётчик тапов живёт в состоянии экрана и
    # сбрасывается вместе с ним, а на медленном эмуляторе часть тапов теряется.
    field = None
    for attempt in range(4):
        mark = find("Splitty", exact=True)
        if mark is None:
            raise RuntimeError("не найден блок с названием — экран входа не открылся")
        x, y = center(mark)
        for _ in range(5):
            sh("shell", "input", "tap", str(x), str(y))
            time.sleep(0.35)
        time.sleep(1.5)
        # Явное сравнение с None обязательно: Element без детей ЛОЖЕН, и
        # «find(a) or find(b)» молча выбрасывал найденный узел.
        field = find("splitor.zagirnur.dev")
        if field is None:
            field = find("10.0.2.2")
        if field is not None:
            break
    if field is None:
        raise RuntimeError("поле «Сервер» не появилось после жеста")
    fx, fy = center(field)
    sh("shell", "input", "tap", str(fx), str(fy))
    time.sleep(1.0)
    clear_focused_field()
    sh("shell", "input", "text", "http://10.0.2.2:7171")
    time.sleep(0.8)
    sh("shell", "input", "keyevent", "111")  # Esc — убрать клавиатуру
    time.sleep(1.2)


def capture(lang: str):
    cfg = LABELS[lang]
    dst = ROOT / "metadata" / "screenshots-android" / lang
    dst.mkdir(parents=True, exist_ok=True)
    for old in dst.glob("*.png"):
        old.unlink()
    index = [0]

    # Чистый старт: язык приложения, снесённые данные, «заряжено» в статус-баре.
    sh("shell", "pm", "clear", PKG)
    sh("shell", "cmd", "locale", "set-app-locales", PKG, "--locales", cfg["locale"])
    sh("shell", "settings", "put", "global", "sysui_demo_allowed", "1")
    sh("shell", "am", "broadcast", "-a", "com.android.systemui.demo",
       "-e", "command", "enter")
    sh("shell", "am", "broadcast", "-a", "com.android.systemui.demo",
       "-e", "command", "clock", "-e", "hhmm", "0941")
    sh("shell", "am", "broadcast", "-a", "com.android.systemui.demo",
       "-e", "command", "battery", "-e", "level", "100", "-e", "plugged", "false")
    sh("shell", "am", "broadcast", "-a", "com.android.systemui.demo",
       "-e", "command", "network", "-e", "wifi", "show", "-e", "level", "4")
    sh("shell", "am", "broadcast", "-a", "com.android.systemui.demo",
       "-e", "command", "notifications", "-e", "visible", "false")
    sh("shell", "monkey", "-p", PKG, "-c", "android.intent.category.LAUNCHER", "1")
    time.sleep(6)

    # Системный запрос на уведомления перекрывает экран входа и в кадр
    # витрине не нужен: отклоняем, разрешение здесь ни на что не влияет.
    for deny in ("Don\u2019t allow", "Don't allow", "Не разрешать", "No permitir",
                 "Nicht zulassen", "Ne pas autoriser"):
        if tap_text(deny, exact=True, required=False):
            time.sleep(1.5)
            break

    point_at_local_backend()

    tap_text(cfg["disclosure"])
    time.sleep(1.5)
    tap_text("Email", exact=True)
    time.sleep(1.0)
    clear_focused_field(45)
    sh("shell", "input", "text", cfg["email"])
    time.sleep(0.8)
    # Между полями — TAB, а не тап: с выехавшей клавиатурой шторка уезжает
    # вверх, и координаты из предыдущего дампа промахиваются мимо пароля.
    sh("shell", "input", "keyevent", "61")
    time.sleep(0.8)
    sh("shell", "input", "text", PASSWORD)
    time.sleep(0.8)
    sh("shell", "input", "keyevent", "66")
    time.sleep(8)

    tap_text(cfg["groups"])
    time.sleep(1.5)
    shoot(dst, "groups", index)

    tap_text(cfg["room"])
    time.sleep(1.5)
    shoot(dst, "group", index)

    if tap_text(cfg["balances"], required=False):
        time.sleep(1.0)
        shoot(dst, "balances", index)

    if tap_text(cfg["totals"], required=False):
        time.sleep(1.8)
        shoot(dst, "totals", index)

    sh("shell", "input", "keyevent", "4")  # назад к списку групп
    time.sleep(1.5)
    tap_text(cfg["friends"])
    time.sleep(1.5)
    shoot(dst, "friends", index)

    print(f"[{lang}] кадров: {index[0]} → {dst.relative_to(ROOT)}")


if __name__ == "__main__":
    for lang in (sys.argv[1:] or ["ru", "en"]):
        if lang not in LABELS:
            sys.exit(f"нет набора для «{lang}»")
        capture(lang)
