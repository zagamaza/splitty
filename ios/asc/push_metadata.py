#!/usr/bin/env python3
"""Заливает тексты витрины App Store из metadata/ в App Store Connect.

Руками это пять языков × шесть полей в веб-интерфейсе, и каждая правка
описания повторяется заново. Здесь источник правды — файлы в metadata/,
а ASC получает их как есть.

    python3 ios/asc/push_metadata.py            # все языки
    python3 ios/asc/push_metadata.py ru en-US   # выборочно

Читает:
  metadata/app-info/<loc>.json    — name, subtitle, privacyPolicyUrl
  metadata/version/<ver>/<loc>.json — description, keywords, promotionalText,
                                      whatsNew, supportUrl

Каталог версии выбирается по номеру редактируемой версии в ASC, а не зашит:
с зашитой «1.4» скрипт залил бы в новую версию тексты позапрошлой, и заметить
это можно было бы только по «Что нового» на витрине. Если каталога с таким
номером нет, берётся `metadata/version/next` — черновик текстов, которым номер
ещё не назначен.
"""
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import asc  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent.parent
VERSIONS = ROOT / "metadata" / "version"
INFO_DIR = ROOT / "metadata" / "app-info"


def die(msg):
    sys.exit(f"ОШИБКА: {msg}")


def editable_version():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/appStoreVersions?limit=10")
    for v in r.get("data", []):
        if v["attributes"].get("appStoreState") in (
            "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED",
        ):
            return v["id"], v["attributes"].get("versionString")
    die("нет версии в редактируемом состоянии")


def app_info_id():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/appInfos")
    for info in r.get("data", []):
        if info["attributes"].get("appStoreState") != "READY_FOR_SALE":
            return info["id"]
    die("нет редактируемого appInfo")


def existing(path, key="locale"):
    st, r = asc.req("GET", path)
    return {x["attributes"][key]: x["id"] for x in r.get("data", [])}


def upsert(kind, parent_rel, parent_id, have, locale, attrs):
    """Патчит локализацию, если она есть, иначе заводит новую."""
    if locale in have:
        st, r = asc.req("PATCH", f"/v1/{kind}/{have[locale]}",
                        {"data": {"type": kind, "id": have[locale], "attributes": attrs}})
        action = "обновлено"
    else:
        body = {"data": {"type": kind, "attributes": dict(attrs, locale=locale),
                         "relationships": {parent_rel: {"data": {
                             "type": parent_rel[:-2] + "s" if False else PARENT_TYPE[parent_rel],
                             "id": parent_id}}}}}
        st, r = asc.req("POST", f"/v1/{kind}", body)
        action = "создано"
    if st >= 300:
        return action, st, json.dumps(r, ensure_ascii=False)[:400]
    return action, st, None


PARENT_TYPE = {"appInfo": "appInfos", "appStoreVersion": "appStoreVersions"}


def version_dir(version_string):
    exact = VERSIONS / str(version_string)
    if exact.is_dir():
        return exact
    draft = VERSIONS / "next"
    if draft.is_dir():
        print(f"каталога metadata/version/{version_string} нет — беру {draft.name}")
        return draft
    die(f"нет ни metadata/version/{version_string}, ни metadata/version/next")


def main(locales):
    version_id, version_string = editable_version()
    info_id = app_info_id()
    versions_dir = version_dir(version_string)
    print(f"версия {version_string} ({version_id}), appInfo {info_id}, тексты из {versions_dir.name}\n")

    have_info = existing(f"/v1/appInfos/{info_id}/appInfoLocalizations")
    st, builds = asc.req("GET", f"/v1/apps/{asc.APP_ID}/appStoreVersions?limit=200")
    first_release = len(builds.get("data", [])) <= 1
    if first_release:
        print("это первый релиз — «Что нового» не заполняется\n")

    for loc in locales:
        info_file = INFO_DIR / f"{loc}.json"
        ver_file = versions_dir / f"{loc}.json"
        if not info_file.exists() or not ver_file.exists():
            print(f"{loc}: нет файлов метаданных — пропускаю")
            continue
        info = json.loads(info_file.read_text())
        ver = json.loads(ver_file.read_text())

        act, st, err = upsert("appInfoLocalizations", "appInfo", info_id, have_info, loc, {
            "name": info["name"],
            "subtitle": info["subtitle"],
            "privacyPolicyUrl": info["privacyPolicyUrl"],
        })
        print(f"{loc}: имя/подзаголовок — {act} {st}" + (f"\n   {err}" if err else ""))

        # Список перечитывается КАЖДЫЙ раз: заводя локализацию имени, ASC сам
        # создаёт парную локализацию версии, и снимок, снятый до цикла, врёт.
        have_ver = existing(f"/v1/appStoreVersions/{version_id}/appStoreVersionLocalizations")

        attrs = {
            "description": ver["description"],
            "keywords": ver["keywords"],
            "promotionalText": ver["promotionalText"],
            "supportUrl": ver["supportUrl"],
        }
        # «Что нового» есть только у обновления: у первого релиза ASC отвечает
        # 409 «cannot be edited at this time» и режет весь запрос.
        if not first_release:
            attrs["whatsNew"] = ver["whatsNew"]

        act, st, err = upsert("appStoreVersionLocalizations", "appStoreVersion", version_id,
                              have_ver, loc, attrs)
        print(f"{loc}: описание/ключевые — {act} {st}" + (f"\n   {err}" if err else ""))


if __name__ == "__main__":
    main(sys.argv[1:] or ["en-US", "ru", "es-ES", "de-DE", "fr-FR"])
