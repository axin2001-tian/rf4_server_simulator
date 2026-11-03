import json

p = r"F:\rf4_src\internal\wiregen\testdata\wire_spec.json"
d = json.load(open(p, encoding="utf-8"))
for cls in ["ndiejnnioni", "kegeppimjbm"]:
    c = d["classes"][cls]
    print("=" * 20, cls, "type", c.get("type_id"), "dto", c.get("dto"))
    print("ops", c.get("ops"))
    for f in c.get("fields", []):
        print(json.dumps(f, ensure_ascii=False))
