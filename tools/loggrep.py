"""loggrep.py - grep a server log tail (fast, UTF-8 tolerant).

Usage: python tools/loggrep.py <logfile> <regex> [tail_bytes] [max_lines]
"""
import re
import sys

path, pattern = sys.argv[1], sys.argv[2]
tail = int(sys.argv[3]) if len(sys.argv) > 3 else 20_000_000
max_lines = int(sys.argv[4]) if len(sys.argv) > 4 else 40

with open(path, "rb") as f:
    f.seek(0, 2)
    size = f.tell()
    f.seek(max(0, size - tail))
    data = f.read()
text = data.decode("utf-8", "replace")
rx = re.compile(pattern)
hits = [l for l in text.splitlines() if rx.search(l)]
print(f"hits={len(hits)} (tail={min(tail, size)} bytes)")
for l in hits[-max_lines:]:
    print(l[:400])
