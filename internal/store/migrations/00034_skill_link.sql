-- 跨类目同名技能使用同一 link_id 共享等级。
CREATE TABLE IF NOT EXISTS skill_link(
    node_id INTEGER PRIMARY KEY,
    link_id INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_skill_link_link ON skill_link(link_id);

INSERT OR IGNORE INTO skill_link(node_id, link_id) VALUES
    (514, 1), (701, 1), (313, 1), (1002, 1),
    (718, 2), (1008, 2),
    (721, 3), (1020, 3);
