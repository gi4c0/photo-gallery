package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash BLOB NOT NULL
);

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
	width        INTEGER NOT NULL DEFAULT 0, -- 0 when unknown (HEIC)
	height       INTEGER NOT NULL DEFAULT 0,
	has_thumb    INTEGER NOT NULL DEFAULT 0,
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
	return db, nil
}

// ensureUser creates the account, or resets its password if it exists.
func ensureUser(ctx context.Context, db *sql.DB, email, password string) error {
	email = strings.TrimSpace(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES (?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET password_hash = excluded.password_hash`,
		uuid.NewString(), email, hash)
	return err
}

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
