package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func sharedSkillStoreTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "shared-skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT OR IGNORE INTO skill_def(cat_id,node_id,group_id) VALUES
		(1,501,1),(1,503,1),(2,702,1),(2,708,1),
		(1,514,0),(2,701,0),(3,313,0),(11,1002,0),
		(2,718,20),(2,721,20),(11,1008,2),(11,1020,2)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSharedSkillPointsKeepExistingSaveBalance(t *testing.T) {
	db := sharedSkillStoreTestDB(t)
	ctx := context.Background()
	const uid = int64(42)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`UPDATE profile SET level=50,xp=123456,gold=4567 WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	nodes := map[int64]int64{501: 7, 503: 0, 702: 7, 708: 0, 1010: 1}
	for node := int64(1001); node <= 1009; node++ {
		nodes[node] = 7
	}
	// 原始导入记录保持单边等级；读取共享视图不批量改写旧档。
	for node, level := range nodes {
		if _, err := db.SQL.Exec(`INSERT INTO skill_node(uid,node_id,level) VALUES(?,?,?)`, uid, node, level); err != nil {
			t.Fatal(err)
		}
	}
	total, spent, bonus, available := db.SkillPointsAvailable(ctx, uid)
	if total != 113 || spent != 78 || bonus != 0 || available != 35 {
		t.Fatalf("existing points changed: total=%d spent=%d bonus=%d available=%d", total, spent, bonus, available)
	}
	projected, err := db.ListSkillNodes(ctx, uid)
	if err != nil || projected[501] != 7 || projected[503] != 7 || projected[702] != 7 || projected[708] != 7 {
		t.Fatalf("shared projection=%v err=%v", projected, err)
	}
	for _, node := range []int64{503, 708} {
		var raw int64
		if err := db.SQL.QueryRow(`SELECT level FROM skill_node WHERE uid=? AND node_id=?`, uid, node).Scan(&raw); err != nil || raw != 0 {
			t.Fatalf("read rewrote saved node %d: %d %v", node, raw, err)
		}
	}
	if err := db.SetSkillBonus(ctx, uid, 37); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	total, spent, bonus, available = db.SkillPointsAvailable(ctx, uid)
	if total != 113 || spent != 78 || bonus != 37 || available != 72 {
		t.Fatalf("extra points changed: total=%d spent=%d bonus=%d available=%d", total, spent, bonus, available)
	}
	p, err := db.GetProfile(ctx, uid)
	if err != nil || p.Level != 50 || p.XP != 123456 || p.Gold != 4567 {
		t.Fatalf("unrelated saved progress changed: %+v %v", p, err)
	}
}

func TestSharedSkillImportedUnequalNodesUseMaximumWithoutWriting(t *testing.T) {
	db := sharedSkillStoreTestDB(t)
	ctx := context.Background()
	const uid = int64(43)
	for node, level := range map[int64]int64{501: 2, 503: 5, 702: 3, 708: 1, 9300: 4} {
		if _, err := db.SQL.Exec(`INSERT INTO skill_node(uid,node_id,level) VALUES(?,?,?)`, uid, node, level); err != nil {
			t.Fatal(err)
		}
	}
	for node, want := range map[int64]int64{501: 5, 503: 5, 702: 3, 708: 3, 9300: 4} {
		if got, err := db.GetSkillNode(ctx, uid, node); err != nil || got != want {
			t.Fatalf("node %d=%d want=%d err=%v", node, got, want, err)
		}
	}
	if got, err := db.SkillNodeLevelSum(ctx, uid); err != nil || got != 12 {
		t.Fatalf("shared points=%d want=12 err=%v", got, err)
	}
	var old int64
	if err := db.SQL.QueryRow(`SELECT level FROM skill_node WHERE uid=? AND node_id=501`, uid).Scan(&old); err != nil || old != 2 {
		t.Fatalf("old raw row changed: %d %v", old, err)
	}
}

func TestSharedSkillNodeWritesAreAtomicAndOtherNodesStayIndependent(t *testing.T) {
	db := sharedSkillStoreTestDB(t)
	ctx := context.Background()
	const uid = int64(44)
	if _, err := db.SQL.Exec(`INSERT INTO skill_node(uid,node_id,level) VALUES(44,501,2),(44,503,5);
		CREATE TRIGGER reject_shared_second BEFORE UPDATE ON skill_node WHEN NEW.node_id=503
		BEGIN SELECT RAISE(ABORT,'test shared write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSkillNode(ctx, uid, 501, 6); err == nil {
		t.Fatal("partial shared write succeeded")
	}
	for node, want := range map[int64]int64{501: 2, 503: 5} {
		var got int64
		if err := db.SQL.QueryRow(`SELECT level FROM skill_node WHERE uid=? AND node_id=?`, uid, node).Scan(&got); err != nil || got != want {
			t.Fatalf("failed transaction changed node %d: %d %v", node, got, err)
		}
	}
	if _, err := db.SQL.Exec(`DROP TRIGGER reject_shared_second`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSkillNode(ctx, uid, 503, 6); err != nil {
		t.Fatal(err)
	}
	for _, node := range []int64{501, 503} {
		var got int64
		if err := db.SQL.QueryRow(`SELECT level FROM skill_node WHERE uid=? AND node_id=?`, uid, node).Scan(&got); err != nil || got != 6 {
			t.Fatalf("shared write did not update %d: %d %v", node, got, err)
		}
	}
	for node, level := range map[int64]int64{9101: 2, 9102: 3} {
		if err := db.SetSkillNode(ctx, uid, node, level); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetSkillNode(ctx, uid, 9101, 4); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetSkillNode(ctx, uid, 9102); err != nil || got != 3 {
		t.Fatalf("unrelated node synchronized: %d %v", got, err)
	}
	if got, err := db.SkillNodeLevelSum(ctx, uid); err != nil || got != 13 {
		t.Fatalf("wrong point cost after shared write: %d %v", got, err)
	}
}

func TestSkillIdentityProjectsGroupsAndLinksWithoutMutatingInput(t *testing.T) {
	db := sharedSkillStoreTestDB(t)
	ident, err := db.LoadSkillIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := ident.Group(514); !reflect.DeepEqual(got, []int64{313, 514, 701, 1002}) {
		t.Fatalf("spinning reel link=%v", got)
	}
	if got := ident.Group(718); !reflect.DeepEqual(got, []int64{718, 721, 1008, 1020}) {
		t.Fatalf("power reel group=%v", got)
	}
	input := map[int64]int64{514: 2, 701: 5, 313: 1, 1002: 3, 718: 2, 721: 3, 1008: 4, 1020: 1}
	projected := ident.Project(input)
	for _, node := range []int64{514, 701, 313, 1002} {
		if projected[node] != 5 {
			t.Fatalf("spinning reel node %d=%d", node, projected[node])
		}
	}
	for _, node := range []int64{718, 721, 1008, 1020} {
		if projected[node] != 4 {
			t.Fatalf("power reel node %d=%d", node, projected[node])
		}
	}
	if input[514] != 2 || ident.SpentPoints(input) != 9 {
		t.Fatalf("input mutated=%v spent=%d", input, ident.SpentPoints(input))
	}
	if got, err := db.SkillNodeLevelSum(context.Background(), 99); err != nil || got != 0 {
		t.Fatalf("empty character points=%d err=%v", got, err)
	}
}

func TestSkillLinkMigrationUpgradesExistingSkillRows(t *testing.T) {
	db := sharedSkillStoreTestDB(t)
	ctx := context.Background()
	const uid = int64(45)
	if _, err := db.SQL.Exec(`DROP TABLE skill_link;
		DELETE FROM schema_migrations WHERE version=34;
		INSERT INTO skill_node(uid,node_id,level) VALUES(?,514,2),(?,701,1),(?,1002,0)`, uid, uid, uid); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	nodes, err := db.ListSkillNodes(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []int64{514, 701, 313, 1002} {
		if nodes[node] != 2 {
			t.Fatalf("node %d=%d", node, nodes[node])
		}
	}
	var raw int64
	if err := db.SQL.QueryRow(`SELECT level FROM skill_node WHERE uid=? AND node_id=701`, uid).Scan(&raw); err != nil || raw != 1 {
		t.Fatalf("raw level=%d err=%v", raw, err)
	}
	if spent, err := db.SkillNodeLevelSum(ctx, uid); err != nil || spent != 2 {
		t.Fatalf("spent=%d err=%v", spent, err)
	}
	var links int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM skill_link`).Scan(&links); err != nil || links != 8 {
		t.Fatalf("links=%d err=%v", links, err)
	}
}
