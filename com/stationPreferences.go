package com

import (
	"OnlySats/config"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"OnlySats/com/shared"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// ---------- Types ----------

type Note struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"` // stored as UNIX seconds
}

type AboutImage struct {
	ID      int64  `json:"id"`
	Path    string `json:"path"`    // relative or absolute path/URL
	Caption string `json:"caption"` // optional
	Sort    int    `json:"sort"`
}

type Composite struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type PassType struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	DatasetFile string `json:"dataset_file"`
	RawDataFile string `json:"rawdata_file"`
	Downlink    string `json:"downlink"`
}

type ImageDirRule struct {
	ID          int64  `json:"id"`
	PassTypeID  int64  `json:"pass_type_id"`
	DirName     string `json:"dir_name"` // directory key ("" allowed for root)
	Sensor      string `json:"sensor"`
	IsFilled    bool   `json:"is_filled"`
	VPix        int    `json:"v_pix"`
	IsCorrected bool   `json:"is_corrected"`
	Composite   string `json:"composite"`
}

type FolderInclude struct {
	ID           int64  `json:"id"`
	Prefix       string `json:"prefix"`                   // e.g., "meteor", "noaa"
	PassTypeID   int64  `json:"pass_type_id"`             // FK to pass_types
	PassTypeCode string `json:"pass_type_code,omitempty"` // joined convenience
}

type Satdump struct {
	Name    string `json:"name"`
	Address string `json:"address"` // may be empty
	Port    int    `json:"port"`    // 0 = unset
	Logging int    `json:"log"`
}

type tblCol struct {
	Name    string
	NotNull bool
}

type Message struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	Image     []byte    `json:"image,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type UserRow struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Level    int    `json:"level"`
}

// ---------- Open / Close / Migrate ----------

func OpenLocalData() error {
	dataDir := strings.TrimSpace(config.GetString("paths.data"))
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "local_data.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open local_data.db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return fmt.Errorf("init pragmas: %w", err)
	}

	if err := migrateTables(db); err != nil {
		_ = shared.CloseDatabase(db)
		return err
	}
	if err := migrateColumns(db, "satdump", "log", "log INTEGER"); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE satdump SET log = 0 WHERE log IS NULL`); err != nil {
		return fmt.Errorf("backfill satdump.log: %w", err)
	}
	return nil
}

func execDDL(db *sql.DB, stmts ...string) error {
	for i, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("ddl[%d] failed near start of: %.60s ... : %w", i, q, err)
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func migrateColumns(db *sql.DB, table, columnName, columnDef string) error {
	exists, err := columnExists(db, table, columnName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	alter := `ALTER TABLE ` + table + ` ADD COLUMN ` + columnDef + `;`
	if _, err := db.Exec(alter); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, columnName, err)
	}
	return nil
}

func migrateTables(db *sql.DB) error {
	return execDDL(db,
		`CREATE TABLE IF NOT EXISTS admin_notes (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			title     TEXT NOT NULL,
			body      TEXT NOT NULL,
			ts        INTEGER NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS satdump (
			name    TEXT PRIMARY KEY,
			address TEXT,     
			port    INTEGER,
			log     INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS about_body (
			id        INTEGER PRIMARY KEY CHECK (id=1),
			body      TEXT,
			updated   INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS about_images (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			caption     TEXT,
			sort        INTEGER DEFAULT 0,
			data        BLOB,
			mime        TEXT,
			size_bytes  INTEGER,
			width       INTEGER,
			height      INTEGER,
			created_at  INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS about_meta (
			key       TEXT PRIMARY KEY,
			value     TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS color_codes (
			var       TEXT PRIMARY KEY,
			value     TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS app_settings (
			key       TEXT PRIMARY KEY,
			value     TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS composites (
			key     TEXT PRIMARY KEY,
			label   TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1
		);`,

		`CREATE TABLE IF NOT EXISTS pass_types (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			code         TEXT NOT NULL UNIQUE,
			dataset_file TEXT,
			rawdata_file TEXT,
			downlink     TEXT,
			created_ts   INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			updated_ts   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);`,
		`CREATE TRIGGER IF NOT EXISTS trg_pass_types_updated
		AFTER UPDATE ON pass_types
		BEGIN
			UPDATE pass_types SET updated_ts = strftime('%s','now') WHERE id = NEW.id;
		END;`,

		`CREATE TABLE IF NOT EXISTS image_dir_rules (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			pass_type_id  INTEGER NOT NULL REFERENCES pass_types(id) ON DELETE CASCADE,
			dir_name      TEXT NOT NULL,
			sensor        TEXT,
			is_filled     INTEGER NOT NULL DEFAULT 0,
			v_pix         INTEGER NOT NULL DEFAULT 0,
			is_corrected  INTEGER NOT NULL DEFAULT 0,
			composite     TEXT,
			UNIQUE(pass_type_id, dir_name)
		);`,

		`CREATE TABLE IF NOT EXISTS folder_includes (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			prefix        TEXT NOT NULL UNIQUE,
			pass_type_id  INTEGER NOT NULL REFERENCES pass_types(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS users (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			username    TEXT NOT NULL UNIQUE,
			hash        TEXT NOT NULL,
			level       INTEGER NOT NULL CHECK(level BETWEEN 0 AND 10),
			created_ts  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			updated_ts  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
		);`,

		`CREATE TRIGGER IF NOT EXISTS trg_users_updated
		AFTER UPDATE ON users
		BEGIN
			UPDATE users SET updated_ts = strftime('%s','now') WHERE id = NEW.id;
		END;`,

		`CREATE TABLE IF NOT EXISTS messages (
            id        INTEGER PRIMARY KEY AUTOINCREMENT,
            ts        INTEGER NOT NULL,
            title     TEXT NOT NULL,
            message   TEXT NOT NULL,
            type      TEXT,
            image     BLOB
        );`,
	)
}

