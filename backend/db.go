package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash BLOB NOT NULL,
	is_admin      INTEGER NOT NULL DEFAULT 0,
	created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Studio branding shared by the whole site; always exactly one row.
CREATE TABLE IF NOT EXISTS settings (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	studio_name  TEXT NOT NULL DEFAULT '',
	accent_color TEXT NOT NULL DEFAULT '#1C1B19'
);
INSERT OR IGNORE INTO settings (id) VALUES (1);

CREATE TABLE IF NOT EXISTS galleries (
	id             TEXT PRIMARY KEY,
	owner_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name           TEXT NOT NULL,
	shot_date      TEXT NOT NULL DEFAULT '',  -- YYYY-MM-DD or empty
	shared_on      TEXT NOT NULL,             -- YYYY-MM-DD, start of the download period
	months         INTEGER NOT NULL,          -- download period length; 0 = no limit
	slug           TEXT NOT NULL UNIQUE,      -- secret part of the client link
	cover_photo_id TEXT REFERENCES photos(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS galleries_owner ON galleries(owner_id);

CREATE TABLE IF NOT EXISTS photos (
	id           TEXT PRIMARY KEY,
	gallery_id   TEXT NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,             -- original file name, used for downloads
	ext          TEXT NOT NULL,             -- extension of the stored original
	content_type TEXT NOT NULL,
	size         INTEGER NOT NULL,
	width        INTEGER NOT NULL,          -- display size after EXIF rotation
	height       INTEGER NOT NULL,
	position     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS photos_gallery ON photos(gallery_id, position);

-- Used by github.com/alexedwards/scs/sqlite3store.
CREATE TABLE IF NOT EXISTS sessions (
	token  TEXT PRIMARY KEY,
	data   BLOB NOT NULL,
	expiry REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry);
`

func openDB(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrations[i] moves a database from user_version i to i+1. Fresh databases
// get the base schema above and then run every migration. Only append.
var migrations = []string{
	// Client download statistics, shown only to the photographer. Timestamps are UTC RFC 3339.
	`ALTER TABLE galleries ADD COLUMN zip_downloads INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE galleries ADD COLUMN zip_last_downloaded_at TEXT;
	 ALTER TABLE photos ADD COLUMN downloads INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE photos ADD COLUMN last_downloaded_at TEXT;`,
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this server supports (%d)", version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		err := withTx(context.Background(), db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(migrations[v]); err != nil {
				return err
			}
			_, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("migrate schema to version %d: %w", v+1, err)
		}
	}
	return nil
}

// nowUTC is the SQL expression for download timestamps.
const nowUTC = `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`

// withTx runs fn in a transaction and commits if fn returns nil.
func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

var errNotFound = errors.New("not found")
