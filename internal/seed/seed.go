// Package seed —— 本地数据库初始化：迁移 + 静态配置导入 + 默认档案/管理员。
// cmd/seed 与 launcher 共用。
package seed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动

	"rf4server/internal/config"
	"rf4server/internal/store"
)

const (
	// StaticSchemaVersion 标识列签名/导入算法；StaticDataVersion 标识数据代际。
	StaticSchemaVersion = 1
	StaticDataVersion   = "2026-11-01-f"

	staticDatasetName = "rf4_static"
	defaultStaticPath = "server_data/static.sqlite3"
)

// Result 初始化结果摘要。
type Result struct {
	Seeded       bool // 是否执行了静态导入
	Tables       int  // 静态表数
	Rows         map[string]int64
	AdminCreated bool
	Elapsed      time.Duration
}

type logger interface {
	Info(string)
	Warn(string)
	Error(string)
}

type tableSnapshot struct {
	Columns   []string
	Signature string
	Rows      int64
	Digest    string
}

type datasetSnapshot struct {
	ContentSHA256 string
	Tables        map[string]tableSnapshot
	RowCount      int64
}

type datasetMeta struct {
	SchemaVersion int64
	DataVersion   string
	ContentSHA256 string
	TableCount    int64
	RowCount      int64
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Ensure 本地数据库就绪：打开→迁移→（内容变化时）原子静态导入→默认档案→管理员。
// 幂等：可重复调用。staticPath 为空 = 跳过静态导入。
func Ensure(ctx context.Context, cfg *config.Config, staticPath, adminUser, adminPass string, log logger) (*Result, error) {
	start := time.Now()
	res := &Result{Rows: map[string]int64{}, Tables: len(store.StaticTables)}

	db, err := store.Open(cfg.SQLite.Path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite %s: %w", cfg.SQLite.Path, err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return nil, fmt.Errorf("迁移失败: %w", err)
	}
	if err := store.ValidateTableOwnership(ctx, db); err != nil {
		return nil, fmt.Errorf("表归属校验失败: %w", err)
	}
	log.Info("数据库迁移完成 path=" + cfg.SQLite.Path)

	if staticPath != "" {
		source, err := inspectStatic(staticPath)
		if err != nil {
			return nil, err
		}
		target, targetErr := snapshotDataset(ctx, db.SQL, "main")
		meta, metaFound, metaErr := loadDatasetMeta(ctx, db.SQL)
		if targetErr == nil && metaErr == nil && metaMatchesSnapshot(meta, metaFound, target, source) {
			res.Rows = snapshotRows(target)
			log.Info(fmt.Sprintf("静态配置已是最新版本 version=%s sha256=%s", StaticDataVersion, source.ContentSHA256))
		} else {
			n, rows, err := importStatic(db, staticPath, source, log)
			if err != nil {
				return nil, err
			}
			res.Seeded = true
			res.Tables = n
			res.Rows = rows
		}
	}

	if _, err := db.EnsureProfile(ctx, cfg.Game.DefaultUserid); err != nil {
		return nil, fmt.Errorf("默认档案创建失败: %w", err)
	}
	if adminUser != "" {
		created, err := ensureAdmin(db, adminUser, adminPass)
		if err != nil {
			return nil, fmt.Errorf("初始化管理员失败: %w", err)
		}
		res.AdminCreated = created
		if created {
			log.Info("已创建初始管理员（仅表空时生效） user=" + adminUser)
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// NeedsSeed 完整判断是否需要导入：检查全部静态表、数据集版本和内容哈希。
// 可选 staticPath 用于比较源库内容；不传时使用标准 server_data/static.sqlite3。
func NeedsSeed(cfg *config.Config, staticPaths ...string) bool {
	path := defaultStaticPath
	if len(staticPaths) > 0 && staticPaths[0] != "" {
		path = staticPaths[0]
	}
	return NeedsSeedForSource(cfg, path)
}

// dbHasPlayerData 判断库里是否已有玩家物品（>=1 件即视为已初始化）。
func dbHasPlayerData(path string) bool {
	db, err := sql.Open("sqlite", sqliteReadOnlyURI(path))
	if err != nil {
		return false
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM item`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// NeedsSeedForSource 比较目标库与指定静态源库。
func NeedsSeedForSource(cfg *config.Config, staticPath string) bool {
	if _, err := os.Stat(cfg.SQLite.Path); err != nil {
		return true
	}
	// 库里已有玩家数据（物品非空）时不再重新导入静态配置：
	// 导入会整表重写静态表，对已经游玩的存档既无必要也有覆盖风险。
	if dbHasPlayerData(cfg.SQLite.Path) {
		return false
	}
	source, err := inspectStatic(staticPath)
	if err != nil {
		return true
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyURI(cfg.SQLite.Path))
	if err != nil {
		return true
	}
	defer db.Close()
	target, err := snapshotDataset(context.Background(), db, "main")
	if err != nil {
		return true
	}
	meta, found, err := loadDatasetMeta(context.Background(), db)
	if err != nil || !metaMatchesSnapshot(meta, found, target, source) {
		return true
	}
	return false
}

// ensureAdmin 无管理员时创建初始账号（幂等）。
func ensureAdmin(db *store.DB, username, password string) (bool, error) {
	var n int64
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM admin_user`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if err := db.EnsureAdmin(context.Background(), username, password); err != nil {
		return false, err
	}
	return true, nil
}

func inspectStatic(path string) (*datasetSnapshot, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("静态配置库不存在: %s（仓库自带 server_data/static.sqlite3，请在仓库根运行）", path)
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyURI(path))
	if err != nil {
		return nil, fmt.Errorf("打开静态配置库: %w", err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRow(`PRAGMA main.integrity_check`).Scan(&integrity); err != nil {
		return nil, fmt.Errorf("检查静态配置库完整性: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("静态配置库完整性失败: %s", integrity)
	}
	snap, err := snapshotDataset(context.Background(), db, "main")
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func snapshotDataset(ctx context.Context, q queryer, schema string) (*datasetSnapshot, error) {
	tables := append([]string(nil), store.StaticTables...)
	sort.Strings(tables)
	out := &datasetSnapshot{Tables: make(map[string]tableSnapshot, len(tables))}
	h := sha256.New()
	for _, table := range tables {
		columns, signature, err := tableColumns(ctx, q, schema, table)
		if err != nil {
			return nil, err
		}
		digest, rows, err := digestTable(ctx, q, schema, table, columns, signature)
		if err != nil {
			return nil, err
		}
		out.Tables[table] = tableSnapshot{Columns: columns, Signature: signature, Rows: rows, Digest: digest}
		out.RowCount += rows
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%s\n", table, signature, rows, digest)
	}
	out.ContentSHA256 = hex.EncodeToString(h.Sum(nil))
	return out, nil
}

func tableColumns(ctx context.Context, q queryer, schema, table string) ([]string, string, error) {
	stmt := fmt.Sprintf(`PRAGMA %s.table_info(%s)`, quoteIdent(schema), quoteIdent(table))
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return nil, "", fmt.Errorf("读取 %s.%s 列信息: %w", schema, table, err)
	}
	defer rows.Close()
	var columns []string
	var signature strings.Builder
	for rows.Next() {
		var cid, notNull, pk int64
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, "", fmt.Errorf("读取 %s.%s 列信息: %w", schema, table, err)
		}
		columns = append(columns, name)
		fmt.Fprintf(&signature, "%d:%s:%s:%d:%s:%d;", cid, name, typ, notNull, dflt.String, pk)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("遍历 %s.%s 列信息: %w", schema, table, err)
	}
	if len(columns) == 0 {
		return nil, "", fmt.Errorf("静态配置库缺少表 %s.%s", schema, table)
	}
	return columns, signature.String(), nil
}

func digestTable(ctx context.Context, q queryer, schema, table string, columns []string, signature string) (string, int64, error) {
	order := make([]string, len(columns))
	for i, column := range columns {
		order[i] = quoteIdent(column)
	}
	stmt := fmt.Sprintf(`SELECT * FROM %s.%s ORDER BY %s`,
		quoteIdent(schema), quoteIdent(table), strings.Join(order, ","))
	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		return "", 0, fmt.Errorf("读取 %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	h := sha256.New()
	_, _ = h.Write([]byte(signature))
	var count int64
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return "", 0, fmt.Errorf("扫描 %s.%s: %w", schema, table, err)
		}
		for _, value := range values {
			writeCanonical(h, value)
		}
		_, _ = h.Write([]byte{'\n'})
		count++
	}
	if err := rows.Err(); err != nil {
		return "", 0, fmt.Errorf("遍历 %s.%s: %w", schema, table, err)
	}
	return hex.EncodeToString(h.Sum(nil)), count, nil
}

func writeCanonical(h interface{ Write([]byte) (int, error) }, value any) {
	switch v := value.(type) {
	case nil:
		_, _ = h.Write([]byte("n;"))
	case int64:
		fmt.Fprintf(h, "i:%d;", v)
	case float64:
		fmt.Fprintf(h, "f:%016x;", math.Float64bits(v))
	case string:
		fmt.Fprintf(h, "s:%d:", len(v))
		_, _ = h.Write([]byte(v))
	case []byte:
		fmt.Fprintf(h, "b:%d:", len(v))
		_, _ = h.Write(v)
	default:
		text := fmt.Sprint(v)
		fmt.Fprintf(h, "x:%T:%d:%s;", v, len(text), text)
	}
}

func loadDatasetMeta(ctx context.Context, q queryer) (datasetMeta, bool, error) {
	var exists int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='static_dataset_meta'`).Scan(&exists); err != nil {
		return datasetMeta{}, false, err
	}
	if exists == 0 {
		return datasetMeta{}, false, nil
	}
	var meta datasetMeta
	if err := q.QueryRowContext(ctx, `
		SELECT schema_version, data_version, content_sha256, table_count, row_count
		FROM static_dataset_meta WHERE name=?`, staticDatasetName).Scan(
		&meta.SchemaVersion, &meta.DataVersion, &meta.ContentSHA256, &meta.TableCount, &meta.RowCount); err != nil {
		if err == sql.ErrNoRows {
			return datasetMeta{}, false, nil
		}
		return datasetMeta{}, false, err
	}
	return meta, true, nil
}

func metaMatchesSnapshot(meta datasetMeta, found bool, target, source *datasetSnapshot) bool {
	return found &&
		meta.SchemaVersion == StaticSchemaVersion &&
		meta.DataVersion == StaticDataVersion &&
		meta.ContentSHA256 == source.ContentSHA256 &&
		meta.ContentSHA256 == target.ContentSHA256 &&
		meta.TableCount == int64(len(store.StaticTables)) &&
		meta.RowCount == target.RowCount
}

func snapshotRows(snap *datasetSnapshot) map[string]int64 {
	rows := make(map[string]int64, len(snap.Tables))
	for table, info := range snap.Tables {
		rows[table] = info.Rows
	}
	return rows
}

// importStatic 在一个 IMMEDIATE 事务中完成预检、替换和版本写入。
// 源库通过 mode=ro ATTACH，任何误写都会由 SQLite 直接拒绝。
func importStatic(db *store.DB, sqlitePath string, source *datasetSnapshot, log logger) (int, map[string]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := db.SQL.Conn(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("获取静态导入连接: %w", err)
	}
	defer conn.Close()
	if err := attachStaticReadOnly(ctx, conn, sqlitePath); err != nil {
		return 0, nil, err
	}
	inTx := false
	committed := false
	defer func() {
		if inTx && !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
		_, _ = conn.ExecContext(context.Background(), `DETACH DATABASE src`)
	}()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, nil, fmt.Errorf("开始静态导入事务: %w", err)
	}
	inTx = true

	attached, err := snapshotDataset(ctx, conn, "src")
	if err != nil {
		return 0, nil, err
	}
	if attached.ContentSHA256 != source.ContentSHA256 {
		return 0, nil, fmt.Errorf("静态配置库在预检后发生变化: %s != %s", attached.ContentSHA256, source.ContentSHA256)
	}
	target, err := snapshotDataset(ctx, conn, "main")
	if err != nil {
		return 0, nil, err
	}
	rows := make(map[string]int64, len(source.Tables))
	for _, table := range store.StaticTables {
		srcInfo := attached.Tables[table]
		dstInfo := target.Tables[table]
		if srcInfo.Signature != dstInfo.Signature || strings.Join(srcInfo.Columns, "\x00") != strings.Join(dstInfo.Columns, "\x00") {
			return 0, nil, fmt.Errorf("静态表 %s 列签名不一致: src=%s dst=%s", table, srcInfo.Signature, dstInfo.Signature)
		}
	}
	for _, table := range store.StaticTables {
		start := time.Now()
		if _, err := conn.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(table)); err != nil {
			return 0, nil, fmt.Errorf("清空 %s: %w", table, err)
		}
		columns := quoteColumnList(attached.Tables[table].Columns)
		stmt := fmt.Sprintf(`INSERT INTO main.%s (%s) SELECT %s FROM src.%s`,
			quoteIdent(table), columns, columns, quoteIdent(table))
		result, err := conn.ExecContext(ctx, stmt)
		if err != nil {
			return 0, nil, fmt.Errorf("复制 %s: %w", table, err)
		}
		nrows, err := result.RowsAffected()
		if err != nil {
			return 0, nil, fmt.Errorf("读取 %s 导入行数: %w", table, err)
		}
		if nrows != attached.Tables[table].Rows {
			return 0, nil, fmt.Errorf("复制 %s 行数不一致: copied=%d source=%d", table, nrows, attached.Tables[table].Rows)
		}
		rows[table] = nrows
		log.Info(fmt.Sprintf("静态配置导入完成 table=%s rows=%d elapsed=%s",
			table, nrows, time.Since(start).Round(time.Millisecond)))
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO static_dataset_meta(name, schema_version, data_version, content_sha256, table_count, row_count, applied_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET
			schema_version=excluded.schema_version,
			data_version=excluded.data_version,
			content_sha256=excluded.content_sha256,
			table_count=excluded.table_count,
			row_count=excluded.row_count,
			applied_at=excluded.applied_at`,
		staticDatasetName, StaticSchemaVersion, StaticDataVersion, source.ContentSHA256,
		len(store.StaticTables), source.RowCount, float64(time.Now().Unix())); err != nil {
		return 0, nil, fmt.Errorf("写入静态数据集版本: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, nil, fmt.Errorf("提交静态导入事务: %w", err)
	}
	committed = true
	return len(store.StaticTables), rows, nil
}

func attachStaticReadOnly(ctx context.Context, conn *sql.Conn, path string) error {
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS src`, sqliteReadOnlyURI(path)); err != nil {
		return fmt.Errorf("ATTACH 静态配置库（只读）: %w", err)
	}
	return nil
}

func sqliteReadOnlyURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	return u.String()
}

func quoteIdent(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

func quoteColumnList(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdent(column)
	}
	return strings.Join(quoted, ",")
}