// ---------- Admin Notes (CRUD) ----------

func AddNote(db *sql.DB, ctx context.Context, title, body string, ts time.Time) (int64, error) {
	if title == "" {
		return 0, errors.New("title required")
	}
	if body == "" {
		return 0, errors.New("body required")
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	res, err := db.ExecContext(ctx, `INSERT INTO admin_notes (title, body, ts) VALUES (?, ?, ?)`,
		title, body, ts.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetNote(db *sql.DB, ctx context.Context, id int64) (*Note, error) {
	var n Note
	var unix int64
	err := db.QueryRowContext(ctx, `SELECT id, title, body, ts FROM admin_notes WHERE id=?`, id).
		Scan(&n.ID, &n.Title, &n.Body, &unix)
	if err != nil {
		return nil, err
	}
	n.Timestamp = time.Unix(unix, 0).UTC()
	return &n, nil
}

func ListNotes(db *sql.DB, ctx context.Context, limit, offset int) ([]Note, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, title, body, ts
FROM admin_notes
ORDER BY ts DESC, id DESC
LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		var n Note
		var unix int64
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &unix); err != nil {
			return nil, err
		}
		n.Timestamp = time.Unix(unix, 0).UTC()
		out = append(out, n)
	}
	return out, rows.Err()
}

func UpdateNote(db *sql.DB, ctx context.Context, id int64, title, body string) error {
	_, err := db.ExecContext(ctx, `UPDATE admin_notes SET title=?, body=? WHERE id=?`, title, body, id)
	return err
}

// Helper on the store to delete by ID with "0 rows" clarity
func DeleteNoteByID(db *sql.DB, ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM admin_notes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errors.New("not found")
	}
	return nil
}

func DeleteNoteByTimestamp(db *sql.DB, ctx context.Context, ts int64) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM admin_notes WHERE ts=?`, ts)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

// ---------- About Page (body, images, meta KV) ----------

func SetAboutBody(db *sql.DB, ctx context.Context, body string) error {
	now := time.Now().Unix()
	_, err := db.ExecContext(ctx, `
