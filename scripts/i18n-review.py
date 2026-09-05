#!/usr/bin/env python3
"""Счётные проверки переводов плюс выборка для человека.

Вычитать 647 строк на девяти языках человек не станет, а «агент посмотрел и всё
хорошо» ничего не значит. Поэтому две вещи, обе воспроизводимые:

1. Регистр. Японский и корейский различают вежливость грамматически, и
   смешивать формы в одном интерфейсе нельзя. Ищем не «есть ли вежливая
   форма» — её нет у половины интерфейса, потому что подписи кнопок и
   заголовки это существительные («Сохранить» → 保存), — а ПРОСТУЮ форму
   (だ/する/した, 한다/해/이다) там, где рядом всё вежливое. Так проверка ловит
   ровно то, что портит впечатление, и не тонет в ложных срабатываниях.

2. Терминология. Для ключевых слов эталон берётся из САМОГО каталога: перевод
   отдельной записи «Расход» и есть целевое слово. Дальше проверяется, что в
   каждой строке, где по-русски стоит этот термин, в переводе стоит то же
   слово. Список синонимов выдумывать не нужно — расхождение видно счётчиком.

Артефакт для человека — docs/i18n-review/<язык>.md: тридцать строк вперемешку
(и подписи, и длинные тексты) рядом с русским оригиналом.

    python3 scripts/i18n-review.py
"""
import json
import pathlib
import random
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
CATALOG = ROOT / "ios/Splitty/Localizable.xcstrings"
OUT = ROOT / "docs/i18n-review"

LANGUAGES = ["en", "de", "fr", "es", "it", "pt-BR", "ja", "ko", "zh-Hans"]

# Простые (невежливые) окончания. Существительное на конце — норма, а вот
# сказуемое в простой форме рядом с вежливым соседом это разнобой.
PLAIN = {
    "ja": ("する", "した", "しない", "だろう", "である", "になる", "できる", "いない"),
    "ko": ("한다", "이다", "했다", "없다", "있다", "된다", "하자"),
}

# Вежливые окончания — их наличие снимает подозрение (…しました, …ですか).
POLITE = {
    "ja": ("ます", "ました", "ません", "でした", "です", "ください", "ましょう",
           "ますか", "ですか", "でしょう", "ませんでした"),
    "ko": ("습니다", "합니다", "세요", "십시오", "까요", "니까", "됩니다", "옵니다"),
}

# Термин: (что искать в русском, ключ-эталон в каталоге). Эталон — отдельная
# запись каталога с этим словом; её перевод и есть целевое слово.
#
# «Плательщика» тут нет намеренно: отдельной записи с этим словом на iOS не
# существует, оно живёт внутри трёх длинных предложений. Выдумывать эталон из
# головы значит проверять не каталог, а свою догадку.
TERMS = {
    "расход": (r"(?<!пере)расход", "Расход"),
    "группа": (r"групп", "Группа"),
    "участник": (r"участник", "Участник"),
    "долг": (r"долг(?!о(?!в))", "Долги"),   # «долго не отвечает» — не термин
    "доля": (r"\bдол[яию]\b|\bдолей\b|\bдоле\b", "доля"),
}

# Названия, которые перевода не подлежат: их узнают по написанию.
BRANDS = ("Splitty", "Plus", "Apple ID", "Google", "Telegram")


def value(entry: dict, lang: str) -> str | None:
    loc = entry.get("localizations", {}).get(lang)
    if not loc:
        return None
    if "stringUnit" in loc:
        return loc["stringUnit"]["value"]
    forms = loc.get("variations", {}).get("plural", {})
    other = forms.get("other") or next(iter(forms.values()), None)
    return other["stringUnit"]["value"] if other else None


def stem(word: str) -> str:
    """Основа целевого слова: «Ausgabe» ловит «Ausgaben», «Gasto» — «gastos»."""
    word = word.strip().lower()
    return word[: max(4, len(word) - 2)]


