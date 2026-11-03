"""logcmdcount.py - count cmd=X/Y occurrences in the tail of a server log since its last start.

Usage: python tools/logcmdcount.py <logfile> [tail_bytes]
"""
import re
import sys

path = sys.argv[1]
nbytes = int(sys.argv[2]) if len(sys.argv) > 2 else 3_000_000
with open(path, "rb") as f:
    f.seek(0, 2)
    size = f.tell()
    f.seek(max(0, size - nbytes))
    data = f.read()
lines = data.decode("utf-8", "replace").splitlines()

idx = 0
for i, l in enumerate(lines):
    if "level=INFO" in l and "version=" in l and "gameserver" in l:
        idx = i
recent = lines[idx:]
cmds = {}
for l in recent:
    m = re.search(r"cmd=(\d+/\d+)", l)
    if m:
        cmds[m.group(1)] = cmds.get(m.group(1), 0) + 1
print("lines_since_start", len(recent))
for k in sorted(cmds, key=lambda x: -cmds[x])[:12]:
    print(f"  {k}: {cmds[k]}")
if recent:
    print("last:", recent[-1][:200])
