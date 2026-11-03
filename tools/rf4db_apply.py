"""rf4db_apply.py - apply scraped 达标线/奖杯线/蓝冠线 into species_weight tables.

Usage: python tools/rf4db_apply.py <thresholds.json> <db1> [<db2> ...]

The JSON maps fish_key -> {"达标线": g, "奖杯线": g, "蓝冠线": g}.
"""
import json
import sqlite3
import sys

path = sys.argv[1]
dbs = sys.argv[2:]
with open(path, "r", encoding="utf-8") as f:
    data = json.load(f)

ok_rows = {k: v for k, v in data.items() if "达标线" in v}
bad = [k for k, v in data.items() if "达标线" not in v]
print("loaded", len(data), "ok", len(ok_rows), "bad", bad)

for db in dbs:
    conn = sqlite3.connect(db)
    cur = conn.cursor()
    cur.execute("pragma table_info(species_weight)")
    cols = [r[1] for r in cur.fetchall()]
    if "trophy_line_g" not in cols:
        print(db, "no species_weight table, skip")
        conn.close()
        continue
    changed = 0
    for key, v in ok_rows.items():
        cur.execute(
            "update species_weight set trophy_g=?, trophy_line_g=?, super_trophy_line_g=? where fish_key=?",
            (v.get("达标线"), v.get("奖杯线"), v.get("蓝冠线"), key),
        )
        changed += cur.rowcount
        if v.get("最大体重"):
            cur.execute(
                "update species_weight set max_g=? where fish_key=? and (max_g is null or max_g<?)",
                (v["最大体重"], key, v["最大体重"]),
            )
    conn.commit()
    cur.execute("select count(*), sum(case when trophy_g is null then 1 else 0 end) from species_weight")
    print(db, "rows changed", changed, "total/null:", cur.fetchone())
    conn.close()
