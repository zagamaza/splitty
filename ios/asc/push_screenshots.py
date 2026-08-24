#!/usr/bin/env python3
"""Заливает оформленные скриншоты в App Store Connect.

Загрузка картинки в ASC — три шага: зарезервировать место (POST даёт список
`uploadOperations` с адресами и кусками), залить куски и подтвердить приём
хешем MD5. Ручной аналог — перетаскивать по семь файлов на каждый язык.

    python3 ios/asc/push_screenshots.py            # все языки
    python3 ios/asc/push_screenshots.py ru

Берёт metadata/screenshots-framed/<lang>/*.png (1320×2868).
"""
import hashlib
import json
import pathlib
import sys
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import asc  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent.parent
SHOTS = ROOT / "metadata" / "screenshots-framed"

# 6.9" и 6.7" в ASC — один и тот же набор APP_IPHONE_67; отдельного типа под
# 6.9 нет, туда идут кадры 1320×2868.
DISPLAY_TYPE = "APP_IPHONE_67"

# Каталог витрины и каталог скриншотов называют локали по-разному.
LOCALE_DIR = {"en-US": "en", "ru": "ru", "es-ES": "es", "de-DE": "de", "fr-FR": "fr"}


def editable_version():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/appStoreVersions?limit=10")
    for v in r.get("data", []):
        if v["attributes"].get("appStoreState") in (
            "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED",
        ):
            return v["id"]
    sys.exit("нет версии в редактируемом состоянии")


def version_localizations(version_id):
    st, r = asc.req("GET", f"/v1/appStoreVersions/{version_id}/appStoreVersionLocalizations")
    return {x["attributes"]["locale"]: x["id"] for x in r.get("data", [])}


def screenshot_set(loc_id):
    """Возвращает набор кадров нужного типа, при необходимости заводит и чистит."""
    st, r = asc.req("GET", f"/v1/appStoreVersionLocalizations/{loc_id}/appScreenshotSets")
    for s in r.get("data", []):
        if s["attributes"].get("screenshotDisplayType") == DISPLAY_TYPE:
            # Старые кадры удаляем: иначе новые лягут ХВОСТОМ к прежним и
            # витрина покажет вперемешку два поколения картинок.
            st, old = asc.req("GET", f"/v1/appScreenshotSets/{s['id']}/appScreenshots")
            for shot in old.get("data", []):
                asc.req("DELETE", f"/v1/appScreenshots/{shot['id']}")
            return s["id"]
    st, r = asc.req("POST", "/v1/appScreenshotSets", {"data": {
        "type": "appScreenshotSets",
        "attributes": {"screenshotDisplayType": DISPLAY_TYPE},
        "relationships": {"appStoreVersionLocalization": {
            "data": {"type": "appStoreVersionLocalizations", "id": loc_id}}},
    }})
    if st >= 300:
        sys.exit(f"не создался набор кадров: {st} {json.dumps(r, ensure_ascii=False)[:300]}")
    return r["data"]["id"]


def upload(set_id, path: pathlib.Path):
    blob = path.read_bytes()
    st, r = asc.req("POST", "/v1/appScreenshots", {"data": {
        "type": "appScreenshots",
        "attributes": {"fileName": path.name, "fileSize": len(blob)},
        "relationships": {"appScreenshotSet": {
            "data": {"type": "appScreenshotSets", "id": set_id}}},
    }})
    if st >= 300:
        return f"резервирование не прошло: {st} {json.dumps(r, ensure_ascii=False)[:300]}"
    shot_id = r["data"]["id"]

    for op in r["data"]["attributes"]["uploadOperations"]:
        chunk = blob[op["offset"]: op["offset"] + op["length"]]
        req = urllib.request.Request(op["url"], data=chunk, method=op["method"])
        for h in op.get("requestHeaders", []):
            req.add_header(h["name"], h["value"])
        with urllib.request.urlopen(req, timeout=300) as resp:
            if resp.status >= 300:
                return f"кусок не залился: {resp.status}"

    st, r = asc.req("PATCH", f"/v1/appScreenshots/{shot_id}", {"data": {
        "type": "appScreenshots", "id": shot_id,
        "attributes": {"uploaded": True, "sourceFileChecksum": hashlib.md5(blob).hexdigest()},
    }})
    if st >= 300:
        return f"приём не подтверждён: {st} {json.dumps(r, ensure_ascii=False)[:300]}"
    return None


def main(locales):
    version_id = editable_version()
    locs = version_localizations(version_id)
    for loc in locales:
        if loc not in locs:
            print(f"{loc}: нет локализации версии — пропускаю")
            continue
        folder = SHOTS / LOCALE_DIR.get(loc, loc)
        files = sorted(folder.glob("*.png"))
        if not files:
            print(f"{loc}: нет кадров в {folder}")
            continue
        set_id = screenshot_set(locs[loc])
        print(f"{loc}: набор {set_id}, файлов {len(files)}")
        for f in files:
            err = upload(set_id, f)
            print(f"   {f.name}: {'ок' if err is None else 'СБОЙ — ' + err}")


if __name__ == "__main__":
    main(sys.argv[1:] or list(LOCALE_DIR))