INSERT INTO about_body (id, body, updated) VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET body=excluded.body, updated=excluded.updated`,
		body, now)
	return err
}

func GetAboutBody(db *sql.DB, ctx context.Context) (body string, updated time.Time, err error) {
	var unix sql.NullInt64
	err = db.QueryRowContext(ctx, `SELECT body, updated FROM about_body WHERE id=1`).Scan(&body, &unix)
	if err == sql.ErrNoRows {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if unix.Valid {
		updated = time.Unix(unix.Int64, 0).UTC()
	}
	return
}

func tableCols(db *sql.DB, ctx context.Context, table string) (map[string]tblCol, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]tblCol{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[strings.ToLower(name)] = tblCol{Name: name, NotNull: notnull == 1}
	}
	return cols, rows.Err()
}

// inserts image bytes into about_images, adapting to the actual schema.
// Works if `path` was dropped, is nullable, or is NOT NULL
func AddAboutImageBlobFlexible(
	db *sql.DB,
	ctx context.Context,
	data []byte,
	mime string,
	width, height int,
	caption string,
	sort int,
) (int64, error) {
	if len(data) == 0 || mime == "" {
		return 0, errors.New("empty image or mime")
	}
	cols, err := tableCols(db, ctx, "about_images")
	if err != nil {
		return 0, err
	}

	// Build insert column list dynamically.
	type kv struct {
		col string
		val any
	}
	items := []kv{
		{col: "data", val: data},
		{col: "mime", val: mime},
		{col: "size_bytes", val: len(data)},
		{col: "width", val: width},
		{col: "height", val: height},
		{col: "caption", val: caption},
		{col: "sort", val: sort},
	}

	// Optional created_at
	if _, ok := cols["created_at"]; ok {
		items = append(items, kv{col: "created_at", val: time.Now().Unix()})
	}

	// Handle path column if it exists and is NOT NULL.
	needsPath := false
	if c, ok := cols["path"]; ok && c.NotNull {
		needsPath = true
		items = append(items, kv{col: "path", val: "blob://pending"})
	} else if ok { // exists but nullable
		// Let it be NULL (omit column) or set to empty; omitting is fine.
	}

	// Filter to only columns that actually exist in the table.
	filtered := make([]kv, 0, len(items))
	for _, it := range items {
		if _, ok := cols[strings.ToLower(it.col)]; ok {
			filtered = append(filtered, it)
		}
	}
	if len(filtered) == 0 {
		return 0, errors.New("about_images has no matching columns for insert")
	}

	// Build SQL
	colNames := make([]string, len(filtered))
	place := make([]string, len(filtered))
	args := make([]any, len(filtered))
	for i, it := range filtered {
		colNames[i] = it.col
		place[i] = "?"
		args[i] = it.val
	}
	q := fmt.Sprintf("INSERT INTO about_images (%s) VALUES (%s)",
		strings.Join(colNames, ", "),
		strings.Join(place, ", "),
	)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// If path exists and is NOT NULL, set the canonical raw URL with id.
	if needsPath {
		raw := fmt.Sprintf("api/about/images/%d/raw", id)
		if _, err = tx.ExecContext(ctx, `UPDATE about_images SET path=? WHERE id=?`, raw, id); err != nil {
			return 0, err
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func GetAboutImageBlob(db *sql.DB, ctx context.Context, id int64) (data []byte, mime string, createdAt int64, err error) {
	err = db.QueryRowContext(ctx, `
SELECT data, mime, IFNULL(created_at, 0)
FROM about_images
WHERE id = ?
`, id).Scan(&data, &mime, &createdAt)
	if err == sql.ErrNoRows {
		return nil, "", 0, errors.New("not found")
	}
	return
}

func RemoveAboutImage(db *sql.DB, ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM about_images WHERE id=?`, id)
	return err
}

