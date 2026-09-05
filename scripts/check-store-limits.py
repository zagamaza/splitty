#!/usr/bin/env python3
"""Лимиты и обязательные блоки витрин App Store и Google Play.

Обрезанное описание в магазине выглядит как брак, а его длину видно только
после заливки: ASC отвечает ошибкой, Play молча режет. Поэтому считаем здесь.

Отдельно проверяется блок про подписку. Отказ по 3.1.2(c) мы уже получали:
Apple требует в описании название подписки, длительность, цену, что подписка
продлевается сама, и ссылки на условия и политику. Забыть одну строку легко —
она не мешает ни собрать, ни залить.

    python3 scripts/check-store-limits.py            # свежая версия ASC + Play
    python3 scripts/check-store-limits.py 1.4 1.9    # конкретные версии

Код возврата 1, если хоть что-то не сошлось.
"""
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
META = ROOT / "metadata"

# Поле: предел в символах. Считаем по символам, а не по байтам: японский
# укладывается в 30 символов, но не в 30 байт.
ASC_VERSION = {"description": 4000, "keywords": 100, "promotionalText": 170, "whatsNew": 4000}
ASC_APP_INFO = {"name": 30, "subtitle": 30}
PLAY = {"title": 30, "shortDescription": 80, "fullDescription": 4000}

TERMS_URL = "https://splitor.zagirnur.dev/terms"
PRIVACY_URL = "https://splitor.zagirnur.dev/privacy"

# Цена в описании обязана быть. Знак стоит и до числа («$2.99»), и после
# («2,99 $», «299 ₽») — в наших переводах как раз второй вариант.
PRICE = re.compile(r"[\$€₽¥]\s?\d|\d[\d\s.,]*\s?[\$€₽¥]|\d[\d.,]*\s?(?:USD|EUR|RUB|JPY|KRW|CNY|BRL)")


def check(text: str, field: str, limit: int, where: str, problems: list[str]) -> None:
    if len(text) > limit:
        problems.append(f"{where}: {field} — {len(text)} символов, предел {limit}")


def check_subscription_block(description: str, where: str, problems: list[str]) -> None:
    """Требования 3.1.2(c) к описанию платного приложения."""
    if "Plus" not in description and "PLUS" not in description:
        problems.append(f"{where}: нет блока про подписку Splitor Plus")
        return
    if TERMS_URL not in description:
        problems.append(f"{where}: нет ссылки на условия ({TERMS_URL})")
    if PRIVACY_URL not in description:
        problems.append(f"{where}: нет ссылки на политику ({PRIVACY_URL})")
    if not PRICE.search(description):
        problems.append(f"{where}: в блоке про подписку нет цены")


def check_asc(version: str, problems: list[str]) -> int:
    checked = 0
    for path in sorted((META / "version" / version).glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        where = f"asc/{version}/{path.stem}"
        for field, limit in ASC_VERSION.items():
            if field in data:
                check(data[field], field, limit, where, problems)
        if "description" in data:
            check_subscription_block(data["description"], where, problems)
        checked += 1
    for path in sorted((META / "app-info").glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        where = f"asc/app-info/{path.stem}"
        for field, limit in ASC_APP_INFO.items():
            if field in data:
                check(data[field], field, limit, where, problems)
        checked += 1
    return checked


# Блок про подписку проверяется только для App Store: требование 3.1.2(c)
# апстора, и отказ мы получали именно там. У Play описание подписки живёт в
# консоли, а не в тексте витрины.
def check_play(problems: list[str]) -> int:
    checked = 0
    for path in sorted((META / "play").glob("*.json")):
        data = json.loads(path.read_text(encoding="utf-8"))
        where = f"play/{path.stem}"
        for field, limit in PLAY.items():
            if field in data:
                check(data[field], field, limit, where, problems)
        checked += 1
    return checked


def versions() -> list[str]:
    """Свежая версия каталога. Прошлые не проверяем: 1.4 вышла до подписки, и
    требовать от неё блок 3.1.2(c) значит держать красным то, что уже продано.
    """
    names = sorted((p.name for p in (META / "version").iterdir() if p.is_dir()),
                   key=lambda n: [int(x) for x in n.split(".")])
    return names[-1:]


def main(argv: list[str]) -> int:
    wanted = argv or versions()
    problems: list[str] = []
    checked = sum(check_asc(v, problems) for v in wanted) + check_play(problems)

    print(f"проверено файлов: {checked} (версии ASC: {', '.join(wanted)})")
    for item in problems:
        print("  ✗", item)
    if problems:
        print(f"\nнарушений: {len(problems)}")
        return 1
    print("лимиты и обязательные блоки — в порядке")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
