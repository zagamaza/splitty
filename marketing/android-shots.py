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

# Подписи для поиска берутся из ресурсов приложения на нужном языке, а не
# переписываются сюда руками: прежний список отставал от вёрстки («Рассчитаться ↗»
# после редизайна стало «Рассчитаться») и знал только ru и en.
RES = ROOT / "android" / "app" / "src" / "main" / "res"
LOCALES = {
    "ru": ("values-ru", "shots-ru@splitty.test",
           ["Дача на выходные", "Квартира на Тверской", "Поездка в Стамбул"], "Поездка в Стамбул"),
    "en": ("values", "shots-en@splitty.test",
           ["Weekend cabin", "Flat share", "Trip to Lisbon"], "Trip to Lisbon"),
    "es": ("values-es", "shots-es@splitty.test",
           ["Finca el finde", "Piso compartido", "Viaje a Barcelona"], "Viaje a Barcelona"),
    "de": ("values-de", "shots-de@splitty.test",
           ["Wochenende am See", "WG Prenzlauer Berg", "Städtetrip Berlin"], "Städtetrip Berlin"),
    "fr": ("values-fr", "shots-fr@splitty.test",
           ["Chalet en montagne", "Coloc rue Vieille", "Week-end à Lyon"], "Week-end à Lyon"),
    "ja": ("values-ja", "shots-ja@splitty.test", ["シェアハウス", "京都旅行", "週末の別荘"], "京都旅行"),
    "zh-Hans": ("values-zh-rCN", "shots-zh@splitty.test", ["周末民宿", "合租公寓", "成都之行"], "成都之行"),
    "ko": ("values-ko", "shots-ko@splitty.test", ["셰어하우스", "제주도 여행", "주말 펜션"], "제주도 여행"),
    "pt-BR": ("values-pt-rBR", "shots-pt@splitty.test",
              ["República", "Sítio no fim de semana", "Viagem a Salvador"], "Viagem a Salvador"),
    "it": ("values-it", "shots-it@splitty.test",
           ["Casa condivisa", "Viaggio a Napoli", "Weekend in montagna"], "Viaggio a Napoli"),
}


# Расход, разобранный по позициям, — кадр «чек» (те же демо-данные, что у iOS).
RECEIPT_EXPENSE = {
    'ru': 'Ужин в Кадыкёе',
    'en': 'Dinner in Alfama',
    'es': 'Cena en el Born',
    'de': 'Abendessen in Kreuzberg',
    'fr': 'Dîner à la Croix-Rousse',
    'ja': '先斗町の夕食',
    'zh-Hans': '宽窄巷子晚餐',
    'ko': '흑돼지 저녁',
    'pt-BR': 'Jantar no Pelourinho',
    'it': 'Cena a Spaccanapoli',
}


