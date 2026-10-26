#!/usr/bin/env python3
"""Wire freshly started Sonarr and Radarr (deploy/dev/arrsim) to arrsim and to Roxy.

For each app: read its API key from config.xml, add the /media root folder, arrsim's
SABnzbd as the download client, arrsim's Newznab as the indexer, and a Webhook
connection to Roxy (so imports reach the library like on a real server). Safe to run
again: existing entries with the same name are left alone. Prints the keys at the end.

    sudo python3 deploy/dev/arrsim/setup.py [--home /srv/arrsim] [--roxy http://localhost:8096]
"""
import argparse
import json
import re
import sys
import time
import urllib.error
import urllib.request

ap = argparse.ArgumentParser()
ap.add_argument("--home", default="/srv/arrsim")
ap.add_argument("--roxy", default="http://localhost:8096")
ap.add_argument("--roxy-user", default="roxy")
ap.add_argument("--roxy-password", default="roxy")
ap.add_argument("--arrsim", default="http://localhost:8099")
args = ap.parse_args()


def call(base, key, method, path, body=None):
    req = urllib.request.Request(base + path, method=method, data=None if body is None else json.dumps(body).encode())
    req.add_header("Content-Type", "application/json")
    if key:
        req.add_header("X-Api-Key", key)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            data = r.read()
            return json.loads(data) if data else None
    except urllib.error.HTTPError as e:
        sys.exit(f"{method} {base}{path}: HTTP {e.code}: {e.read().decode(errors='replace')[:800]}")


def api_key(app):
    path = f"{args.home}/{app}/config.xml"
    for _ in range(90):
        try:
            m = re.search(r"<ApiKey>([^<]+)</ApiKey>", open(path).read())
            if m:
                return m.group(1)
        except OSError:
            pass
        time.sleep(2)
    sys.exit(f"{app}: no API key in {path} (is the container running?)")


def wait_up(base, key):
    for _ in range(90):
        try:
            call(base, key, "GET", "/api/v3/system/status")
            return
        except Exception:
            time.sleep(2)
    sys.exit(f"{base} did not come up")


def set_fields(schema, values):
    for f in schema["fields"]:
        if f["name"] in values:
            f["value"] = values[f["name"]]
    return schema


def add_from_schema(base, key, kind, impl, name, values, extra=None):
    existing = call(base, key, "GET", f"/api/v3/{kind}")
    if any(e["name"] == name for e in existing):
        print(f"  {kind} {name}: already there")
        return
    schema = next(s for s in call(base, key, "GET", f"/api/v3/{kind}/schema") if s["implementation"] == impl)
    schema = set_fields(schema, values)
    schema["name"] = name
    schema.update(extra or {})
    call(base, key, "POST", f"/api/v3/{kind}?forceSave=true", schema)
    print(f"  {kind} {name}: added")


def roxy_api_key():
    tok = call(args.roxy, None, "POST", "/v1/auth/login", {"username": args.roxy_user, "password": args.roxy_password, "client": "arrsim-setup", "device": "arrsim", "deviceId": "arrsim-setup"})["token"]
    req = urllib.request.Request(args.roxy + "/v1/auth/keys", method="POST", data=json.dumps({"name": "arrsim webhooks"}).encode())
    req.add_header("Authorization", "Bearer " + tok)
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=15) as r:
        d = json.loads(r.read())
    return d.get("key") or d.get("token") or d.get("apiKey")


apps = {
    "sonarr": {"url": "http://localhost:8989", "cat": "tv", "catField": "tvCategory", "indexerCats": [5000, 5040], "anime": [5070]},
    "radarr": {"url": "http://localhost:7878", "cat": "movies", "catField": "movieCategory", "indexerCats": [2000, 2040], "anime": None},
}
try:
    roxy_key = roxy_api_key()
except Exception as e:  # Roxy not running: everything else still works
    print("Roxy API key not created:", e)
    roxy_key = None

keys = {}
for app, a in apps.items():
    key = api_key(app)
    keys[app] = key
    base = a["url"]
    wait_up(base, key)
    print(app, base)
    if not any(r["path"].rstrip("/") == "/media" for r in call(base, key, "GET", "/api/v3/rootfolder")):
        call(base, key, "POST", "/api/v3/rootfolder", {"path": "/media"})
        print("  root folder /media: added")
    add_from_schema(base, key, "downloadclient", "Sabnzbd", "arrsim SAB", {
        "host": "localhost", "port": int(args.arrsim.rsplit(":", 1)[1]), "urlBase": "/sabnzbd", "apiKey": "arrsim",
        a["catField"]: a["cat"], "useSsl": False,
    }, {"enable": True, "priority": 1})
    vals = {"baseUrl": args.arrsim + "/newznab", "apiPath": "/api", "apiKey": "arrsim", "categories": a["indexerCats"]}
    if a["anime"]:
        vals["animeCategories"] = a["anime"]
    add_from_schema(base, key, "indexer", "Newznab", "arrsim indexer", vals,
                    {"enableRss": False, "enableAutomaticSearch": True, "enableInteractiveSearch": True, "priority": 25})
    if roxy_key:
        add_from_schema(base, key, "notification", "Webhook", "Roxy", {
            "url": f"{args.roxy}/v1/webhooks/{app}?api_key={roxy_key}", "method": 1,
        }, {"onGrab": True, "onDownload": True, "onUpgrade": True, "onRename": True, "onSeriesDelete": True, "onEpisodeFileDelete": True,
            "onMovieDelete": True, "onMovieFileDelete": True, "onManualInteractionRequired": True, "onHealthIssue": False})

print()
print("SONARR_KEY=" + keys["sonarr"])
print("RADARR_KEY=" + keys["radarr"])