def check_register(strings: dict, lang: str) -> list[str]:
    if lang not in PLAIN:
        return []
    bad = []
    for key, entry in strings.items():
        text = value(entry, lang)
        ru = value(entry, "ru") or key
        if not text:
            continue
        # Подписи кнопок в счёт не идут: «Пригласить» → 招待する это норма, а не
        # разнобой. Предложение опознаём по точке — в русском оригинале либо в
        # переводе (у японского и корейского своя, 。).
        if not (ru.rstrip().endswith((".", "!", "?")) or "。" in text or "." in text):
            continue
        tail = text.rstrip(" 。．.!！?？」』)）")
        if tail.endswith(POLITE[lang]):
            continue
        if tail.endswith(PLAIN[lang]):
            bad.append(f"{key!r} → {text!r}")
    return bad


def check_brands(strings: dict, lang: str) -> list[str]:
    bad = []
    for key, entry in strings.items():
        ru = value(entry, "ru") or key
        text = value(entry, lang)
        if not text:
            continue
        for brand in BRANDS:
            if brand in ru and brand not in text:
                bad.append(f"«{brand}» потерян: {key!r} → {text!r}")
    return bad


def check_terms(strings: dict, lang: str) -> tuple[list[str], dict[str, str]]:
    targets = {}
    for term, (_, reference) in TERMS.items():
        entry = strings.get(reference)
        target = value(entry, lang) if entry else None
        if target:
            targets[term] = target

    bad = []
    for term, (pattern, _) in TERMS.items():
        target = targets.get(term)
        if not target:
            bad.append(f"нет эталона: ключа «{TERMS[term][1]}» нет в каталоге или он без перевода")
            continue
        needle = stem(target)
        for key, entry in strings.items():
            ru = value(entry, "ru") or key
            if not re.search(pattern, ru, re.IGNORECASE):
                continue
            text = value(entry, lang)
            if text and needle not in text.lower():
                bad.append(f"«{term}» → ждали «{target}»: {key!r} → {text!r}")
    return bad, targets


def sample(strings: dict, lang: str, count: int = 30) -> list[tuple[str, str]]:
    rows = [(value(e, "ru") or k, value(e, lang)) for k, e in strings.items()]
    rows = [(ru, t) for ru, t in rows if t]
    # Половина коротких подписей, половина длинных текстов: обрезание видно
    # только на первых, кальки и канцелярит — только на вторых.
    short = [r for r in rows if len(r[0]) <= 25]
    long = [r for r in rows if len(r[0]) > 25]
    rnd = random.Random(20260905)
    return rnd.sample(short, min(count // 2, len(short))) + rnd.sample(long, min(count - count // 2, len(long)))


def main() -> int:
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    strings = catalog["strings"]
    OUT.mkdir(parents=True, exist_ok=True)

    problems = 0
    for lang in LANGUAGES:
        register = check_register(strings, lang)
        terms, targets = check_terms(strings, lang)
        brands = check_brands(strings, lang)
        terms += brands
        problems += len(register) + len(terms)

        lines = [f"# Выборка переводов: {lang}", ""]
        if targets:
            lines += ["## Термины (эталон из каталога)", ""]
            lines += [f"- {term} → **{word}**" for term, word in sorted(targets.items())]
            lines.append("")
        lines += [
            f"## Расхождений: регистр {len(register)}, терминология {len(terms)}",
            "",
            "Расхождение — повод посмотреть, а не приговор: пропуск слова там, где",
            "рядом стоит название в кавычках, для перевода нормален. Приговор тут",
            "один — разнобой: одно и то же русское слово переведено в двух местах",
            "по-разному.",
            "",
        ]
        for item in register[:20]:
            lines.append(f"- регистр: {item}")
        for item in terms[:20]:
            lines.append(f"- термин: {item}")
        if register[20:] or terms[20:]:
            lines.append(f"- …и ещё {len(register[20:]) + len(terms[20:])}")
        lines += ["", "## Тридцать строк для глаз", "", "| Русский | Перевод |", "| --- | --- |"]
        for ru, text in sample(strings, lang):
            lines.append(f"| {ru.replace(chr(10), ' ').replace('|', '\\|')} "
                         f"| {text.replace(chr(10), ' ').replace('|', '\\|')} |")
        (OUT / f"{lang}.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
        print(f"{lang}: регистр {len(register)}, терминология {len(terms)}")

    print(f"\nвсего расхождений: {problems}; выборки в {OUT.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
