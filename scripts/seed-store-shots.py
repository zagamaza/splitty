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
    "es": {
        "room": "Viaje a Barcelona",
        "description": "Cena en el Born",
        "donor": "me",
        "items": [
            ("Pulpo a la gallega", 24, 1, ["me", 0]),
            ("Pan con tomate", 9, 1, [0, 1]),
            ("Tabla de ibéricos", 22, 1, "all"),
            ("Vermut de la casa", 18, 2, ["me", 1]),
            ("Crema catalana", 12, 4, "all"),
        ],
        "surcharge": ("Servicio", 9, 10),
    },
    "de": {
        "room": "Städtetrip Berlin",
        "description": "Abendessen in Kreuzberg",
        "donor": "me",
        "items": [
            ("Wiener Schnitzel", 26, 1, ["me", 0]),
            ("Currywurst mit Pommes", 12, 1, [0, 1]),
            ("Vorspeisenplatte", 19, 1, "all"),
            ("Weißbier", 21, 3, ["me", 1]),
            ("Kaffee und Kuchen", 10, 4, "all"),
        ],
        "surcharge": ("Trinkgeld", 9, 10),
    },
    "fr": {
        "room": "Week-end à Lyon",
        "description": "Dîner à la Croix-Rousse",
        "donor": "me",
        "items": [
            ("Quenelle de brochet", 23, 1, ["me", 0]),
            ("Salade lyonnaise", 14, 1, [0, 1]),
            ("Planche de charcuterie", 21, 1, "all"),
            ("Côtes-du-Rhône", 26, 2, ["me", 1]),
            ("Café gourmand", 12, 4, "all"),
        ],
        "surcharge": ("Service", 10, 10),
    },
    "ja": {
        "room": "京都旅行",
        "description": "先斗町の夕食",
        "donor": "me",
        "items": [
            ("焼き魚の定食", 5000, 1, ["me", 0]),
            ("だし巻き卵", 2500, 1, [0, 1]),
            ("おばんざい盛り合わせ", 3100, 1, "all"),
            ("日本酒", 3700, 2, ["me", 1]),
            ("抹茶わらび餅", 1200, 4, "all"),
        ],
        "surcharge": ("サービス料", 1600, 10),
    },
    "zh-Hans": {
        "room": "成都之行",
        "description": "宽窄巷子晚餐",
        "donor": "me",
        "items": [
            ("烤鱼", 220, 1, ["me", 0]),
            ("口水鸡", 110, 1, [0, 1]),
            ("凉菜拼盘", 140, 1, "all"),
            ("精酿啤酒", 170, 2, ["me", 1]),
            ("红糖糍粑", 60, 4, "all"),
        ],
        "surcharge": ("服务费", 70, 10),
    },
    "ko": {
        "room": "제주도 여행",
        "description": "흑돼지 저녁",
        "donor": "me",
        "items": [
            ("흑돼지 구이", 45000, 1, ["me", 0]),
            ("전복 뚝배기", 22000, 1, [0, 1]),
            ("모둠 밑반찬", 28000, 1, "all"),
            ("한라산 소주", 34000, 2, ["me", 1]),
            ("한라봉 셔벗", 11000, 4, "all"),
        ],
        "surcharge": ("봉사료", 14000, 10),
    },
    "pt-BR": {
        "room": "Viagem a Salvador",
        "description": "Jantar no Pelourinho",
        "donor": "me",
        "items": [
            ("Moqueca de peixe", 160, 1, ["me", 0]),
            ("Bobó de camarão", 80, 1, [0, 1]),
            ("Petiscos variados", 100, 1, "all"),
            ("Caipirinha", 120, 2, ["me", 1]),
            ("Cocada", 40, 4, "all"),
        ],
        "surcharge": ("Serviço", 50, 10),
    },
    "it": {
        "room": "Viaggio a Napoli",
        "description": "Cena a Spaccanapoli",
        "donor": "me",
        "items": [
            ("Pesce alla griglia", 32, 1, ["me", 0]),
            ("Impepata di cozze", 16, 1, [0, 1]),
            ("Antipasto misto", 20, 1, "all"),
            ("Falanghina", 24, 2, ["me", 1]),
            ("Sfogliatelle", 8, 4, "all"),
        ],
        "surcharge": ("Coperto", 10, 10),
    },
}