def labels(lang: str) -> dict:
    values, email, rooms, room = LOCALES[lang]
    raw = (RES / values / "strings.xml").read_text(encoding="utf-8")

    def res(name: str) -> str:
        m = re.search(rf'<string name="{name}">([^<]*)</string>', raw)
        if not m:
            sys.exit(f"[{lang}] нет строки {name}")
        return m.group(1).replace("\\'", "'")

    return {
        # Язык приложения в Android — тег BCP 47: zh-Hans → zh-CN, pt-BR как есть.
        "locale": {"zh-Hans": "zh-CN"}.get(lang, lang),
        "email": email, "room": room, "rooms": rooms,
        "groups": res("tab_groups"), "friends": res("tab_friends"),
        "totals": res("totals_title"), "balances": res("group_balances_title"),
        "disclosure": res("login_email_disclosure_action"),
        "email_field": res("login_email_placeholder"),
        "skip": res("welcome_skip"), "settle": res("group_settle_up"),
        "receipt": RECEIPT_EXPENSE[lang],
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
        mark = find("Splitor", exact=True)
        if mark is None:
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


def tap_add_tab():
    """Тапает центральную вкладку «+»: у неё нет текстовой подписи."""
    nodes = [n for n in dump().iter("node")
             if n.get("bounds") and (n.get("content-desc") or "").strip()]
    # Нижняя панель — самые низкие кликабельные элементы экрана.
    bottom = sorted(nodes, key=lambda n: center(n)[1])[-5:]
    bottom.sort(key=lambda n: center(n)[0])
    if not bottom:
        raise RuntimeError("не нашёл нижнюю панель")
    x, y = center(bottom[len(bottom) // 2])
    sh("shell", "input", "tap", str(x), str(y))
    time.sleep(1.2)


def ensure_foreground():
    """Перед съёмкой приложение обязано быть на экране: лишний «назад» или
    запрос разрешения иногда уводят его в лаунчер, и кадры уезжают мимо."""
    focus = sh("shell", "dumpsys", "window").stdout.split("mCurrentFocus=", 1)[-1].split("\n", 1)[0]
    if PKG not in focus:
        sh("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
        time.sleep(5)
        deny_notifications()


def deny_notifications():
    """Системный запрос на уведомления перекрывает экран; в кадр он не нужен."""
    for deny in ("Don\u2019t allow", "Don't allow", "Не разрешать", "Запретить", "No permitir",
                 "Nicht zulassen", "Ne pas autoriser"):
        if tap_text(deny, exact=True, required=False):
            time.sleep(1.5)
            return


def dismiss_keyboard_setup():
    """Мастер раскладки Gboard для нового языка (ja, ko, zh) встаёт поверх
    приложения и прячет экран входа. В дерево uiautomator окно клавиатуры не
    попадает, поэтому смотрим на сам метод ввода: показан до того, как мы
    тронули поле, — значит это мастер, и «назад» его закрывает."""
    shown = sh("shell", "dumpsys", "input_method").stdout
    if "mInputShown=true" in shown:
        sh("shell", "input", "keyevent", "4")
        time.sleep(1.5)
        # Если клавиатуры на деле не было, «назад» закрыл само приложение.
        focus = sh("shell", "dumpsys", "window").stdout
        if f"mCurrentFocus=Window{{" in focus and PKG not in focus.split("mCurrentFocus=", 1)[1].split("\n", 1)[0]:
            sh("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
            time.sleep(4)


def capture(lang: str):
    cfg = labels(lang)
    dst = ROOT / "metadata" / "screenshots-android" / lang
    dst.mkdir(parents=True, exist_ok=True)
    for old in dst.glob("*.png"):
        old.unlink()
    index = [0]

    # Чистый старт: язык приложения, снесённые данные, «заряжено» в статус-баре.
    # Вход — латиницей: при языке приложения ja/zh/ko Gboard переключается на
    # свою раскладку и переиначивает набранный пароль. Входим под английским
    # интерфейсом, язык ставим уже после входа — сессия переживает перезапуск.
    login_cfg = labels("en") if lang in ("ja", "zh-Hans", "ko") else cfg
    sh("shell", "pm", "clear", PKG)
    sh("shell", "cmd", "locale", "set-app-locales", PKG, "--locales", login_cfg["locale"])
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
    # `am start`, а не monkey: на образах без физических клавиш monkey
    # завершается с кодом -5, ничего не запустив.
    sh("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
    time.sleep(6)

    # Системный запрос на уведомления перекрывает экран входа и в кадр
    # витрине не нужен: отклоняем, разрешение здесь ни на что не влияет.
    deny_notifications()

    # Приветствие стоит ПЕРЕД входом: на чистом старте первым показывается
    # оно, и без пропуска экрана входа нет.
    if tap_text(login_cfg["skip"], exact=True, required=False):
        time.sleep(1.5)

    dismiss_keyboard_setup()
    point_at_local_backend()

    # На длинных переводах (ja) ссылка входа по email ниже края экрана.
    for _ in range(3):
        dismiss_keyboard_setup()
        if tap_text(login_cfg["disclosure"], required=False):
            break
        sh("shell", "input", "swipe", "540", "1800", "540", "700", "300")
        time.sleep(1.0)
    else:
        tap_text(login_cfg["disclosure"])
    time.sleep(1.5)
    tap_text(login_cfg["email_field"], exact=True)
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

    if login_cfg is not cfg:
        sh("shell", "cmd", "locale", "set-app-locales", PKG, "--locales", cfg["locale"])
        sh("shell", "am", "force-stop", PKG)
        sh("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
        time.sleep(6)
        deny_notifications()

    ensure_foreground()
    tap_text(cfg["groups"])
    time.sleep(1.5)
    shoot(dst, "groups", index)

    tap_text(cfg["room"])
    time.sleep(1.5)
    shoot(dst, "group", index)

    # Разбор чека: расход с позициями открывается документом, позиции — ниже
    # участников. Кадр «погашение» витрина больше не берёт: список долгов
    # повторял «Балансы» и не доказывал «один перевод вместо пяти».
    for _ in range(4):
        if find(cfg["receipt"]) is not None:
            break
        sh("shell", "input", "swipe", "540", "1700", "540", "1000", "400")
        time.sleep(1.0)
    if tap_text(cfg["receipt"], required=False):
        time.sleep(2.0)
        # Медленный свайп без броска: позиции встают под шапку экрана.
        sh("shell", "input", "swipe", "540", "1850", "540", "550", "700")
        time.sleep(1.5)
        shoot(dst, "receipt", index)
        sh("shell", "input", "keyevent", "4")
        time.sleep(1.5)
        for _ in range(4):
            sh("shell", "input", "swipe", "540", "800", "540", "1800", "300")
        time.sleep(1.0)

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

    # Голосовой композер — самый непохожий на конкурентов экран, и в витрине
    # Play его не хватало. Центральная вкладка «+» подписи не имеет, поэтому
    # берём среднюю кнопку нижней панели.
    tap_add_tab()
    time.sleep(2.0)
    for room in cfg["rooms"]:
        if tap_text(room, required=False):
            break
    time.sleep(1.5)
    shoot(dst, "add", index)

    print(f"[{lang}] кадров: {index[0]} → {dst.relative_to(ROOT)}")


if __name__ == "__main__":
    for lang in (sys.argv[1:] or ["ru", "en"]):
        if lang not in LOCALES:
            sys.exit(f"нет набора для «{lang}»")
        capture(lang)
