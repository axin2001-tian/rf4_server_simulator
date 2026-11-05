import json
import sqlite3
import sys

db = r"F:\QQ接收文件\rf4_server（开源单机成品）\rf4_server（开源单机成品）\data\rf4server.db"
conn = sqlite3.connect(db)
cur = conn.cursor()
cur.execute("select guid, item_id, wire_class, fields_json from item where item_id like 'elcon%'")
for guid, item_id, cls, fj in cur.fetchall():
    print(guid, item_id, cls)
    fields = json.loads(fj or "[]")
    print("  field count:", len(fields))
    for i, f in enumerate(fields):
        if i >= 15:
            break
        s = json.dumps(f, ensure_ascii=False)
        print("  f%-2d %s" % (i, s[:120]))
