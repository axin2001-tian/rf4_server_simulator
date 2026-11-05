package gameserver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rf4server/internal/store"
)

var gameserverTestDBRoot string

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "rf4-gameserver-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gameserverTestDBRoot = root
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

var migratedDBTemplate struct {
	once sync.Once
	path string
	err  error
}

var itemDBTemplate struct {
	once sync.Once
	path string
	err  error
}

func migratedTestDBTemplate() (string, error) {
	migratedDBTemplate.once.Do(func() {
		migratedDBTemplate.path = filepath.Join(gameserverTestDBRoot, "migrated.db")
		migratedDBTemplate.err = buildTestDBTemplate(migratedDBTemplate.path, false)
	})
	return migratedDBTemplate.path, migratedDBTemplate.err
}

func itemTestDBTemplate() (string, error) {
	itemDBTemplate.once.Do(func() {
		itemDBTemplate.path = filepath.Join(gameserverTestDBRoot, "item-def.db")
		itemDBTemplate.err = buildTestDBTemplate(itemDBTemplate.path, true)
	})
	return itemDBTemplate.path, itemDBTemplate.err
}

func buildTestDBTemplate(path string, withItemDef bool) error {
	db, err := store.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return err
	}
	if withItemDef {
		staticPath := filepath.Join("..", "..", "server_data", "static.sqlite3")
		if _, err := db.SQL.Exec(`ATTACH DATABASE ? AS src`, staticPath); err != nil {
			return fmt.Errorf("附加静态库: %w", err)
		}
		if _, err := db.SQL.Exec(`INSERT OR IGNORE INTO main.item_def SELECT * FROM src.item_def`); err != nil {
			_, _ = db.SQL.Exec(`DETACH DATABASE src`)
			return fmt.Errorf("导入 item_def: %w", err)
		}
		if _, err := db.SQL.Exec(`DETACH DATABASE src`); err != nil {
			return err
		}
	}
	_, _ = db.SQL.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return nil
}

func seedTestDBFile(t *testing.T, path string, withItemDef bool) {
	t.Helper()
	template, err := migratedTestDBTemplate()
	if withItemDef {
		template, err = itemTestDBTemplate()
	}
	if err != nil {
		t.Skipf("准备测试数据库模板: %v", err)
	}
	copyTestDBFile(t, template, path)
}

func openMigratedTestDB(t *testing.T) *store.DB {
	t.Helper()
	return openTestDBFromTemplate(t, false)
}

func openItemTestDB(t *testing.T) *store.DB {
	t.Helper()
	return openTestDBFromTemplate(t, true)
}

func openTestDBFromTemplate(t *testing.T, withItemDef bool) *store.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	seedTestDBFile(t, path, withItemDef)
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("打开测试 SQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func copyTestDBFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("打开数据库模板: %v", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("创建测试数据库: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("复制数据库模板: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("关闭测试数据库: %v", err)
	}
}

const fastBiteDelaySeconds = 0.5

func enableFastBites(t *testing.T, db *store.DB, srv *Server) {
	t.Helper()
	if err := db.TunableSet(t.Context(), "bite_delay_fixed", "0.5", "test"); err != nil {
		t.Fatalf("设置快速咬钩: %v", err)
	}
	srv.tunMu.Lock()
	srv.tunCache = map[string]float64{"bite_delay_fixed": fastBiteDelaySeconds}
	srv.tunAt = time.Now()
	srv.tunMu.Unlock()
}

func backdateCastForFastBite(t *testing.T, srv *Server, uid int64) {
	t.Helper()
	st := srv.stateFor(uid)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.CastTs = time.Now().Add(-13 * time.Second)
	if st.FallSeen && !st.FallTs.IsZero() {
		st.FallTs = st.CastTs
	}
	st.RigSettled = true
}
