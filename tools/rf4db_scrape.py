"""rf4db_scrape.py - scrape 达标线/奖杯线/蓝冠线 from rf4db.com fish pages.

Usage:
    python tools/rf4db_scrape.py sample <key> [<key> ...]
    python tools/rf4db_scrape.py all <static.sqlite3> <out.json>

The site pages contain:
    <dt>达标线</dt><dd>2 kg</dd><dt>奖杯线</dt><dd>25 kg</dd><dt>蓝冠线</dt><dd>32 kg</dd>
Values are grams after parsing ("2 kg" -> 2000).
"""
import concurrent.futures
import json
import re
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = "https://rf4db.com/zh/fishes/"
HDRS = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) rf4db-local-tool/1.0"}

PAIR_RE = re.compile(r"<dt>\s*(达标线|奖杯线|蓝冠线)\s*</dt>\s*<dd>\s*([^<]*?)\s*</dd>", re.S)
# 官方说明里的"最大体重/最大重量"（例如"最大体重记录为 40 公斤"、"最大重量可以达到60公斤"）
MAX_RE = re.compile(r"最大(?:体重|重量)[^。；;]{0,30}?([0-9]+(?:\.[0-9]+)?)\s*(公斤|千克|kg|克|g)", re.I)


def parse_weight_g(txt):
    t = (txt or "").strip().replace(",", ".").replace("，", ".")
    if not t or t in ("-", "—", "无"):
        return None
    m = re.match(r"([0-9]+(?:\.[0-9]+)?)\s*(kg|g|KG|G)", t)
    if not m:
        return None
    v = float(m.group(1))
    unit = (m.group(2) or "").lower()
    if unit == "kg":
        v *= 1000
    return int(round(v))


def fetch(key):
    url = BASE + urllib.parse.quote(key, safe=".")
    req = urllib.request.Request(url, headers=HDRS)
    last = None
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=25) as r:
                html = r.read().decode("utf-8", "replace")
            got = {}
            for name, val in PAIR_RE.findall(html):
                got[name] = parse_weight_g(val)
            mx = MAX_RE.search(html)
            if mx:
                unit = mx.group(2).lower()
                v = float(mx.group(1))
                if unit in ("公斤", "千克", "kg"):
                    v *= 1000
                got["最大体重"] = int(round(v))
            if "达标线" in got:
                return key, got, None
            last = "no thresholds in page"
        except Exception as e:  # noqa: BLE001
            last = str(e)
        time.sleep(1.0 + attempt)
    return key, None, last


def main():
    mode = sys.argv[1]
    if mode == "sample":
        keys = sys.argv[2:]
        for k in keys:
            print(fetch(k), flush=True)
        return
    db_path, out_path = sys.argv[2], sys.argv[3]
    conn = sqlite3.connect(db_path)
    keys = [r[0] for r in conn.execute("select fish_key from species_weight order by fish_key")]
    conn.close()
    print("keys", len(keys), flush=True)
    out = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as ex:
        futs = {ex.submit(fetch, k): k for k in keys}
        done = 0
        for fut in concurrent.futures.as_completed(futs):
            key, got, err = fut.result()
            done += 1
            if got:
                out[key] = got
            else:
                out[key] = {"error": err}
            if done % 20 == 0:
                print("progress", done, "/", len(keys), flush=True)
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=1, sort_keys=True)
    ok = sum(1 for v in out.values() if "达标线" in v)
    print("done", len(out), "ok", ok, "->", out_path, flush=True)


if __name__ == "__main__":
    main()
