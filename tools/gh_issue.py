"""Create a GitHub issue with a title and body file."""
import json
import sys
import urllib.request

repo, title, body_path, token_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
with open(body_path, "r", encoding="utf-8") as f:
    body = f.read()
with open(token_path, "r", encoding="utf-8") as f:
    token = f.read().strip()

payload = json.dumps({"title": title, "body": body}, ensure_ascii=False).encode("utf-8")
req = urllib.request.Request(
    "https://api.github.com/repos/%s/issues" % repo,
    data=payload,
    headers={
        "Authorization": "Bearer " + token,
        "User-Agent": "rf4-pr-bot",
        "Accept": "application/vnd.github+json",
        "Content-Type": "application/json; charset=utf-8",
    },
)
with urllib.request.urlopen(req, timeout=40) as r:
    data = json.loads(r.read().decode("utf-8"))
print("issue:", data.get("html_url"))