func ListAboutImages(db *sql.DB, ctx context.Context) ([]AboutImage, error) {
	cols, _ := tableCols(db, ctx, "about_images")
	_, hasPath := cols["path"]

	var rows *sql.Rows
	var err error
	if hasPath {
		rows, err = db.QueryContext(ctx, `
SELECT id, path, caption, sort
FROM about_images
ORDER BY sort ASC, id ASC`)
	} else {
		rows, err = db.QueryContext(ctx, `
SELECT id, caption, sort
FROM about_images
ORDER BY sort ASC, id ASC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AboutImage
	for rows.Next() {
		var a AboutImage
		if hasPath {
			if err := rows.Scan(&a.ID, &a.Path, &a.Caption, &a.Sort); err != nil {
				return nil, err
			}
			if strings.TrimSpace(a.Path) == "" {
				a.Path = fmt.Sprintf("api/about/images/%d/raw", a.ID)
			}
		} else {
			if err := rows.Scan(&a.ID, &a.Caption, &a.Sort); err != nil {
				return nil, err
			}
			a.Path = fmt.Sprintf("api/about/images/%d/raw", a.ID)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func SetAboutMeta(db *sql.DB, ctx context.Context, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("key required")
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO about_meta (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func DeleteAboutMeta(db *sql.DB, ctx context.Context, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM about_meta WHERE key=?`, key)
	return err
}

func GetAllAboutMeta(db *sql.DB, ctx context.Context) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM about_meta ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func DeleteAboutBody(db *sql.DB, ctx context.Context) error {
	_, err := db.ExecContext(ctx, `DELETE FROM about_body WHERE id=1`)
	return err
}

func UpdateAboutImage(db *sql.DB, ctx context.Context, id int64, path *string, caption *string, sort *int) error {
	// Build a dynamic UPDATE that only touches provided fields.
	type part struct {
		sql string
		arg any
	}
	parts := []part{}
	if path != nil {
		parts = append(parts, part{sql: "path = ?", arg: *path})
	}
	if caption != nil {
		parts = append(parts, part{sql: "caption = ?", arg: *caption})
	}
	if sort != nil {
		parts = append(parts, part{sql: "sort = ?", arg: *sort})
	}
	if len(parts) == 0 {
		return errors.New("no fields to update")
	}
	var q strings.Builder
	q.WriteString("UPDATE about_images SET ")
	args := make([]any, 0, len(parts)+1)
	for i, p := range parts {
		if i > 0 {
			q.WriteString(", ")
		}
		q.WriteString(p.sql)
		args = append(args, p.arg)
	}
	q.WriteString(" WHERE id = ?")
	args = append(args, id)

	_, err := db.ExecContext(ctx, q.String(), args...)
	return err
}

// ---------- Satdump (CRUD) ----------

// insert a new row. Address may be empty; port may be 0.
func CreateSatdump(db *sql.DB, ctx context.Context, name, address string, port int, log int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO satdump (name, address, port, log) VALUES (?, ?, ?, ?)
	`, name, strings.TrimSpace(address), port, log)
	return err
}

// insert or updates by primary key (name).
func UpsertSatdump(db *sql.DB, ctx context.Context, name, address string, port int, log int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO satdump (name, address, port, log) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET address=excluded.address, port=excluded.port, log=excluded.log
	`, name, strings.TrimSpace(address), port, log)
	return err
}

// fetch a single host by name.
func GetSatdump(db *sql.DB, ctx context.Context, name string) (*Satdump, error) {
	var row Satdump
	var addr sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT name,
		       address,
		       port,
		       IFNULL(log, 0) AS log
		FROM satdump
		WHERE name = ?
	`, strings.TrimSpace(name)).Scan(&row.Name, &addr, &row.Port, &row.Logging)
	if err != nil {
		return nil, err
	}
	if addr.Valid {
		row.Address = addr.String
	}
	return &row, nil
}

// return all hosts ordered by name.
func ListSatdump(db *sql.DB, ctx context.Context) ([]Satdump, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name,
		       address,
		       port,
		       IFNULL(log, 0) AS log
		FROM satdump
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Satdump
	for rows.Next() {
		var r Satdump
		var addr sql.NullString
		if err := rows.Scan(&r.Name, &addr, &r.Port, &r.Logging); err != nil {
			return nil, err
		}
		if addr.Valid {
			r.Address = addr.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func UpdateSatdump(
	db *sql.DB,
	ctx context.Context,
	oldName, newName string,
	addrPtr *string,
	portPtr *int,
	logPtr *int,
) error {

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `SELECT name FROM satdump WHERE name=?`, oldName)
	var existing string
	if err := row.Scan(&existing); err != nil {
		return err
	}

	setParts := []string{"name = ?"}
	args := []any{newName}

	if addrPtr != nil {
		setParts = append(setParts, "address = ?")
		args = append(args, *addrPtr)
	}
	if portPtr != nil {
		setParts = append(setParts, "port = ?")
		args = append(args, *portPtr)
	}
	if logPtr != nil {
		setParts = append(setParts, "log = ?")
		args = append(args, *logPtr)
	}

	args = append(args, oldName)

	q := fmt.Sprintf(`UPDATE satdump SET %s WHERE name=?`, strings.Join(setParts, ", "))
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return err
	}

	if newName != oldName && db != nil {
		if _, err := db.ExecContext(ctx,
			`UPDATE satdump_readings SET instance=? WHERE instance=?`,
			newName, oldName,
		); err != nil {
			return fmt.Errorf("failed to update logs for rename: %w", err)
		}
	}

	return tx.Commit()
}

func DeleteSatdump(db *sql.DB, ctx context.Context, name string) error {
	res, err := db.ExecContext(ctx, `
		DELETE FROM satdump WHERE name = ?
	`, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func ListSatdumpLoggingEnabled(db *sql.DB, ctx context.Context) ([]Satdump, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, address, port, log FROM satdump WHERE IFNULL(log,0) != 0 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Satdump
	for rows.Next() {
		var r Satdump
		var addr sql.NullString
		if err := rows.Scan(&r.Name, &addr, &r.Port, &r.Logging); err != nil {
			return nil, err
		}
		if addr.Valid {
			r.Address = addr.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- Color Codes (CSS variables) ----------

func SetColor(db *sql.DB, ctx context.Context, variable, value string) error {
	variable = strings.TrimSpace(variable)
	value = strings.TrimSpace(value)
	if variable == "" {
		return errors.New("variable required")
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO color_codes (var, value) VALUES (?, ?)
ON CONFLICT(var) DO UPDATE SET value=excluded.value`, variable, value)
	return err
}

