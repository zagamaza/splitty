#!/usr/bin/env python3
"""Заливает скриншот экрана оплаты в Review Information подписок.

Без него подписка висит в MISSING_METADATA и в заявку на ревью не берётся:
Apple требует показать, где именно в приложении покупают. Кадр один и тот же
для обоих продуктов — экран оплаты у них общий.

    python3 ios/asc/push_iap_review_shot.py metadata/iap/paywall-review.png
"""
import hashlib
import json
import pathlib
import sys
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import asc  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent.parent


def subscriptions():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/subscriptionGroups?limit=10")
    out = []
    for g in r.get("data", []):
        st, s = asc.req("GET", f"/v1/subscriptionGroups/{g['id']}/subscriptions?limit=20")
        out += [(d["id"], d["attributes"]["productId"], d["attributes"]["state"])
                for d in s.get("data", [])]
    return out


def drop_existing(sub_id):
    st, r = asc.req("GET", f"/v1/subscriptions/{sub_id}/appStoreReviewScreenshot")
    if st == 200 and r.get("data"):
        asc.req("DELETE", f"/v1/subscriptionAppStoreReviewScreenshots/{r['data']['id']}")


def upload(sub_id, path: pathlib.Path):
    blob = path.read_bytes()
    st, r = asc.req("POST", "/v1/subscriptionAppStoreReviewScreenshots", {"data": {
        "type": "subscriptionAppStoreReviewScreenshots",
        "attributes": {"fileName": path.name, "fileSize": len(blob)},
        "relationships": {"subscription": {
            "data": {"type": "subscriptions", "id": sub_id}}},
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

    st, r = asc.req("PATCH", f"/v1/subscriptionAppStoreReviewScreenshots/{shot_id}", {"data": {
        "type": "subscriptionAppStoreReviewScreenshots", "id": shot_id,
        "attributes": {"uploaded": True, "sourceFileChecksum": hashlib.md5(blob).hexdigest()},
    }})
    if st >= 300:
        return f"приём не подтверждён: {st} {json.dumps(r, ensure_ascii=False)[:300]}"
    return None


def main(argv):
    path = pathlib.Path(argv[0]) if argv else ROOT / "metadata" / "iap" / "paywall-review.png"
    if not path.exists():
        sys.exit(f"нет файла {path}")
    for sub_id, product, state in subscriptions():
        drop_existing(sub_id)
        err = upload(sub_id, path)
        print(f"{product} ({state}): {'ок' if err is None else 'СБОЙ — ' + err}")


if __name__ == "__main__":
    main(sys.argv[1:])
