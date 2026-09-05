#!/usr/bin/env python3
"""Тесты слияния переводов в String Catalog.

Первая же версия скрипта сортировала ключи и переставила языки во всех 634
записях: диф на 454 строки вместо шести. Поэтому проверяется не только «перевод
доехал», но и что порядок ключей и чужие языки остались нетронутыми.
"""
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import importlib
merge_mod = importlib.import_module("merge-xcstrings")


def catalog_fixture() -> dict:
    return {
        "sourceLanguage": "ru",
        "strings": {
            "Профиль": {"localizations": {
                "en": {"stringUnit": {"state": "translated", "value": "Profile"}},
                "de": {"stringUnit": {"state": "translated", "value": "Profil"}},
            }},
            "Долги": {"localizations": {
                "en": {"stringUnit": {"state": "translated", "value": "Debts"}},
            }},
        },
        "version": "1.0",
    }


class MergeTests(unittest.TestCase):
    def test_adds_language(self):
        cat = catalog_fixture()
        added, existed = merge_mod.merge(cat, "ja", {"Профиль": "プロフィール"})
        self.assertEqual((added, existed), (1, 0))
        unit = cat["strings"]["Профиль"]["localizations"]["ja"]["stringUnit"]
        self.assertEqual(unit["value"], "プロフィール")
        self.assertEqual(unit["state"], "translated")

    def test_keeps_other_languages_and_their_order(self):
        cat = catalog_fixture()
        merge_mod.merge(cat, "ja", {"Профиль": "プロフィール"})
        locs = cat["strings"]["Профиль"]["localizations"]
        self.assertEqual(list(locs), ["en", "de", "ja"], "новый язык дописан в конец, чужие не переставлены")
        self.assertEqual(locs["en"]["stringUnit"]["value"], "Profile")

    def test_keeps_key_order(self):
        cat = catalog_fixture()
        merge_mod.merge(cat, "ja", {"Долги": "借金"})
        self.assertEqual(list(cat["strings"]), ["Профиль", "Долги"], "ключи не переставлены")

    def test_unknown_key_is_an_error(self):
        with self.assertRaises(KeyError):
            merge_mod.merge(catalog_fixture(), "ja", {"Такого ключа нет": "x"})

    def test_dump_keeps_non_alphabetical_order(self):
        """Именно эта регрессия и случилась: sort_keys переставил языки во всех
        записях каталога — «de» уехал перед «en», диф вышел на 454 строки."""
        cat = catalog_fixture()  # порядок языков en, de — НЕ по алфавиту
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "c.xcstrings"
            merge_mod.dump(cat, path)
            written = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(list(written["strings"]["Профиль"]["localizations"]), ["en", "de"])
            self.assertEqual(list(written["strings"]), ["Профиль", "Долги"])

    def test_dump_is_stable(self):
        """Повторная запись без правок не должна менять файл ни на байт."""
        cat = catalog_fixture()
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "c.xcstrings"
            merge_mod.dump(cat, path)
            first = path.read_bytes()
            merge_mod.dump(json.loads(path.read_text(encoding="utf-8")), path)
            self.assertEqual(first, path.read_bytes())
            self.assertTrue(first.endswith(b"\n"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