func DeleteColor(db *sql.DB, ctx context.Context, variable string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM color_codes WHERE var=?`, strings.TrimSpace(variable))
	return err
}

func GetColors(db *sql.DB, ctx context.Context) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT var, value FROM color_codes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// return the colors stylesheet.
func GenerateColorsCSS(db *sql.DB, ctx context.Context) (string, error) {
	kv, err := GetColors(db, ctx)
	if err != nil {
		return "", err
	}
	// stable order
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(":root{\n")
	for _, k := range keys {
		// Ensure variables are CSS-safe-ish
		name := strings.TrimSpace(k)
		if !strings.HasPrefix(name, "--") {
			name = "--" + name
		}
		b.WriteString(fmt.Sprintf("  %s: %s;\n", name, kv[k]))
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// ---------- App Settings (misc KV that don't need to live in TOML) ----------

func SetSetting(db *sql.DB, ctx context.Context, key, value string) error {
	if db == nil {
		return errors.New("databased is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}

func GetSetting(db *sql.DB, ctx context.Context, key string) (string, error) {
	var v sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, strings.TrimSpace(key)).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	if v.Valid {
		return v.String, nil
	}
	return "", nil
}

func DeleteSetting(db *sql.DB, ctx context.Context, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM app_settings WHERE key=?`, strings.TrimSpace(key))
	return err
}

func ListSettings(db *sql.DB, ctx context.Context) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---------- Composites and Pass Templates ----------

