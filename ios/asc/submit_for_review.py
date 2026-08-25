#!/usr/bin/env python3
"""Собирает заявку на ревью: версия и подписки.

Подписки — отдельные объекты ревью. Заявка с одной версией отправляет
приложение на проверку с продуктами, которых ревьюер не видит: экран оплаты
покажет «не удалось загрузить тарифы», и это отказ по 2.1.

⚠️ ПЕРВУЮ подписку приложения публичный API приложить не может. У
`reviewSubmissionItems` нет связи `subscription` (как и `inAppPurchaseV2`), а
`/v1/subscriptionSubmissions` на неё отвечает
FIRST_SUBSCRIPTION_MUST_BE_SUBMITTED_ON_VERSION — даже когда заявка с версией
уже создана и ещё не отправлена. Галочка «отправить вместе с версией» живёт
только в веб-консоли. Поэтому скрипт доводит заявку до READY_FOR_REVIEW и
останавливается: дожать первую отправку надо руками в App Store Connect.
Со второй и дальше подписки уходят уже отсюда.

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


def add_version(submission_id, version_id, label):
    st, r = asc.req("POST", "/v1/reviewSubmissionItems", {"data": {
        "type": "reviewSubmissionItems",
        "relationships": {
            "reviewSubmission": {"data": {"type": "reviewSubmissions", "id": submission_id}},
            "appStoreVersion": {"data": {"type": "appStoreVersions", "id": version_id}},
        },
    }})
    if st >= 300:
        die(f"версия {label} не добавилась: {st} {json.dumps(r, ensure_ascii=False)[:400]}")
    print(f"   версия {label}: добавлена")


def submit_subscription(sub_id, product):
    """Подписки идут не пунктом заявки, а собственным ресурсом.

    `reviewSubmissionItems` их не принимает: 'subscription' там не связь. Первую
    подписку приложения Apple требует отправлять ОДНОВРЕМЕННО с версией — то
    есть уже после того, как заявка с версией создана, но ещё до `submitted`.
    """
    st, r = asc.req("POST", "/v1/subscriptionSubmissions", {"data": {
        "type": "subscriptionSubmissions",
        "relationships": {"subscription": {"data": {"type": "subscriptions", "id": sub_id}}},
    }})
    if st >= 300:
        die(f"подписка {product} не отправлена: {st} {json.dumps(r, ensure_ascii=False)[:600]}")
    print(f"   подписка {product}: отправлена")


def items(submission_id):
    st, r = asc.req("GET", f"/v1/reviewSubmissions/{submission_id}/items?limit=50")
    return r.get("data", [])


def main(argv):
    submit = "--submit" in argv

    version = editable_version()
    va = version["attributes"]
    # Список версий приходит без данных связи — билд спрашиваем отдельно,
    # иначе «не привязан» показывается на привязанном билде.
    st, b = asc.req("GET", f"/v1/appStoreVersions/{version['id']}/build")
    build = b.get("data") if st == 200 else None
    label = f"{build['attributes']['version']} ({build['id']})" if build else "НЕ ПРИВЯЗАН"
    print(f"версия {va['versionString']} ({va['appStoreState']}), билд {label}")
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

    print("состав заявки:")
    if not items(sub["id"]):
        add_version(sub["id"], version["id"], va["versionString"])
    else:
        print(f"   версия {va['versionString']}: уже в заявке")
    for sid, product, state in subs:
        if state == "READY_TO_SUBMIT":
            submit_subscription(sid, product)
        else:
            print(f"   подписка {product}: {state}, отправлять не надо")

    st, r = asc.req("PATCH", f"/v1/reviewSubmissions/{sub['id']}", {"data": {
        "type": "reviewSubmissions", "id": sub["id"],
        "attributes": {"submitted": True},
    }})
    if st >= 300:
        die(f"отправка не прошла: {st} {json.dumps(r, ensure_ascii=False)[:600]}")
    print(f"\nотправлено: состояние {r['data']['attributes'].get('state')}")


if __name__ == "__main__":
    main(sys.argv[1:])
