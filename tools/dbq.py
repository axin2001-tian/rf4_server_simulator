"""dbq.py - run one SQL query against a SQLite file and print rows.

Usage: python tools/dbq.py <db_path> <query_file>
"""
import sqlite3
import sys

db, qfile = sys.argv[1], sys.argv[2]
with open(qfile, "r", encoding="utf-8") as f:
    sql = f.read().strip()
conn = sqlite3.connect(db)
cur = conn.cursor()
for stmt in [s for s in sql.split(";") if s.strip()]:
    cur.execute(stmt)
    if cur.description:
        cols = [d[0] for d in cur.description]
        print(" | ".join(cols))
        for row in cur.fetchall():
            print(" | ".join("" if v is None else str(v) for v in row))
    print("-" * 40)
conn.close()
