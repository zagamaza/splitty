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
# Расход, РАЗОБРАННЫЙ ПО ПОЗИЦИЯМ. Ради него всё и затевалось: витрине надо
# показать не «ужин 16 400 ₽», а что распознавание вытащило из чека каждую
# строку и разложило её по тем, кто это ел. Обычные ops такого не дают —
# у них только общая сумма и деление поровну.
#
# Формат позиции: (название, сумма строки, количество, кто делит).
# "all" — все участники комнаты.
ITEMIZED = {
    "ru": {
        "room": "Поездка в Стамбул",
        "description": "Ужин в Кадыкёе",
        "donor": "me",
        "items": [
            ("Дорада на гриле", 3200, 1, ["me", 0]),
            ("Мидии по-измирски", 1450, 1, [0, 1]),
            ("Мезе ассорти", 1800, 1, "all"),
            ("Ракы", 2400, 2, ["me", 1]),
            ("Чай и лукум", 620, 4, "all"),
        ],
        "surcharge": ("Сервисный сбор", 947, 10),
    },
    "en": {
        "room": "Trip to Lisbon",
        "description": "Dinner in Alfama",
        "donor": "me",
        "items": [
            ("Grilled sea bass", 32, 1, ["me", 0]),
            ("Clams à Bulhão Pato", 16, 1, [0, 1]),
            ("Petiscos platter", 20, 1, "all"),
            ("Vinho verde", 24, 2, ["me", 1]),
            ("Pastéis de nata", 8, 4, "all"),
        ],
        "surcharge": ("Service charge", 10, 10),
    },
}

# Валюта комнат по языку витрины. Скриншоты для американского App Store с
# рублёвыми ценами выглядят так, будто приложение не для этого рынка.
CURRENCY = {"ru": "RUB", "en": "EUR"}

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
                    ("Apartment, three nights", 410, "me", "all"),
                    ("Dinner at Ramiro", 84, 0, "all"),
                    ("Airport taxi", 26, 1, "all"),
                    ("Tram 28 day passes", 32, "me", "all"),
                    ("Pastéis de Belém", 12, 2, "all"),
                    ("Ferry to Cacilhas", 7, 0, "all"),
                    ("Time Out Market lunch", 59, "me", "all"),
                    ("Fado night", 68, 1, "all"),
                ],
            },
            {
                "name": "Flat share",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("August utilities", 96, "me", "all"),
                    ("Internet", 29, 0, "all"),
                    ("Weekly groceries", 62, "me", "all"),
                    ("Cleaning", 40, 0, "all"),
                ],
            },
            {
                "name": "Weekend cabin",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Firewood and food", 48, 2, "all"),
                    ("Gas", 34, "me", "all"),
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


def seed_itemized(lang, room_name, rid, tokens, ids, everyone, call):
    """Создаёт расход, разобранный по позициям чека.

    Такой расход выглядит в приложении не строкой с суммой, а чеком: каждая
    позиция со своей ценой и своим набором едоков. Ради этого кадра витрины он
    и нужен — обычный расход показать распознавание не может.

    Сумма НЕ задаётся: сервер считает её из позиций сам, и подгонять её руками
    значило бы однажды разойтись с ним на рубль.
    """
    spec = ITEMIZED.get(lang)
    if not spec or spec["room"] != room_name:
        return

    def resolve(who):
        people = everyone if who == "all" else who
        return [ids[p] for p in people]

    items = []
    for name, price, qty, who in spec["items"]:
        items.append({
            "name": name,
            "price": price,
            "qty": qty,
            "kind": "item",
            "shares": [{"userId": uid, "weight": 1} for uid in resolve(who)],
        })
    if spec.get("surcharge"):
        name, price, percent = spec["surcharge"]
        items.append({
            "name": name,
            "price": price,
            "qty": 1,
            "kind": "surcharge",
            "split": "proportional",
            "percent": percent,
        })

    total = sum(i["price"] for i in items)
    st, op = call("POST", f"/rooms/{rid}/operations", tokens[spec["donor"]], {
        "description": spec["description"],
        "sum": total,
        "donorId": ids[spec["donor"]],
        "recipientIds": [ids[p] for p in everyone],
        "splitType": "equally",
        "items": items,
    })
    if st not in (200, 201):
        sys.exit(f"расход-чек «{spec['description']}»: {st} {op}")
    print(f"    чек «{spec['description']}»: позиций {len(items)}, сумма {total}")


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
        want = CURRENCY.get(lang)
        if want:
            st, res = call("PUT", f"/rooms/{rid}/currency", tokens[room["owner"]], {"currency": want})
            if st not in (200, 204):
                sys.exit(f"валюта «{room['name']}» → {want}: {st} {res}")
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
        seed_itemized(lang, room["name"], rid, tokens, ids, everyone, call)
        print(f"  «{room['name']}» → {rid}, расходов: {len(room['ops'])}")

    print(f"[{lang}] вход: {spec['email']} / {PASSWORD}\n")


if __name__ == "__main__":
    langs = sys.argv[1:] or list(DATA)
    for lang in langs:
        if lang not in DATA:
            sys.exit(f"нет набора для «{lang}»; есть: {', '.join(DATA)}")
        seed(lang)