func UpsertComposite(db *sql.DB, ctx context.Context, key, name string, enabled bool) error {
	key = strings.TrimSpace(key)
	name = strings.TrimSpace(name)
	if key == "" || name == "" {
		return errors.New("key and name required")
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO composites (key, label, enabled) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET label=excluded.label, enabled=excluded.enabled
`, key, name, boolToInt(enabled))
	return err
}

func GetComposite(db *sql.DB, ctx context.Context, key string) (*Composite, error) {
	row := db.QueryRowContext(ctx, `SELECT key, label, enabled FROM composites WHERE key=?`, strings.TrimSpace(key))
	var c Composite
	var en int
	if err := row.Scan(&c.Key, &c.Name, &en); err != nil {
		return nil, err
	}
	c.Enabled = en != 0
	return &c, nil
}

func ListConfiguredComposites(db *sql.DB, ctx context.Context) ([]Composite, error) {
	const q = `
SELECT key, label, enabled
FROM composites
ORDER BY key;
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Composite
	for rows.Next() {
		var c Composite
		var en int
		if err := rows.Scan(&c.Key, &c.Name, &en); err != nil {
			return nil, err
		}
		c.Enabled = en != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func ListRuleComposites(db *sql.DB, ctx context.Context) ([]Composite, error) {
	const q = `
SELECT DISTINCT
    composite AS key,
    composite AS label,
    1 AS enabled
FROM image_dir_rules
ORDER BY composite;
`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Composite
	for rows.Next() {
		var c Composite
		var en int
		if err := rows.Scan(&c.Key, &c.Name, &en); err != nil {
			return nil, err
		}
		c.Enabled = true
		out = append(out, c)
	}
	return out, rows.Err()
}

func DeleteComposite(db *sql.DB, ctx context.Context, key string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM composites WHERE key=?`, strings.TrimSpace(key))
	return err
}

// ---------- Pass Types (CRUD) ----------

func getPassTypeIDByCode(db *sql.DB, ctx context.Context, code string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM pass_types WHERE code=?`, strings.TrimSpace(code)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func UpsertPassType(db *sql.DB, ctx context.Context, code, datasetFile, rawdataFile, downlink string) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, errors.New("code required")
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO pass_types (code, dataset_file, rawdata_file, downlink)
VALUES (?, ?, ?, ?)
ON CONFLICT(code) DO UPDATE SET dataset_file=excluded.dataset_file, rawdata_file=excluded.rawdata_file, downlink=excluded.downlink
`, code, strings.TrimSpace(datasetFile), strings.TrimSpace(rawdataFile), strings.TrimSpace(downlink))
	if err != nil {
		return 0, err
	}
	return getPassTypeIDByCode(db, ctx, code)
}

func GetPassTypeByCode(db *sql.DB, ctx context.Context, code string) (*PassType, error) {
	var p PassType
	err := db.QueryRowContext(ctx, `
SELECT id, code, dataset_file, rawdata_file, downlink FROM pass_types WHERE code=?`, strings.TrimSpace(code)).
		Scan(&p.ID, &p.Code, &p.DatasetFile, &p.RawDataFile, &p.Downlink)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func GetPassTypeByID(db *sql.DB, ctx context.Context, id int64) (*PassType, error) {
	var p PassType
	err := db.QueryRowContext(ctx, `
SELECT id, code, dataset_file, rawdata_file, downlink FROM pass_types WHERE id=?`, id).
		Scan(&p.ID, &p.Code, &p.DatasetFile, &p.RawDataFile, &p.Downlink)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func ListPassTypes(db *sql.DB, ctx context.Context) ([]PassType, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, code, dataset_file, rawdata_file, downlink FROM pass_types ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PassType
	for rows.Next() {
		var p PassType
		if err := rows.Scan(&p.ID, &p.Code, &p.DatasetFile, &p.RawDataFile, &p.Downlink); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func DeletePassType(db *sql.DB, ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("code required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM pass_types WHERE code=?`, code).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_dir_rules WHERE pass_type_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM folder_includes WHERE pass_type_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pass_types WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// ---------- Image Dir Rules (CRUD) ----------

func getImageDirRuleID(db *sql.DB, ctx context.Context, passTypeID int64, dirName string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
SELECT id FROM image_dir_rules WHERE pass_type_id=? AND dir_name=?`, passTypeID, dirName).
		Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func UpsertImageDirRule(db *sql.DB, ctx context.Context, passTypeCode, dirName, sensor string, isFilled bool, vPix int, isCorrected bool, composite string) (int64, error) {
	ptID, err := getPassTypeIDByCode(db, ctx, passTypeCode)
	if err != nil {
		return 0, fmt.Errorf("pass type not found: %w", err)
	}

	res, err := db.ExecContext(ctx, `
INSERT INTO image_dir_rules (pass_type_id, dir_name, sensor, is_filled, v_pix, is_corrected, composite)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(pass_type_id, dir_name) DO UPDATE
  SET sensor=excluded.sensor,
      is_filled=excluded.is_filled,
      v_pix=excluded.v_pix,
      is_corrected=excluded.is_corrected,
	  composite=excluded.composite
`, ptID, dirName, strings.TrimSpace(sensor), boolToInt(isFilled), vPix, boolToInt(isCorrected), strings.TrimSpace(composite))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// ON CONFLICT update path; fetch id
		return getImageDirRuleID(db, ctx, ptID, dirName)
	}
	return id, nil
}

func ListImageDirRules(db *sql.DB, ctx context.Context, passTypeCode string) ([]ImageDirRule, error) {
	ptID, err := getPassTypeIDByCode(db, ctx, passTypeCode)
	if err != nil {
		return nil, fmt.Errorf("pass type not found: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, pass_type_id, dir_name, sensor, is_filled, v_pix, is_corrected, composite
FROM image_dir_rules
WHERE pass_type_id=?
ORDER BY dir_name`, ptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImageDirRule
	for rows.Next() {
		var r ImageDirRule
		var filled, corrected int
		if err := rows.Scan(&r.ID, &r.PassTypeID, &r.DirName, &r.Sensor, &filled, &r.VPix, &corrected, &r.Composite); err != nil {
			return nil, err
		}
		r.IsFilled = filled != 0
		r.IsCorrected = corrected != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func DeleteImageDirRule(db *sql.DB, ctx context.Context, passTypeCode, dirName string) error {
	ptID, err := getPassTypeIDByCode(db, ctx, passTypeCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	_, err = db.ExecContext(ctx, `
DELETE FROM image_dir_rules WHERE pass_type_id=? AND dir_name=?`,
		ptID, dirName)
	if err != nil {
		return err
	}
	return nil
}

// ---------- Folder Includes (CRUD) ----------

func UpsertFolderInclude(db *sql.DB, ctx context.Context, prefix, passTypeCode string) (int64, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return 0, errors.New("prefix required")
	}
	ptID, err := getPassTypeIDByCode(db, ctx, passTypeCode)
	if err != nil {
		return 0, fmt.Errorf("pass type not found: %w", err)
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO folder_includes (prefix, pass_type_id)
VALUES (?, ?)
ON CONFLICT(prefix) DO UPDATE SET pass_type_id=excluded.pass_type_id
`, prefix, ptID)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// updated existing; fetch id
		return getFolderIncludeID(db, ctx, prefix)
	}
	return id, nil
}

func getFolderIncludeID(db *sql.DB, ctx context.Context, prefix string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM folder_includes WHERE prefix=?`, prefix).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func ListFolderIncludes(db *sql.DB, ctx context.Context) ([]FolderInclude, error) {
	rows, err := db.QueryContext(ctx, `
SELECT f.id, f.prefix, f.pass_type_id, p.code
FROM folder_includes f
JOIN pass_types p ON p.id = f.pass_type_id
ORDER BY f.prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FolderInclude
	for rows.Next() {
		var f FolderInclude
		if err := rows.Scan(&f.ID, &f.Prefix, &f.PassTypeID, &f.PassTypeCode); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func DeleteFolderInclude(db *sql.DB, ctx context.Context, prefix string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM folder_includes WHERE prefix=?`, strings.TrimSpace(prefix))
	return err
}

func SeedFromPassConfig(db *sql.DB, ctx context.Context, passCfg *PassConfig) error {
	if passCfg == nil {
		return nil
	}
	// composites
	for k, v := range passCfg.Composites {
		if err := UpsertComposite(db, ctx, k, v, true); err != nil {
			return err
		}
	}
	// pass types + image dir rules
	for code, pt := range passCfg.PassTypes {
		if _, err := UpsertPassType(db, ctx, code, pt.DatasetFile, pt.RawDataFile, pt.Downlink); err != nil {
			return err
		}
		for dir, rule := range pt.ImageDirs {
			if _, err := UpsertImageDirRule(db, ctx, code, dir, rule.Sensor, rule.IsFilled, rule.VPix, rule.IsCorrected, rule.Composite); err != nil {
				return err
			}
		}
	}
	// folder includes
	for prefix, code := range passCfg.Passes.FolderIncludes {
		if _, err := UpsertFolderInclude(db, ctx, prefix, code); err != nil {
			return err
		}
	}
	return nil
}

// ------------ Users CRUD-----------

func CreateUser(db *sql.DB, ctx context.Context, username string, level int, plainPassword string) (int64, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, errors.New("username required")
	}
	if level < 0 || level > 10 {
		return 0, errors.New("level must be 0..10")
	}
	if len(plainPassword) == 0 {
		return 0, errors.New("password required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO users (username, hash, level) VALUES (?, ?, ?)
	`, username, string(hash), level)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetUserByUsername(db *sql.DB, ctx context.Context, username string) (*UserRow, error) {
	var u UserRow
	err := db.QueryRowContext(ctx, `
		SELECT id, username, level FROM users WHERE username = ?
	`, strings.TrimSpace(username)).Scan(&u.ID, &u.Username, &u.Level)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func ListUsers(db *sql.DB, ctx context.Context) ([]UserRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, username, level FROM users ORDER BY username
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Username, &u.Level); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func UpdateUsername(db *sql.DB, ctx context.Context, id int64, newUsername string) error {
	newUsername = strings.TrimSpace(newUsername)
	if newUsername == "" {
		return errors.New("username required")
	}
	_, err := db.ExecContext(ctx, `
		UPDATE users SET username = ? WHERE id = ?
	`, newUsername, id)
	return err
}

func UpdateUserLevel(db *sql.DB, ctx context.Context, id int64, newLevel int) error {
	if newLevel < 0 || newLevel > 10 {
		return errors.New("level must be 0..10")
	}
	_, err := db.ExecContext(ctx, `
		UPDATE users SET level = ? WHERE id = ?
	`, newLevel, id)
	return err
}

// replaces the bcrypt hash
func ResetUserPassword(db *sql.DB, ctx context.Context, id int64, newPlain string) error {
	if newPlain == "" {
		return errors.New("password required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPlain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE users SET hash = ? WHERE id = ?
	`, string(hash), id)
	return err
}

func DeleteUser(db *sql.DB, ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func CountUsers(db *sql.DB, ctx context.Context) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// checks bcrypt against stored hash; returns (username, level, ok).
func AuthenticateUser(db *sql.DB, ctx context.Context, username, password string) (string, int, bool, error) {
	var hash string
	var level int
	err := db.QueryRowContext(ctx, `
		SELECT hash, level FROM users WHERE username = ?
	`, strings.TrimSpace(username)).Scan(&hash, &level)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", 0, false, nil
	}
	return username, level, true, nil
}

// -------- Messages CRUD ---------

func AddMessage(db *sql.DB, ctx context.Context, title, msg, typ string, img []byte, ts time.Time) (int64, error) {
	if title == "" || msg == "" {
		return 0, errors.New("title and message required")
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	res, err := db.ExecContext(ctx, `
        INSERT INTO messages (ts, title, message, type, image)
        VALUES (?, ?, ?, ?, ?)`,
		ts.Unix(), title, msg, typ, img)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetMessage(db *sql.DB, ctx context.Context, id int64) (*Message, error) {
	var m Message
	var unix int64
	err := db.QueryRowContext(ctx, `
        SELECT id, ts, title, message, type, image
        FROM messages WHERE id=?`, id).
		Scan(&m.ID, &unix, &m.Title, &m.Message, &m.Type, &m.Image)
	if err != nil {
		return nil, err
	}
	m.Timestamp = time.Unix(unix, 0).UTC()
	return &m, nil
}

// List (with limit/offset)
func ListMessages(db *sql.DB, ctx context.Context, limit, offset int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
        SELECT id, ts, title, message, type, image
        FROM messages
        ORDER BY ts DESC, id DESC
        LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var unix int64
		if err := rows.Scan(&m.ID, &unix, &m.Title, &m.Message, &m.Type, &m.Image); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(unix, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// Update (replace all fields except ts)
func UpdateMessage(db *sql.DB, ctx context.Context, id int64, title, msg, typ *string, img []byte, ts *time.Time) error {
	if id <= 0 {
		return errors.New("invalid id")
	}
	type part struct {
		sql string
		arg any
	}
	set := []part{}
	if title != nil {
		set = append(set, part{"title = ?", *title})
	}
	if msg != nil {
		set = append(set, part{"message = ?", *msg})
	}
	if typ != nil {
		set = append(set, part{"type = ?", *typ})
	}
	// update if caller passed a non-nil slice; allow empty to clear
	if img != nil {
		set = append(set, part{"image = ?", img})
	}
	if ts != nil {
		set = append(set, part{"ts = ?", ts.Unix()})
	}
	if len(set) == 0 {
		return errors.New("nothing to update")
	}
	var q strings.Builder
	q.WriteString("UPDATE messages SET ")
	args := make([]any, 0, len(set)+1)
	for i, p := range set {
		if i > 0 {
			q.WriteString(", ")
		}
		q.WriteString(p.sql)
		args = append(args, p.arg)
	}
	q.WriteString(" WHERE id = ?")
	args = append(args, id)

	res, err := db.ExecContext(ctx, q.String(), args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// by ID
func DeleteMessage(db *sql.DB, ctx context.Context, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM messages WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("not found")
	}
	return nil
}

// Public endpoint
func ListMessagesBefore(db *sql.DB, ctx context.Context, before time.Time, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if before.IsZero() {
		before = time.Now().UTC()
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, ts, title, message, type, image
		FROM messages
		WHERE ts < ?
		ORDER BY ts DESC, id DESC
		LIMIT ?`, before.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		var m Message
		var unix int64
		if err := rows.Scan(&m.ID, &unix, &m.Title, &m.Message, &m.Type, &m.Image); err != nil {
			return nil, err
		}
		m.Timestamp = time.Unix(unix, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}
