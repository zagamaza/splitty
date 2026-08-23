#!/usr/bin/env python3
"""Демо-данные для магазинных скриншотов: по набору на каждый язык витрины.

От scripts/seed-local.py отличается целью. Тот сеет ровно то, что ищут
UI-тесты по именам, и трогать его нельзя. Здесь нужна другая история: группы
и расходы, которые не стыдно показать в App Store, с суммами и датами,
разложенными так, чтобы графики «Итоги» выглядели живыми, а не пустыми.

Отдельный аккаунт на язык: имя владельца, названия групп и расходов видны на
скриншоте, и англоязычная витрина с «Поездкой в Стамбул» выглядит халтурой.

Даты операций сервер ставит сам (api.Operation.CreateAt). Разложить их по
последнему месяцу можно только записью в базу — этим занимается
scripts/backdate-shots.js, запускаемый следом.

    python3 scripts/seed-store-shots.py            # все языки
    python3 scripts/seed-store-shots.py ru         # только один
"""
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("SPLITTY_BASE_URL", "http://127.0.0.1:7171") + "/api/v1"
PASSWORD = os.environ.get("SHOTS_PASSWORD", "20260806")

# Каждому языку — свой протагонист и свои соседи. id соседей постоянные:
# повторный прогон попадает в тех же людей, а не плодит новых.
DATA = {
    "ru": {
        "email": "shots-ru@splitty.test",
        "me": "Артём",
        "peers": [(9101, "Маша", "masha"), (9102, "Кирилл", "kirill"), (9103, "Даша", "dasha")],
        "rooms": [
            {
                "name": "Поездка в Стамбул",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Отель на три ночи", 32400, "me", "all"),
                    ("Ужин в Балык Экмек", 4600, 0, "all"),
                    ("Такси из аэропорта", 2100, 1, "all"),
                    ("Билеты в Айя-Софию", 3600, "me", "all"),
                    ("Кофе на Истикляль", 980, 2, "all"),
                    ("Паром на азиатский берег", 640, 0, "all"),
                    ("Рынок специй", 2750, "me", "all"),
                    ("Ужин с видом на Босфор", 7300, 1, "all"),
                ],
            },
            {
                "name": "Квартира на Тверской",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Коммуналка за август", 8900, "me", "all"),
                    ("Интернет", 900, 0, "all"),
                    ("Продукты на неделю", 5400, "me", "all"),
                    ("Клининг", 3500, 0, "all"),
                ],
            },
            {
                "name": "Дача на выходные",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Шашлык и уголь", 4200, 2, "all"),
                    ("Бензин", 3100, "me", "all"),
                ],
            },
        ],
    },
    "en": {
        "email": "shots-en@splitty.test",
        "me": "Alex",
        "peers": [(9201, "Maya", "maya"), (9202, "Chris", "chris"), (9203, "Dana", "dana")],
        "rooms": [
            {
                "name": "Trip to Lisbon",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Apartment, three nights", 41000, "me", "all"),
                    ("Dinner at Ramiro", 8400, 0, "all"),
                    ("Airport taxi", 2600, 1, "all"),
                    ("Tram 28 day passes", 3200, "me", "all"),
                    ("Pastéis de Belém", 1150, 2, "all"),
                    ("Ferry to Cacilhas", 720, 0, "all"),
                    ("Time Out Market lunch", 5900, "me", "all"),
                    ("Fado night", 6800, 1, "all"),
                ],
            },
            {
                "name": "Flat share",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("August utilities", 9600, "me", "all"),
                    ("Internet", 1100, 0, "all"),
                    ("Weekly groceries", 6200, "me", "all"),
                    ("Cleaning", 4000, 0, "all"),
                ],
            },
            {
                "name": "Weekend cabin",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Firewood and food", 4800, 2, "all"),
                    ("Gas", 3400, "me", "all"),
                ],
            },
        ],
    },
}


def call(method, path, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw.decode(errors="replace")
    except urllib.error.URLError as e:
        sys.exit(f"бэкенд на {BASE} недоступен: {e.reason}")


def dev_login(uid, name, username):
    st, res = call("POST", "/auth/dev", body={"userId": uid, "displayName": name, "username": username})
    if st == 404:
        sys.exit("POST /auth/dev отвечает 404 — бэкенд поднят без API_DEV_AUTH=true")
    if st != 200:
        sys.exit(f"dev-вход под {uid} не удался: {st} {res}")
    return res["token"], res["user"]["id"]


def account(email, name):
    st, res = call("POST", "/auth/register", body={"email": email, "password": PASSWORD, "displayName": name})
    if st in (200, 201):
        return res["token"], res["user"]["id"], "зарегистрирован"
    if st == 409:
        st, res = call("POST", "/auth/login", body={"email": email, "password": PASSWORD})
        if st != 200:
            sys.exit(f"{email} занят, но пароль не подходит ({st})")
        return res["token"], res["user"]["id"], "уже был"
    sys.exit(f"регистрация {email} не удалась: {st} {res}")


def seed(lang):
    spec = DATA[lang]
    me_token, me_id, how = account(spec["email"], spec["me"])
    print(f"[{lang}] {spec['email']} (id {me_id}) — {how}")

    tokens = {"me": me_token}
    ids = {"me": me_id}
    for i, peer in enumerate(spec["peers"]):
        t, uid = dev_login(*peer)
        tokens[i] = t
        ids[i] = uid

    st, existing = call("GET", "/rooms", me_token)
    have = {r["name"] for r in (existing or [])}

    for room in spec["rooms"]:
        if room["name"] in have:
            print(f"  «{room['name']}» уже есть — пропускаю")
            continue
        st, created = call("POST", "/rooms", tokens[room["owner"]], {"name": room["name"]})
        if st != 201:
            sys.exit(f"создание «{room['name']}»: {st} {created}")
        rid = created["id"]
        for who in room["members"]:
            st, res = call("POST", f"/rooms/{rid}/join", tokens[who])
            if st not in (200, 201):
                sys.exit(f"join в «{room['name']}»: {st} {res}")
        everyone = [room["owner"]] + list(room["members"])
        for desc, total, donor, who in room["ops"]:
            recipients = everyone if who == "all" else who
            st, op = call("POST", f"/rooms/{rid}/operations", tokens[donor], {
                "description": desc,
                "sum": total,
                "donorId": ids[donor],
                "recipientIds": [ids[r] for r in recipients],
                "splitType": "equally",
            })
            if st not in (200, 201):
                sys.exit(f"расход «{desc}»: {st} {op}")
        print(f"  «{room['name']}» → {rid}, расходов: {len(room['ops'])}")

    print(f"[{lang}] вход: {spec['email']} / {PASSWORD}\n")


if __name__ == "__main__":
    langs = sys.argv[1:] or list(DATA)
    for lang in langs:
        if lang not in DATA:
            sys.exit(f"нет набора для «{lang}»; есть: {', '.join(DATA)}")
        seed(lang)
