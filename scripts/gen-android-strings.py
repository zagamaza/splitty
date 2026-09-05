#!/usr/bin/env python3
"""Собирает values-<локаль>/strings.xml из базового файла и iOS-каталога.

Тексты у клиентов одни и те же, поэтому перевод берётся по АНГЛИЙСКОМУ значению
из ios/Splitty/Localizable.xcstrings. Так терминология сходится между iOS и
Android сама, без сверки руками — это требование плана.

Строки, которых в каталоге нет, пропускаются: пусть каталожный тест покажет их
списком, чем в файл попадёт английская копия, неотличимая от перевода.

    python3 scripts/gen-android-strings.py ja values-ja [добор.json]

Третьим аргументом можно передать `{ключ: перевод}` для строк, которых в
каталоге не нашлось.
"""
import html
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
BASE = ROOT / "android/app/src/main/res/values/strings.xml"
CATALOG = ROOT / "ios/Splitty/Localizable.xcstrings"


def ios_index() -> dict:
    """{английский текст: {язык: перевод}} из каталога iOS."""
    cat = json.loads(CATALOG.read_text(encoding="utf-8"))
    out = {}
    for entry in cat["strings"].values():
        locs = entry.get("localizations", {})
        en = locs.get("en", {}).get("stringUnit", {}).get("value")
        if not en:
            continue
        out[en] = {lang: unit["stringUnit"]["value"]
                   for lang, unit in locs.items() if "stringUnit" in unit}
    return out


def unescape(value: str) -> str:
    return html.unescape(value.replace("\\'", "'").replace("\\n", "\n")).strip()


def to_ios(value: str) -> str:
    """%1$s → %1$@, %d → %lld: у платформ разные спецификаторы."""
    value = re.sub(r"%(\d+\$)?s", lambda m: "%" + (m.group(1) or "") + "@", value)
    return re.sub(r"%(\d+\$)?d", lambda m: "%" + (m.group(1) or "") + "lld", value)


def to_android(value: str) -> str:
    value = re.sub(r"%(\d+\$)?@", lambda m: "%" + (m.group(1) or "") + "s", value)
    return re.sub(r"%(\d+\$)?lld", lambda m: "%" + (m.group(1) or "") + "d", value)


def escape(value: str) -> str:
    """Экранирование ресурсов Android: XML плюс апостроф — иначе сборка падает."""
    value = value.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
    return value.replace("'", "\\'").replace("\n", "\\n")


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__)
        return 2
    lang, folder = argv[0], argv[1]
    extra = json.loads(pathlib.Path(argv[2]).read_text(encoding="utf-8")) if len(argv) > 2 else {}

    index = ios_index()
    base = BASE.read_text(encoding="utf-8")
    lines = ['<?xml version="1.0" encoding="utf-8"?>',
             "<!-- Сгенерировано scripts/gen-android-strings.py: переводы берутся по",
             "     английскому значению из ios/Splitty/Localizable.xcstrings, чтобы",
             "     терминология совпадала с iOS. -->",
             "<resources>"]
    missing = []
    for match in re.finditer(r'<string name="([^"]+)"([^>]*)>(.*?)</string>', base, re.S):
        name, attrs, value = match.groups()
        if 'translatable="false"' in attrs:
            continue
        if name in extra:
            translation = extra[name]
        else:
            translation = index.get(to_ios(unescape(value)), {}).get(lang)
        if not translation:
            missing.append(name)
            continue
        lines.append(f'    <string name="{name}">{escape(to_android(translation))}</string>')
    # Множественные формы: у ja/zh-Hans/ko одна форма other, у pt-BR и it две.
    # Их набор задан таблицей, а не переносом из iOS: там свои имена ключей.
    plurals = json.loads((ROOT / "scripts/i18n/android-plurals.json").read_text(encoding="utf-8"))
    for name, langs in plurals.items():
        forms = langs.get(lang)
        if not forms:
            missing.append(name)
            continue
        lines.append(f'    <plurals name="{name}">')
        for quantity, value in forms.items():
            lines.append(f'        <item quantity="{quantity}">{escape(value)}</item>')
        lines.append("    </plurals>")

    lines.append("</resources>")

    target = ROOT / "android/app/src/main/res" / folder / "strings.xml"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")
    pathlib.Path(f"/tmp/android-missing-{lang}.json").write_text(
        json.dumps(missing, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"{folder}: записано {len(lines) - 5}, не нашлось {len(missing)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
