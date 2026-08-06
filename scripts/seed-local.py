#!/usr/bin/env python3
"""Демо-данные локального бэкенда для UI-прогонов iOS и Android.

Создаёт то, что тесты ищут по имени: группу «Поездка в Стамбул» с расходом
«Ужин в ресторане», вторую группу и друга «Алмаз». Протагонист — аккаунт с
email и паролем (UI_EMAIL/UI_PASSWORD): приложение умеет входить только
четырьмя способами, и единственный из них, доступный тесту без внешних
сервисов, — email + пароль.

Остальные участники заводятся через POST /auth/dev: у них нет ни почты, ни
Telegram, и заходить под ними никто не должен — они нужны как вторая и третья
сторона расходов.

Требует бэкенд на BASE (по умолчанию http://127.0.0.1:7171) с API_DEV_AUTH=true.
Повторный запуск ничего не дублирует: существующие группы находятся по имени.

    python3 scripts/seed-local.py
"""
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("SPLITTY_BASE_URL", "http://127.0.0.1:7171") + "/api/v1"

# Учётка протагониста. Те же значения зашиты в UITestHelpers.swift —
# меняете здесь, меняйте и там, иначе прогон не войдёт.
#
# Пароль из ОДНИХ ЦИФР намеренно: UI-тест набирает его экранной клавиатурой, а
# та берёт активную раскладку симулятора. На русской раскладке латиница
# приезжает кириллицей («splittytest» → «ыздшееуые»), сервер отвечает 401, и
# падение выглядит как «неверная учётка». Цифры одинаковы в любой раскладке.
UI_EMAIL = os.environ.get("SEED_UI_EMAIL", "ui-tests@splitty.test")
UI_PASSWORD = os.environ.get("SEED_UI_PASSWORD", "20260806")
UI_NAME = "Загир"

# Соучастники: id намеренно маленькие и постоянные, чтобы повторный запуск
# попадал в тех же людей, а не плодил новых.
ALMAZ = (101, "Алмаз", "almaz")
RUSTAM = (102, "Рустам", "rustam")


def call(method, path, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
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


def ui_account():
    """Регистрирует протагониста или входит, если он уже заведён."""
    st, res = call("POST", "/auth/register",
                   body={"email": UI_EMAIL, "password": UI_PASSWORD, "displayName": UI_NAME})
    if st in (200, 201):
        return res["token"], res["user"]["id"], "зарегистрирован"
    if st == 409:
        st, res = call("POST", "/auth/login", body={"email": UI_EMAIL, "password": UI_PASSWORD})
        if st != 200:
            sys.exit(
                f"{UI_EMAIL} уже занят, но пароль не подходит ({st}). "
                "Смените SEED_UI_PASSWORD или почистите базу."
            )
        return res["token"], res["user"]["id"], "уже был"
    sys.exit(f"регистрация {UI_EMAIL} не удалась: {st} {res}")


def find_room(token, name):
    st, rooms = call("GET", "/rooms", token)
    if st != 200:
        sys.exit(f"не удалось прочитать список групп: {st} {rooms}")
    return next((r for r in rooms if r["name"] == name), None)


def seed_room(spec, tokens, ids):
    owner_token = tokens[spec["owner"]]
    # Ищем СВОИМ токеном, а не владельца: протагонист состоит в каждой
    # сеемой группе, и проверка от владельца находила бы одноимённые группы,
    # в которых протагониста нет, — тесты потом не видели бы ничего.
    existing = find_room(tokens["ui"], spec["name"])
    if existing:
        print(f"  группа «{spec['name']}» уже есть ({existing['id']}) — пропускаю")
        return

    st, room = call("POST", "/rooms", owner_token, {"name": spec["name"]})
    if st != 201:
        sys.exit(f"создание «{spec['name']}» не удалось: {st} {room}")
    rid = room["id"]
    print(f"  группа «{spec['name']}» → {rid}")

    for who in spec["members"]:
        st, res = call("POST", f"/rooms/{rid}/join", tokens[who])
        if st not in (200, 201):
            sys.exit(f"join в «{spec['name']}» не удался: {st} {res}")

    for desc, total, donor, recipients in spec["ops"]:
        st, op = call("POST", f"/rooms/{rid}/operations", tokens[donor], {
            "description": desc,
            "sum": total,
            "donorId": ids[donor],
            "recipientIds": [ids[r] for r in recipients],
            "splitType": "equally",
        })
        if st not in (200, 201):
            sys.exit(f"расход «{desc}» не создался: {st} {op}")
        print(f"    расход «{desc}»: {total}")


def main():
    ui_token, ui_id, how = ui_account()
    almaz_token, almaz_id = dev_login(*ALMAZ)
    rustam_token, rustam_id = dev_login(*RUSTAM)
    print(f"протагонист {UI_EMAIL} (id {ui_id}) — {how}")

    tokens = {"ui": ui_token, "almaz": almaz_token, "rustam": rustam_token}
    ids = {"ui": ui_id, "almaz": almaz_id, "rustam": rustam_id}

    rooms = [
        {
            # Ровно это имя ищут DemoFlowUITests, DashboardShotsUITests и
            # OfflineSmokeUITests — переименование ломает все три.
            "name": "Поездка в Стамбул",
            "owner": "almaz",
            "members": ["ui", "rustam"],
            "ops": [
                # «Ужин в ресторане» открывает DemoFlowUITests как карточку операции.
                ("Ужин в ресторане", 4500, "almaz", ["ui", "almaz", "rustam"]),
                ("Такси из аэропорта", 1800, "rustam", ["ui", "almaz", "rustam"]),
                ("Отель на три ночи", 30000, "almaz", ["ui", "almaz", "rustam"]),
                ("Билеты в Айя-Софию", 2400, "ui", ["ui", "almaz", "rustam"]),
            ],
        },
        {
            "name": "Квартира на Тверской",
            "owner": "ui",
            "members": ["almaz"],
            "ops": [
                ("Коммуналка за июль", 8400, "ui", ["ui", "almaz"]),
                ("Интернет", 900, "almaz", ["ui", "almaz"]),
            ],
        },
    ]
    for spec in rooms:
        seed_room(spec, tokens, ids)

    st, debts = call("GET", "/rooms/" + find_room(ui_token, "Поездка в Стамбул")["id"] + "/debts", ui_token)
    mine = [d for d in (debts or []) if d["debtor"]["id"] == ui_id]
    # DemoFlowUITests жмёт «Погасить» — без долга протагониста шаг молча пропадает.
    print("долг протагониста:", mine[0]["sum"] if mine else "нет (кнопка «Погасить» будет неактивна)")
    print(f"\nготово. Вход для прогонов: {UI_EMAIL} / {UI_PASSWORD}")


if __name__ == "__main__":
    main()
