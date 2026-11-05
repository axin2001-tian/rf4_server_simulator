"""Dump text cells from the BUG spreadsheet."""
import sys

from openpyxl import load_workbook

path = sys.argv[1]
wb = load_workbook(path, read_only=True, data_only=True)
out = []
for ws in wb.worksheets:
    out.append("=== sheet: %s ===" % ws.title)
    n = 0
    for row in ws.iter_rows(values_only=True):
        vals = ["" if v is None else str(v).replace("\n", " ") for v in row]
        while vals and vals[-1] == "":
            vals.pop()
        if not vals:
            continue
        n += 1
        out.append(" | ".join(vals)[:500])
        if n > 400:
            out.append("... (truncated)")
            break
open(r"F:\rf4_src\logs\bugs.txt", "w", encoding="utf-8").write("\n".join(out))
print("lines:", len(out))
