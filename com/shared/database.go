package shared

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	*sql.DB
	Path string
}

type AppConfig struct {
	DataDir       string
	DBPath        string
	LiveOutputDir string
}

func OpenDatabase(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// database settings
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func CloseDatabase(db *sql.DB) error {
	if db == nil {
		return errors.New("Database is nil")
	}
	return db.Close()
}

func InitSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS satdump_readings (
	ts BIGINT NOT NULL,
	instance TEXT,
	data JSON
);`)
	if err != nil {
		return err
	}

	type colInfo struct {
		name string
	}
	rows, err := db.Query(`PRAGMA table_info(satdump_readings);`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hasInstance := false
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
			return err
		}
		if name == "instance" {
			hasInstance = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasInstance {
		if _, err := db.Exec(`ALTER TABLE satdump_readings ADD COLUMN instance TEXT;`); err != nil {
			return err
		}
	}
	return nil
}
