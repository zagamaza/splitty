#!/usr/bin/env python3
"""Заливает в Google Play ТОЛЬКО картинки витрины: скриншоты и feature graphic.

Тексты существующих листингов не трогает — это делает push-play-listing.py.
Локаль, у которой в Play ещё нет листинга, пропускается; с --create-missing
листинг для неё создаётся из metadata/play/<loc>.json, уже заведённые остаются
как есть. Иконка 512 берётся из docs/design/receipt/icon/play-512.png.

    python3 marketing/push-play-images.py            # все языки
    python3 marketing/push-play-images.py ru-RU
    python3 marketing/push-play-images.py --create-missing

Читает metadata/screenshots-framed-android/<lang>/*.png и
metadata/feature-graphic/<loc>.png. Всё внутри одного edit: при любом сбое он
не коммитится, и витрина остаётся прежней.
"""
import json
import pathlib
import sys

import requests
from google.auth.transport.requests import Request
from google.oauth2 import service_account

ROOT = pathlib.Path(__file__).resolve().parent.parent
PKG = "com.zagir.splitty"
SA = ROOT / "android" / "play-sa.json"
BASE = f"https://androidpublisher.googleapis.com/androidpublisher/v3/applications/{PKG}"
UPLOAD = f"https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications/{PKG}"
ICON = ROOT / "docs" / "design" / "receipt" / "icon" / "play-512.png"

# Play-локаль → каталог кадров и файл графики. Своей графики у пяти локалей
# нет — берут английскую: пустой feature graphic Play не принимает.
LOCALES = {
    "en-US": ("en", "en-US"),
    "ru-RU": ("ru", "ru"),
    "es-ES": ("es", "es-ES"),
    "de-DE": ("de", "de-DE"),
    "fr-FR": ("fr", "fr-FR"),
    "ja-JP": ("ja", "en-US"),
    "zh-CN": ("zh-Hans", "en-US"),
    "ko-KR": ("ko", "en-US"),
    "pt-BR": ("pt-BR", "en-US"),
    "it-IT": ("it", "en-US"),
}


def token():
    creds = service_account.Credentials.from_service_account_file(
        str(SA), scopes=["https://www.googleapis.com/auth/androidpublisher"])
    creds.refresh(Request())
    return creds.token


def upload(head, url, path):
    r = requests.post(url, headers={**head, "Content-Type": "image/png"},
                      data=path.read_bytes(), params={"uploadType": "media"}, timeout=180)
    return r.status_code < 300, r


def create_listing(head, edit, loc):
    body = json.loads((ROOT / "metadata" / "play" / f"{loc}.json").read_text())
    r = requests.put(f"{BASE}/edits/{edit}/listings/{loc}", headers=head, timeout=60,
                     json={"language": loc, "title": body["title"],
                           "shortDescription": body["shortDescription"],
                           "fullDescription": body["fullDescription"]})
    return r.status_code < 300, r


def main(locales, create_missing):
    if not SA.exists():
        sys.exit(f"нет ключа сервис-аккаунта: {SA}")
    head = {"Authorization": f"Bearer {token()}"}
    r = requests.post(f"{BASE}/edits", headers=head, timeout=60)
    if r.status_code >= 300:
        sys.exit(f"не создался edit: {r.status_code} {r.text[:300]}")
    edit = r.json()["id"]
    print(f"edit {edit}")
    try:
        r = requests.get(f"{BASE}/edits/{edit}/listings", headers=head, timeout=60)
        existing = {l["language"] for l in r.json().get("listings", [])}
        failed = False
        for loc in locales:
            if loc not in existing:
                if not create_missing:
                    print(f"{loc}: листинга в Play нет — пропускаю (нужен --create-missing)")
                    continue
                ok, r = create_listing(head, edit, loc)
                failed |= not ok
                print(f"{loc}: листинг {'создан' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")
                if not ok:
                    continue
            if ICON.exists():
                requests.delete(f"{BASE}/edits/{edit}/listings/{loc}/icon", headers=head, timeout=60)
                ok, r = upload(head, f"{UPLOAD}/edits/{edit}/listings/{loc}/icon", ICON)
                failed |= not ok
                print(f"{loc}: иконка {'ок' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")
            shots_dir, graphic_name = LOCALES[loc]
            graphic = ROOT / "metadata" / "feature-graphic" / f"{graphic_name}.png"
            if graphic.exists():
                ok, r = upload(head, f"{UPLOAD}/edits/{edit}/listings/{loc}/featureGraphic", graphic)
                failed |= not ok
                print(f"{loc}: feature graphic {'ок' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")
            shots = sorted((ROOT / "metadata" / "screenshots-framed-android" / shots_dir).glob("*.png"))
            if not shots:
                print(f"{loc}: кадров нет — пропускаю")
                continue
            # Загрузка добавляет, а не заменяет: старые кадры сносим.
            requests.delete(f"{BASE}/edits/{edit}/listings/{loc}/phoneScreenshots",
                            headers=head, timeout=60)
            for shot in shots:
                ok, r = upload(head, f"{UPLOAD}/edits/{edit}/listings/{loc}/phoneScreenshots", shot)
                failed |= not ok
                print(f"   {shot.name}: {'ок' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")

        if failed:
            print("\nбыли сбои — edit НЕ коммичу, витрина осталась прежней")
            requests.delete(f"{BASE}/edits/{edit}", headers=head, timeout=60)
            sys.exit(1)
        r = requests.post(f"{BASE}/edits/{edit}:commit", headers=head, timeout=120)
        if r.status_code >= 300:
            sys.exit(f"коммит не прошёл: {r.status_code} {r.text[:400]}")
        print("\nкартинки витрины обновлены")
    except Exception:
        requests.delete(f"{BASE}/edits/{edit}", headers=head, timeout=60)
        raise


if __name__ == "__main__":
    args = sys.argv[1:]
    create = "--create-missing" in args
    args = [a for a in args if a != "--create-missing"]
    main(args or list(LOCALES), create)
