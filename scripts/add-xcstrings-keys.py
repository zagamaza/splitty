#!/usr/bin/env python3
"""Заводит в String Catalog ключи, которых там ещё нет.

merge-xcstrings.py вливает перевод в СУЩЕСТВУЮЩУЮ запись и на незнакомый ключ
падает — это правильно, так ловятся опечатки. Но строку, которую код просит, а
каталога она не видела, влить некуда: `String(localized:)` без записи молча
возвращает сам ключ, то есть русский текст на любом языке. Этот скрипт и
создаёт такие записи.

Список ключей, которые РЕАЛЬНО просит код, снимается сборкой:

    xcodebuild ... SWIFT_EMIT_LOC_STRINGS=YES
    # ключи лежат в Objects-normal/<arch>/*.stringsdata

На входе — JSON `{ключ: {язык: перевод}}`. Перевод — строка либо словарь форм
множественного числа (`{"one": …, "other": …}`); формы кладутся в variations,
как их пишет Xcode. Ключ, который в каталоге уже есть, — ошибка: перезаписью
существующих переводов занимается merge-xcstrings.py.

    python3 scripts/add-xcstrings-keys.py scripts/i18n/new-keys.json
"""
import json
import pathlib
import sys

CATALOG = pathlib.Path(__file__).resolve().parent.parent / "ios/Splitty/Localizable.xcstrings"


def unit(value):
    return {"stringUnit": {"state": "translated", "value": value}}


def localization(value):
    if isinstance(value, dict):
        return {"variations": {"plural": {form: unit(v) for form, v in value.items()}}}
    return unit(value)


def add(catalog: dict, entries: dict) -> int:
    strings = catalog["strings"]
    existing = [k for k in entries if k in strings]
    if existing:
        raise KeyError(f"уже в каталоге, лей через merge-xcstrings.py: {existing[:5]}")

    for key, translations in entries.items():
        strings[key] = {
            "extractionState": "manual",
            "localizations": {lang: localization(v) for lang, v in translations.items()},
        }
    # Xcode держит ключи по алфавиту и при первой же записи переставил бы их
    # сам — диф на весь файл вместо новых строк.
    catalog["strings"] = {k: strings[k] for k in sorted(strings)}
    return len(entries)


def dump(catalog: dict, path: pathlib.Path) -> None:
    text = json.dumps(catalog, ensure_ascii=False, indent=2)
    path.write_text(text + "\n", encoding="utf-8")


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    entries = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    added = add(catalog, entries)
    dump(catalog, CATALOG)
    print(f"заведено ключей: {added}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
