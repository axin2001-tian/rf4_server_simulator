"""Patch a GitHub PR body from a markdown file (UTF-8 safe)."""
import json
import sys
import urllib.request

repo = sys.argv[1]
number = sys.argv[2]
md_path = sys.argv[3]
token_path = sys.argv[4]

with open(md_path, "r", encoding="utf-8") as f:
    body = f.read()
with open(token_path, "r", encoding="utf-8") as f:
    token = f.read().strip()

payload = json.dumps({"body": body}, ensure_ascii=False).encode("utf-8")
req = urllib.request.Request(
    "https://api.github.com/repos/%s/pulls/%s" % (repo, number),
    data=payload,
    method="PATCH",
    headers={
        "Authorization": "Bearer " + token,
        "User-Agent": "rf4-pr-bot",
        "Accept": "application/vnd.github+json",
        "Content-Type": "application/json; charset=utf-8",
    },
)
with urllib.request.urlopen(req, timeout=40) as r:
    data = json.loads(r.read().decode("utf-8"))
print("updated:", data.get("html_url"), "body_len:", len(data.get("body") or ""))