# Валюта комнат по языку витрины. Скриншоты для американского App Store с
# рублёвыми ценами выглядят так, будто приложение не для этого рынка.
# Валюта комнат по языку витрины — родная рынку: кадр для японского App Store
# с долларами выглядит так, будто приложение не для этого рынка.
CURRENCY = {
    "ru": "RUB", "en": "EUR", "es": "EUR", "de": "EUR", "fr": "EUR",
    "ja": "JPY", "zh-Hans": "CNY", "ko": "KRW", "pt-BR": "BRL", "it": "EUR",
}

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
    "es": {
        "email": "shots-es@splitty.test",
        "me": "Álvaro",
        "peers": [(9301, "Lucía", "lucia"), (9302, "Pablo", "pablo"), (9303, "Nuria", "nuria")],
        "rooms": [
            {
                "name": "Viaje a Barcelona",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Apartamento, tres noches", 390, "me", "all"),
                    ("Cena en la Barceloneta", 78, 0, "all"),
                    ("Taxi desde el aeropuerto", 34, 1, "all"),
                    ("Entradas a la Sagrada Familia", 104, "me", "all"),
                    ("Café en el Gòtic", 14, 2, "all"),
                    ("Metro, bonos de tres días", 33, 0, "all"),
                    ("Mercado de la Boqueria", 46, "me", "all"),
                    ("Noche de tapas en Gràcia", 62, 1, "all"),
                ],
            },
            {
                "name": "Piso compartido",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Suministros de agosto", 104, "me", "all"),
                    ("Internet", 32, 0, "all"),
                    ("Compra semanal", 68, "me", "all"),
                    ("Limpieza", 45, 0, "all"),
                ],
            },
            {
                "name": "Finca el finde",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Carne y carbón", 52, 2, "all"),
                    ("Gasolina", 38, "me", "all"),
                ],
            },
        ],
    },
    "de": {
        "email": "shots-de@splitty.test",
        "me": "Jonas",
        "peers": [(9401, "Lena", "lena"), (9402, "Felix", "felix"), (9403, "Marie", "marie")],
        "rooms": [
            {
                "name": "Städtetrip Berlin",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Ferienwohnung, drei Nächte", 420, "me", "all"),
                    ("Abendessen am Schlesischen Tor", 86, 0, "all"),
                    ("Taxi vom Flughafen", 38, 1, "all"),
                    ("Museumsinsel, Tageskarten", 76, "me", "all"),
                    ("Kaffee in Mitte", 16, 2, "all"),
                    ("BVG-Tickets für drei Tage", 42, 0, "all"),
                    ("Markthalle Neun", 54, "me", "all"),
                    ("Konzert im Berghain-Vorprogramm", 72, 1, "all"),
                ],
            },
            {
                "name": "WG Prenzlauer Berg",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Nebenkosten August", 112, "me", "all"),
                    ("Internet", 35, 0, "all"),
                    ("Wocheneinkauf", 74, "me", "all"),
                    ("Putzdienst", 48, 0, "all"),
                ],
            },
            {
                "name": "Wochenende am See",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Grillgut und Kohle", 56, 2, "all"),
                    ("Benzin", 41, "me", "all"),
                ],
            },
        ],
    },
    "fr": {
        "email": "shots-fr@splitty.test",
        "me": "Camille",
        "peers": [(9501, "Léa", "lea"), (9502, "Hugo", "hugo"), (9503, "Sarah", "sarah")],
        "rooms": [
            {
                "name": "Week-end à Lyon",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Appartement, trois nuits", 405, "me", "all"),
                    ("Dîner aux Halles Bocuse", 92, 0, "all"),
                    ("Taxi depuis l'aéroport", 36, 1, "all"),
                    ("Funiculaire et musées", 64, "me", "all"),
                    ("Café sur la presqu'île", 15, 2, "all"),
                    ("Tickets TCL, trois jours", 39, 0, "all"),
                    ("Marché Saint-Antoine", 48, "me", "all"),
                    ("Bouchon lyonnais", 78, 1, "all"),
                ],
            },
            {
                "name": "Coloc rue Vieille",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Charges d'août", 108, "me", "all"),
                    ("Internet", 31, 0, "all"),
                    ("Courses de la semaine", 71, "me", "all"),
                    ("Ménage", 46, 0, "all"),
                ],
            },
            {
                "name": "Chalet en montagne",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Viande et charbon", 54, 2, "all"),
                    ("Essence", 39, "me", "all"),
                ],
            },
        ],
    },
    "ja": {
        "email": "shots-ja@splitty.test",
        "me": "ハルカ",
        "peers": [(9601, "ユウタ", "yuta"), (9602, "ミオ", "mio"), (9603, "ケンジ", "kenji")],
        "rooms": [
            {
                "name": "京都旅行",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("ホテル3泊", 63600, "me", "all"),
                    ("先斗町の夕食", 13000, 0, "all"),
                    ("空港からのタクシー", 4000, 1, "all"),
                    ("嵐山の入場券", 5000, "me", "all"),
                    ("抹茶パフェ", 1900, 2, "all"),
                    ("市バス1日券", 1100, 0, "all"),
                    ("錦市場のランチ", 9100, "me", "all"),
                    ("鴨川沿いの夜", 10500, 1, "all"),
                ],
            },
            {
                "name": "シェアハウス",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("8月の光熱費", 18600, "me", "all"),
                    ("インターネット", 4600, 0, "all"),
                    ("1週間の食材", 14900, "me", "all"),
                    ("ハウスクリーニング", 7400, 0, "all"),
                ],
            },
            {
                "name": "週末の別荘",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("バーベキューと炭", 8400, 2, "all"),
                    ("ガソリン", 5900, "me", "all"),
                ],
            },
        ],
    },
    "zh-Hans": {
        "email": "shots-zh@splitty.test",
        "me": "小雨",
        "peers": [(9701, "子墨", "zimo"), (9702, "佳宁", "jianing"), (9703, "浩然", "haoran")],
        "rooms": [
            {
                "name": "成都之行",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("酒店三晚", 2870, "me", "all"),
                    ("宽窄巷子晚餐", 590, 0, "all"),
                    ("机场打车", 180, 1, "all"),
                    ("大熊猫基地门票", 220, "me", "all"),
                    ("盖碗茶", 80, 2, "all"),
                    ("地铁一日票", 50, 0, "all"),
                    ("锦里午餐", 410, "me", "all"),
                    ("火锅之夜", 480, 1, "all"),
                ],
            },
            {
                "name": "合租公寓",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("八月水电费", 840, "me", "all"),
                    ("宽带", 210, 0, "all"),
                    ("一周买菜", 670, "me", "all"),
                    ("保洁", 340, 0, "all"),
                ],
            },
            {
                "name": "周末民宿",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("烧烤和木炭", 380, 2, "all"),
                    ("汽油", 270, "me", "all"),
                ],
            },
        ],
    },
    "ko": {
        "email": "shots-ko@splitty.test",
        "me": "지훈",
        "peers": [(9801, "서연", "seoyeon"), (9802, "민준", "minjun"), (9803, "하늘", "haneul")],
        "rooms": [
            {
                "name": "제주도 여행",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("호텔 3박", 574000, "me", "all"),
                    ("흑돼지 저녁", 118000, 0, "all"),
                    ("공항 택시", 36000, 1, "all"),
                    ("성산일출봉 입장권", 45000, "me", "all"),
                    ("한라봉 주스", 17000, 2, "all"),
                    ("버스 1일권", 10000, 0, "all"),
                    ("동문시장 점심", 83000, "me", "all"),
                    ("해변가의 저녁", 95000, 1, "all"),
                ],
            },
            {
                "name": "셰어하우스",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("8월 공과금", 168000, "me", "all"),
                    ("인터넷", 42000, 0, "all"),
                    ("일주일 장보기", 134000, "me", "all"),
                    ("청소 서비스", 67000, 0, "all"),
                ],
            },
            {
                "name": "주말 펜션",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("바비큐와 숯", 76000, 2, "all"),
                    ("기름값", 53000, "me", "all"),
                ],
            },
        ],
    },
    "pt-BR": {
        "email": "shots-pt@splitty.test",
        "me": "Bruno",
        "peers": [(9901, "Camila", "camila"), (9902, "Rafa", "rafa"), (9903, "Júlia", "julia")],
        "rooms": [
            {
                "name": "Viagem a Salvador",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Pousada, três noites", 2050, "me", "all"),
                    ("Jantar no Pelourinho", 420, 0, "all"),
                    ("Táxi do aeroporto", 130, 1, "all"),
                    ("Passeio de escuna", 160, "me", "all"),
                    ("Água de coco", 60, 2, "all"),
                    ("Passe de ônibus", 35, 0, "all"),
                    ("Almoço no Mercado", 295, "me", "all"),
                    ("Noite de samba", 340, 1, "all"),
                ],
            },
            {
                "name": "República",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Contas de agosto", 600, "me", "all"),
                    ("Internet", 150, 0, "all"),
                    ("Compras da semana", 480, "me", "all"),
                    ("Faxina", 240, 0, "all"),
                ],
            },
            {
                "name": "Sítio no fim de semana",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Churrasco e carvão", 270, 2, "all"),
                    ("Gasolina", 190, "me", "all"),
                ],
            },
        ],
    },
    "it": {
        "email": "shots-it@splitty.test",
        "me": "Marco",
        "peers": [(9051, "Giulia", "giulia"), (9052, "Luca", "luca"), (9053, "Sara", "sara")],
        "rooms": [
            {
                "name": "Viaggio a Napoli",
                "owner": "me",
                "members": [0, 1, 2],
                "ops": [
                    ("Hotel, tre notti", 410, "me", "all"),
                    ("Cena a Spaccanapoli", 84, 0, "all"),
                    ("Taxi dall'aeroporto", 26, 1, "all"),
                    ("Biglietti per Pompei", 32, "me", "all"),
                    ("Caffè al bar", 12, 2, "all"),
                    ("Biglietti metro", 7, 0, "all"),
                    ("Pranzo al mercato", 59, "me", "all"),
                    ("Serata in centro", 68, 1, "all"),
                ],
            },
            {
                "name": "Casa condivisa",
                "owner": "me",
                "members": [0],
                "ops": [
                    ("Bollette di agosto", 120, "me", "all"),
                    ("Internet", 30, 0, "all"),
                    ("Spesa della settimana", 96, "me", "all"),
                    ("Pulizie", 48, 0, "all"),
                ],
            },
            {
                "name": "Weekend in montagna",
                "owner": 2,
                "members": ["me", 1],
                "ops": [
                    ("Grigliata e carbone", 54, 2, "all"),
                    ("Benzina", 38, "me", "all"),
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
