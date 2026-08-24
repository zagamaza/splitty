#!/usr/bin/env python3
"""Собирает и отправляет заявку на ревью: версия + обе подписки.

Подписки — отдельные объекты ревью. Если положить в заявку только версию,
приложение уедет на проверку с продуктами, которых ревьюер не видит: экран
оплаты покажет «не удалось загрузить тарифы», и это отказ по 2.1.

    python3 ios/asc/submit_for_review.py            # показать, что уйдёт
    python3 ios/asc/submit_for_review.py --submit   # отправить
"""
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import asc  # noqa: E402

EDITABLE = ("PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED")


def die(msg):
    sys.exit(msg)


def editable_version():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/appStoreVersions?limit=10")
    for v in r.get("data", []):
        if v["attributes"].get("appStoreState") in EDITABLE:
            return v
    die("нет версии в редактируемом состоянии")


def ready_subscriptions():
    st, r = asc.req("GET", f"/v1/apps/{asc.APP_ID}/subscriptionGroups?limit=10")
    out = []
    for g in r.get("data", []):
        st, s = asc.req("GET", f"/v1/subscriptionGroups/{g['id']}/subscriptions?limit=20")
        for d in s.get("data", []):
            out.append((d["id"], d["attributes"]["productId"], d["attributes"]["state"]))
    return out


def open_submission():
    st, r = asc.req("GET", f"/v1/reviewSubmissions?filter[app]={asc.APP_ID}&limit=20")
    for d in r.get("data", []):
        if d["attributes"].get("state") in ("READY_FOR_REVIEW", "WAITING_FOR_REVIEW", "IN_REVIEW", "UNRESOLVED_ISSUES"):
            return d
    return None


def create_submission():
    st, r = asc.req("POST", "/v1/reviewSubmissions", {"data": {
        "type": "reviewSubmissions",
        "attributes": {"platform": "IOS"},
        "relationships": {"app": {"data": {"type": "apps", "id": asc.APP_ID}}},
    }})
    if st >= 300:
        die(f"заявка не создалась: {st} {json.dumps(r, ensure_ascii=False)[:400]}")
    return r["data"]


def add_item(submission_id, rel_type, rel_id, label):
    st, r = asc.req("POST", "/v1/reviewSubmissionItems", {"data": {
        "type": "reviewSubmissionItems",
        "relationships": {
            "reviewSubmission": {"data": {"type": "reviewSubmissions", "id": submission_id}},
            rel_type: {"data": {"type": rel_type + "s" if not rel_type.endswith("s") else rel_type, "id": rel_id}},
        },
    }})
    ok = st < 300
    print(f"   {label}: {'добавлено' if ok else 'СБОЙ ' + str(st) + ' ' + json.dumps(r, ensure_ascii=False)[:300]}")
    return ok


def items(submission_id):
    st, r = asc.req("GET", f"/v1/reviewSubmissions/{submission_id}/items?limit=50")
    return r.get("data", [])


def main(argv):
    submit = "--submit" in argv

    version = editable_version()
    va = version["attributes"]
    build = version["relationships"].get("build", {}).get("data")
    print(f"версия {va['versionString']} ({va['appStoreState']}), билд {build['id'] if build else 'НЕ ПРИВЯЗАН'}")
    if not build:
        die("к версии не привязан билд — ревью не примут")

    subs = ready_subscriptions()
    for sid, product, state in subs:
        print(f"подписка {product}: {state}")
    not_ready = [p for _, p, s in subs if s not in ("READY_TO_SUBMIT", "APPROVED", "DEVELOPER_REMOVED_FROM_SALE")]
    if not_ready:
        die(f"подписки не готовы: {', '.join(not_ready)}")

    if not submit:
        print("\nэто предпросмотр — для отправки добавь --submit")
        return

    sub = open_submission()
    if sub:
        print(f"заявка уже открыта: {sub['id']} ({sub['attributes']['state']})")
    else:
        sub = create_submission()
        print(f"заявка создана: {sub['id']}")

    have = {(i["relationships"].get("appStoreVersion", {}).get("data") or {}).get("id")
            for i in items(sub["id"])}
    have |= {(i["relationships"].get("appStoreVersionExperiment", {}).get("data") or {}).get("id")
             for i in items(sub["id"])}
    existing_subs = {(i["relationships"].get("appEvent", {}).get("data") or {}).get("id")
                     for i in items(sub["id"])}

    print("состав заявки:")
    if version["id"] not in have:
        add_item(sub["id"], "appStoreVersion", version["id"], f"версия {va['versionString']}")
    else:
        print(f"   версия {va['versionString']}: уже в заявке")
    for sid, product, _ in subs:
        add_item(sub["id"], "subscription", sid, product)

    st, r = asc.req("PATCH", f"/v1/reviewSubmissions/{sub['id']}", {"data": {
        "type": "reviewSubmissions", "id": sub["id"],
        "attributes": {"submitted": True},
    }})
    if st >= 300:
        die(f"отправка не прошла: {st} {json.dumps(r, ensure_ascii=False)[:600]}")
    print(f"\nотправлено: состояние {r['data']['attributes'].get('state')}")


if __name__ == "__main__":
    main(sys.argv[1:])
