import json

p = r"F:\rf4_src\internal\gameserver\assets\fight_curves_sets.json"
d = json.load(open(p, encoding="utf-8"))
pools = d["pools"]
print("fallback:", d.get("fallback"), "row_shape:", d.get("row_shape"))
print("n pools:", len(pools))
keys = list(pools.keys())
print("sample keys:", keys[:10])
for k in ["crucian", "perch", "karasekarp", "din_m_carp", "s_freaks", "yotsushiro", "c_barbel", "hi_utsuri"]:
    print(k, "->", "ok" if k in pools else "MISSING")
