// Package localdb is the always-on local store: a SQLite database beside the
// recorder's own files (see internal/config.NotesDir), independent of
// whether the person has opted into also syncing to a trecs server.
//
// It uses modernc.org/sqlite (a pure-Go translation of the SQLite C source:
// no cgo, no WebAssembly, no startup cost), rather than
// mattn/go-sqlite3: trecs-notes has to build for Linux, macOS and Windows
// from whichever machine is doing the release, and a pure-Go driver means
// that never needs a C compiler or a platform-specific libsqlite3 - only
// `go build` targeting each GOOS, same as the rest of the toolchain already
// requires for cross-compiling.
package localdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB wraps the connection and the prepared statements the package uses.
type DB struct {
	sql  *sql.DB
	path string
}

// Open opens (creating if needed) the database at path, and applies any
// migration that hasn't run yet. The directory is created with owner-only
// permissions, matching the recorder's session file.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// _pragma sets foreign_keys=on (SQLite defaults it off per-connection)
	// and busy_timeout so a concurrent write from, say, a "browse notes"
	// window open at the same time as a capture doesn't surface as a
	// SQLITE_BUSY error to the person.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB, path: path}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrating %s: %w", path, err)
	}
	return db, nil
}

func (db *DB) Close() error {
	if v, ok := versioners.LoadAndDelete(db); ok {
		if c := v.(*versioner).conn; c != nil {
			_ = c.Close()
		}
	}
	return db.sql.Close()
}

// schemaVersion is bumped whenever migrate() gains a new step.
const schemaVersion = 3

// migrations[i] takes the schema from version i to i+1.
var migrations = [][]string{
	// v1: the two tables. note_tags mirrors trecs-server's recording_tags
	// (see internal/recording/tags.go there, and internal/model/tags.go
	// here): a join table keyed on the normalized tag text, not a free-text
	// column, so "same tags twice" and prefix search are both plain SQL.
	{
		`CREATE TABLE notes (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			app_name          TEXT NOT NULL DEFAULT '',
			window_title      TEXT NOT NULL DEFAULT '',
			body              TEXT NOT NULL,
			created_at_epoch  INTEGER NOT NULL,
			remote_id         TEXT,
			remote_synced_at  INTEGER,
			remote_sync_error TEXT
		)`,
		`CREATE INDEX idx_notes_created_at ON notes(created_at_epoch)`,
		`CREATE TABLE note_tags (
			note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
			tag     TEXT NOT NULL,
			PRIMARY KEY (note_id, tag)
		)`,
		`CREATE INDEX idx_note_tags_tag ON note_tags(tag)`,
	},
	// v2: browsing. A full-word index over the text (FTS5, kept in step by
	// triggers, so no code path can forget to update it); dirty marks a
	// note edited since the server last saw it; a tombstone remembers a
	// deleted note's server id until the server has been told.
	{
		`ALTER TABLE notes ADD COLUMN updated_at_epoch INTEGER`,
		`ALTER TABLE notes ADD COLUMN dirty INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE note_tombstones (
			remote_id        TEXT PRIMARY KEY,
			deleted_at_epoch INTEGER NOT NULL
		)`,
		`CREATE VIRTUAL TABLE notes_fts USING fts5(
			body, app_name, window_title,
			content='notes', content_rowid='id',
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
			INSERT INTO notes_fts(rowid, body, app_name, window_title)
			VALUES (new.id, new.body, new.app_name, new.window_title);
		END`,
		`CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
			INSERT INTO notes_fts(notes_fts, rowid, body, app_name, window_title)
			VALUES ('delete', old.id, old.body, old.app_name, old.window_title);
		END`,
		`CREATE TRIGGER notes_au AFTER UPDATE OF body, app_name, window_title ON notes BEGIN
			INSERT INTO notes_fts(notes_fts, rowid, body, app_name, window_title)
			VALUES ('delete', old.id, old.body, old.app_name, old.window_title);
			INSERT INTO notes_fts(rowid, body, app_name, window_title)
			VALUES (new.id, new.body, new.app_name, new.window_title);
		END`,
		`INSERT INTO notes_fts(notes_fts) VALUES ('rebuild')`,
	},
	// v3: updated_at_epoch (added empty in v2) is filled in for every
	// note: the time it was saved, until it is edited. Code reads and
	// writes it as always set, as the server's notes.updated_at is.
	{
		`UPDATE notes SET updated_at_epoch = created_at_epoch WHERE updated_at_epoch IS NULL`,
	},
}

func (db *DB) migrate() error {
	if _, err := db.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var current int
	row := db.sql.QueryRow(`SELECT version FROM schema_version LIMIT 1`)
	switch err := row.Scan(&current); {
	case err == sql.ErrNoRows:
		current = 0
	case err != nil:
		return err
	}
	if current > schemaVersion {
		return fmt.Errorf("database is version %d, this build understands up to %d: upgrade trecs-notes", current, schemaVersion)
	}

	for current < schemaVersion {
		tx, err := db.sql.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range migrations[current] {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				if strings.Contains(err.Error(), "fts5") {
					return fmt.Errorf("applying v%d schema: this SQLite build lacks FTS5: %w", current+1, err)
				}
				return fmt.Errorf("applying v%d schema: %w", current+1, err)
			}
		}
		current++
		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, current); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
