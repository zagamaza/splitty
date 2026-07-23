#!/usr/bin/env python3
"""Хелпер для App Store Connect API (проект Splitty).

Требует: pip3 install pyjwt cryptography
Ключи ожидаются в ~/.appstoreconnect/private_keys/AuthKey_<KID>.p8

Команды:
  python3 asc.py whoami
  python3 asc.py builds
  python3 asc.py attach-build <BUILD_VERSION>        # привязать билд к Internal-группе
  python3 asc.py add-tester <email> <First> <Last>   # добавить тестера в Internal
  python3 asc.py register-device <UDID> <Name>
  python3 asc.py register-bundleid <identifier> <Name>
  python3 asc.py review-status                       # статус Beta App Review
"""
import jwt, time, json, sys, urllib.request, urllib.error

ISSUER = "a30d44ef-0dc4-4c01-bb7b-7235968f61f8"
APP_ID = "6787746052"                                   # Splitttty
GROUP_INTERNAL = "30552ae6-0874-42da-a579-a1a80f8d3073"
GROUP_PUBLIC = "328fc762-514b-460c-ba01-5c4e9aed5faa"

# T6PMYHX4T7 — рабочий для distribution/export. CZ62LFKG7N — запасной (dev-подпись,
# регистрация устройств); на app-store export даёт Cloud signing permission error.
DEFAULT_KID = "T6PMYHX4T7"
import os
KEY_PATH = os.path.expanduser("~/.appstoreconnect/private_keys/AuthKey_{kid}.p8")


def token(kid):
    with open(KEY_PATH.format(kid=kid)) as f:
        key = f.read()
    now = int(time.time())
    return jwt.encode(
        {"iss": ISSUER, "iat": now, "exp": now + 20 * 60, "aud": "appstoreconnect-v1"},
        key, algorithm="ES256", headers={"kid": kid, "typ": "JWT"},
    )


def req(method, path, body=None, kid=DEFAULT_KID):
    r = urllib.request.Request(
        "https://api.appstoreconnect.apple.com" + path,
        data=json.dumps(body).encode() if body else None,
        method=method,
    )
    r.add_header("Authorization", "Bearer " + token(kid))
    r.add_header("Content-Type", "application/json")
    try:
        resp = urllib.request.urlopen(r)
        return resp.status, json.loads(resp.read() or "{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or "{}")


def builds():
    s, b = req("GET", f"/v1/builds?filter[app]={APP_ID}&limit=10&sort=-uploadedDate")
    return b.get("data", [])


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return
    cmd = sys.argv[1]

    if cmd == "whoami":
        s, r = req("GET", "/v1/apps?limit=5")
        for a in r.get("data", []):
            print(a["id"], a["attributes"]["name"], a["attributes"]["bundleId"])

    elif cmd == "builds":
        for b in builds():
            a = b["attributes"]
            print(f"build {a.get('version'):>4}  {a.get('processingState'):<12} {b['id']}")

    elif cmd == "attach-build":
        want = sys.argv[2]
        hit = [b for b in builds() if str(b["attributes"].get("version")) == want]
        if not hit:
            print(f"билд {want} не найден"); sys.exit(1)
        b = hit[0]
        if b["attributes"].get("processingState") != "VALID":
            print(f"билд {want} ещё обрабатывается: {b['attributes'].get('processingState')}"); sys.exit(1)
        s, _ = req("POST", f"/v1/betaGroups/{GROUP_INTERNAL}/relationships/builds",
                   {"data": [{"type": "builds", "id": b["id"]}]})
        print("attach:", s)
        s, g = req("GET", f"/v1/betaGroups/{GROUP_INTERNAL}/builds")
        print("билды в Internal:", [x["attributes"]["version"] for x in g.get("data", [])])

    elif cmd == "add-tester":
        email, first, last = sys.argv[2], sys.argv[3], sys.argv[4]
        body = {"data": {"type": "betaTesters",
                         "attributes": {"email": email, "firstName": first, "lastName": last},
                         "relationships": {"betaGroups": {"data": [
                             {"type": "betaGroups", "id": GROUP_INTERNAL}]}}}}
        s, r = req("POST", "/v1/betaTesters", body)
        print(s, json.dumps(r.get("data", r), ensure_ascii=False)[:300])

    elif cmd == "register-device":
        udid, name = sys.argv[2], sys.argv[3]
        body = {"data": {"type": "devices",
                         "attributes": {"name": name, "platform": "IOS", "udid": udid}}}
        s, r = req("POST", "/v1/devices", body)
        print(s, json.dumps(r.get("data", r), ensure_ascii=False)[:300])

    elif cmd == "register-bundleid":
        ident, name = sys.argv[2], sys.argv[3]
        body = {"data": {"type": "bundleIds",
                         "attributes": {"identifier": ident, "name": name, "platform": "IOS"}}}
        s, r = req("POST", "/v1/bundleIds", body)
        print(s, json.dumps(r.get("data", r), ensure_ascii=False)[:300])

    elif cmd == "review-status":
        for b in builds():
            s, r = req("GET", f"/v1/builds/{b['id']}/betaAppReviewSubmission")
            a = (r.get("data") or {}).get("attributes")
            print(f"build {b['attributes'].get('version')}: {a}")

    else:
        print(__doc__)


if __name__ == "__main__":
    main()
