#!/usr/bin/env python3
"""Выгружает ключи каталога с опорным переводом — вход для перевода.

    python3 scripts/export-xcstrings.py en 0 100

Печатает JSON `{русский ключ: английский перевод}` для порции ключей. Ключи с
множественными формами пропускаются: у них своя структура и свои правила по
языкам (задача 3 плана).
"""
import json
import pathlib
import sys

CATALOG = pathlib.Path(__file__).resolve().parent.parent / "ios/Splitty/Localizable.xcstrings"


def plain_keys(catalog: dict) -> list[str]:
    out = []
    for key, entry in catalog["strings"].items():
        locs = entry.get("localizations", {})
        if any("variations" in unit for unit in locs.values()):
            continue
        out.append(key)
    return out


def main(argv: list[str]) -> int:
    reference = argv[0] if argv else "en"
    start = int(argv[1]) if len(argv) > 1 else 0
    count = int(argv[2]) if len(argv) > 2 else 10**6
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    keys = plain_keys(catalog)[start:start + count]
    out = {}
    for key in keys:
        unit = catalog["strings"][key].get("localizations", {}).get(reference, {})
        out[key] = unit.get("stringUnit", {}).get("value", "")
    print(json.dumps(out, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
