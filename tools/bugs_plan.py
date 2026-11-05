"""Parse the dumped BUG list into a pending-items markdown table."""
import io
import re

lines = io.open(r"F:\rf4_src\logs\bugs.txt", encoding="utf-8").read().splitlines()
rows = []
for ln in lines[1:]:
    if not ln.strip() or ln.startswith("==="):
        continue
    parts = [p.strip() for p in ln.split("|")]
    if len(parts) < 5:
        continue
    title, desc, plan, who, status = parts[0], parts[1], parts[2], parts[3], parts[4]
    fixed = len(parts) > 5 and parts[5].startswith("☑")
    if not title:
        continue
    rows.append({"title": title, "desc": desc, "plan": plan, "who": who, "status": status, "fixed": fixed})

pending = [r for r in rows if not r["fixed"] and r["status"] not in ("已完结", "重复")]
done = [r for r in rows if r["fixed"] or r["status"] == "已完结"]
print("total", len(rows), "pending", len(pending), "done", len(done))
out = ["# 待修列表（共 %d 条）" % len(pending), ""]
for i, r in enumerate(pending, 1):
    d = (" — " + r["desc"]) if r["desc"] else ""
    p = (" 〔方案：" + r["plan"] + "〕") if r["plan"] else ""
    out.append("%d. **%s**%s%s（%s）" % (i, r["title"], d, p, r["status"]))
out.append("")
out.append("# 已完结/已处理（跳过 %d 条）" % len(done))
for r in done:
    out.append("- %s（%s）" % (r["title"], r["status"]))
io.open(r"F:\QQ接收文件\待修列表.md", "w", encoding="utf-8").write("\n".join(out))
print("written")
