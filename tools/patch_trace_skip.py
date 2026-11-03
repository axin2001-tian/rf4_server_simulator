"""patch_trace_skip.py - make response/push trace honour traceSkip (one-off helper).

Usage: python tools/patch_trace_skip.py
"""
import io
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

targets = [
    ("internal/gameserver/request_receipts.go", "if traceCmds {", "if traceCmds && !traceSkip[ctx.Key()] {", 3),
    ("internal/gameserver/session.go", "if traceCmds {", "if traceCmds && !traceSkip[cmdKey(main, sub)] {", 1),
]

for rel, old, new, want in targets:
    path = os.path.join(ROOT, rel)
    with io.open(path, "r", encoding="utf-8") as f:
        src = f.read()
    n = src.count(old)
    if n != want:
        print(f"{rel}: expected {want} occurrences, found {n} - skipped")
        continue
    src = src.replace(old, new)
    with io.open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(src)
    print(f"{rel}: patched {n}")
