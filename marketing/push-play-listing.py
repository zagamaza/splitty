#!/usr/bin/env python3
"""Заливает витрину Google Play: тексты, feature graphic и скриншоты.

Play-листинг правится внутри «edit»: создаём черновик, вносим всё и
коммитим. Незакоммиченный edit ничего не меняет — при сбое на середине
достаточно не коммитить.

    python3 marketing/push-play-listing.py            # все языки
    python3 marketing/push-play-listing.py ru-RU

Читает metadata/play/<loc>.json, metadata/feature-graphic/<asc-loc>.png и
metadata/screenshots-framed-android/<lang>/*.png
"""
import json
import pathlib
import sys

import requests
from google.oauth2 import service_account
from google.auth.transport.requests import Request

ROOT = pathlib.Path(__file__).resolve().parent.parent
PKG = "com.zagir.splitty"
SA = ROOT / "android" / "play-sa.json"
BASE = f"https://androidpublisher.googleapis.com/androidpublisher/v3/applications/{PKG}"
UPLOAD = f"https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications/{PKG}"

# Play-локали ↔ имена файлов графики и каталогов скриншотов.
LOCALES = {
    "en-US": {"graphic": "en-US", "shots": "en"},
    "ru-RU": {"graphic": "ru",    "shots": "ru"},
    "es-ES": {"graphic": "es-ES", "shots": None},
    "de-DE": {"graphic": "de-DE", "shots": None},
    "fr-FR": {"graphic": "fr-FR", "shots": None},
    # Пять новых локалей переиспользуют английскую графику: своих картинок для
    # них нет, а пустой feature graphic Play не принимает вовсе.
    "ja-JP": {"graphic": "en-US", "shots": None},
    "zh-CN": {"graphic": "en-US", "shots": None},
    "ko-KR": {"graphic": "en-US", "shots": None},
    "pt-BR": {"graphic": "en-US", "shots": None},
    "it-IT": {"graphic": "en-US", "shots": None},
}


def token():
    creds = service_account.Credentials.from_service_account_file(
        str(SA), scopes=["https://www.googleapis.com/auth/androidpublisher"])
    creds.refresh(Request())
    return creds.token


def main(locales):
    if not SA.exists():
        sys.exit(f"нет ключа сервис-аккаунта: {SA}")
    head = {"Authorization": f"Bearer {token()}"}

    r = requests.post(f"{BASE}/edits", headers=head, timeout=60)
    if r.status_code >= 300:
        sys.exit(f"не создался edit: {r.status_code} {r.text[:300]}")
    edit = r.json()["id"]
    print(f"edit {edit}")

    failed = False
    try:
        for loc in locales:
            cfg = LOCALES[loc]
            body = json.loads((ROOT / "metadata" / "play" / f"{loc}.json").read_text())
            r = requests.put(
                f"{BASE}/edits/{edit}/listings/{loc}", headers=head, timeout=60,
                json={"language": loc, "title": body["title"],
                      "shortDescription": body["shortDescription"],
                      "fullDescription": body["fullDescription"]})
            ok = r.status_code < 300
            failed |= not ok
            print(f"{loc}: тексты {'ок' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")
            if not ok:
                continue

            graphic = ROOT / "metadata" / "feature-graphic" / f"{cfg['graphic']}.png"
            if graphic.exists():
                r = requests.post(
                    f"{UPLOAD}/edits/{edit}/listings/{loc}/featureGraphic",
                    headers={**head, "Content-Type": "image/png"},
                    data=graphic.read_bytes(), params={"uploadType": "media"}, timeout=180)
                print(f"{loc}: feature graphic {'ок' if r.status_code < 300 else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")
                failed |= r.status_code >= 300

            if cfg["shots"]:
                folder = ROOT / "metadata" / "screenshots-framed-android" / cfg["shots"]
                shots = sorted(folder.glob("*.png"))
                # Старые кадры сносим: загрузка добавляет, а не заменяет.
                requests.delete(f"{BASE}/edits/{edit}/listings/{loc}/phoneScreenshots",
                                headers=head, timeout=60)
                for shot in shots:
                    r = requests.post(
                        f"{UPLOAD}/edits/{edit}/listings/{loc}/phoneScreenshots",
                        headers={**head, "Content-Type": "image/png"},
                        data=shot.read_bytes(), params={"uploadType": "media"}, timeout=180)
                    ok = r.status_code < 300
                    failed |= not ok
                    print(f"   {shot.name}: {'ок' if ok else 'СБОЙ ' + str(r.status_code) + ' ' + r.text[:200]}")

        if failed:
            print("\nбыли сбои — edit НЕ коммичу, витрина осталась прежней")
            requests.delete(f"{BASE}/edits/{edit}", headers=head, timeout=60)
            sys.exit(1)

        r = requests.post(f"{BASE}/edits/{edit}:commit", headers=head, timeout=120)
        if r.status_code >= 300:
            sys.exit(f"коммит не прошёл: {r.status_code} {r.text[:400]}")
        print("\nвитрина обновлена")
    except Exception:
        requests.delete(f"{BASE}/edits/{edit}", headers=head, timeout=60)
        raise


if __name__ == "__main__":
    main(sys.argv[1:] or list(LOCALES))
