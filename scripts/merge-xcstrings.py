#!/usr/bin/env python3
"""Вливает переводы в String Catalog, сохраняя формат Xcode.

Каталог — 600 КБ JSON, который Xcode переписывает целиком: ключи отсортированы,
отступ два пробела, в конце перевод строки. Точечные правки в нём делать нельзя,
а переписывание «как получится» ломает diff и путает Xcode, поэтому слияние
идёт только через этот скрипт.

На входе — словарь `{ключ каталога: перевод}` в JSON. На выходе — тот же
каталог с добавленным языком; существующие языки не трогаются.

    python3 scripts/merge-xcstrings.py ja scripts/i18n/ja.json

Ключ, которого нет в каталоге, — ошибка: это опечатка в словаре, и молча
проглотить её значит потерять перевод.
"""
import json
import pathlib
import sys

CATALOG = pathlib.Path(__file__).resolve().parent.parent / "ios/Splitty/Localizable.xcstrings"


def merge(catalog: dict, language: str, translations: dict) -> tuple[int, int]:
    """Возвращает (влито, уже было). Бросает KeyError на неизвестный ключ."""
    strings = catalog["strings"]
    unknown = [k for k in translations if k not in strings]
    if unknown:
        raise KeyError(f"нет в каталоге: {unknown[:5]}")

    added = existed = 0
    for key, value in translations.items():
        entry = strings[key]
        locs = entry.setdefault("localizations", {})
        if language in locs:
            existed += 1
        else:
            added += 1
        # Множественные формы этим скриптом не заливаются: у них своя структура
        # и свои правила по языкам — см. задачу 3 плана.
        locs[language] = {"stringUnit": {"state": "translated", "value": value}}
    return added, existed


def dump(catalog: dict, path: pathlib.Path) -> None:
    """Пишет отступом 2 и переводом строки в конце, СОХРАНЯЯ порядок ключей.

    Сортировать нельзя: языки внутри записи лежат не по алфавиту, и `sort_keys`
    переставил бы их все — диф на 454 строки вместо шести, а ревью такого
    файла невозможно. Порядок держится сам: json.loads сохраняет порядок из
    файла, а новый язык дописывается в конец записи.
    """
    text = json.dumps(catalog, ensure_ascii=False, indent=2)
    path.write_text(text + "\n", encoding="utf-8")


def plain_keys(catalog: dict) -> list[str]:
    """Ключи БЕЗ множественных форм, в порядке файла — тот же, что у выгрузки."""
    return [k for k, e in catalog["strings"].items()
            if not any("variations" in u for u in e.get("localizations", {}).values())]


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__)
        return 2
    language, source = argv[0], pathlib.Path(argv[1])
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    payload = json.loads(source.read_text(encoding="utf-8"))

    if isinstance(payload, list):
        # Порядковая форма: список переводов для среза plain_keys, начиная со
        # start. Русские ключи в файл не повторяются — на объёме в тысячи строк
        # это ровно половина лишнего текста.
        start = int(argv[2]) if len(argv) > 2 else 0
        keys = plain_keys(catalog)[start:start + len(payload)]
        if len(keys) != len(payload):
            raise SystemExit(f"переводов {len(payload)}, а ключей в срезе {len(keys)}")
        translations = dict(zip(keys, payload))
    else:
        translations = payload

    added, existed = merge(catalog, language, translations)
    dump(catalog, CATALOG)

    total = len(catalog["strings"])
    done = sum(1 for e in catalog["strings"].values()
               if language in e.get("localizations", {}))
    print(f"{language}: влито {added}, перезаписано {existed}; "
          f"переведено {done} из {total}, осталось {total - done}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
