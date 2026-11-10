package localdb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/model"
)

// ErrNotFound is returned for an id that no longer exists.
var ErrNotFound = errors.New("note not found")

const noteColumns = `n.id, n.app_name, n.window_title, n.body, n.created_at_epoch,
	COALESCE(n.updated_at_epoch, n.created_at_epoch),
	n.remote_id, n.remote_synced_at, n.remote_sync_error, n.dirty,
	(SELECT group_concat(tag, char(31)) FROM (SELECT tag FROM note_tags WHERE note_id = n.id ORDER BY tag))`

type rowScanner interface{ Scan(...any) error }

func scanNote(r rowScanner) (model.Note, error) {
	var n model.Note
	var created, updated int64
	var remoteID, syncErr, tags sql.NullString
	var syncedAt sql.NullInt64
	var dirty int
	if err := r.Scan(&n.ID, &n.AppName, &n.WindowTitle, &n.Text, &created, &updated,
		&remoteID, &syncedAt, &syncErr, &dirty, &tags); err != nil {
		return n, err
	}
	n.CreatedAt = time.Unix(created, 0)
	n.UpdatedAt = time.Unix(updated, 0)
	n.RemoteID = remoteID.String
	n.RemoteSyncError = syncErr.String
	n.Dirty = dirty != 0
	if syncedAt.Valid {
		t := time.Unix(syncedAt.Int64, 0)
		n.RemoteSyncedAt = &t
	}
	n.Tags = []string{}
	if tags.Valid && tags.String != "" {
		n.Tags = strings.Split(tags.String, "\x1f")
	}
	return n, nil
}

// Filter narrows Scan. Every field is optional; they combine with AND.
type Filter struct {
	// Match is an FTS5 expression over body, app name and window title
	// (see internal/notes for how a person's words become one).
	Match string
	// Tags: the note must have all of them.
	Tags []string
	// App: the app name must contain this (case-insensitive).
	App string
	// Since and Until bound the creation time (Unix seconds, 0 = open).
	Since, Until int64
	// Limit caps the rows (0 = no cap).
	Limit int
}

// Scan streams the matching notes, newest first, to fn, which returns false
// to stop. Rows are read as they are produced, so a caller holding a regexp
// can discard most of a large table without ever materialising it.
func (db *DB) Scan(ctx context.Context, f Filter, fn func(model.Note) bool) error {
	q := `SELECT ` + noteColumns + ` FROM notes n`
	var where []string
	var args []any
	if f.Match != "" {
		where = append(where, `n.id IN (SELECT rowid FROM notes_fts WHERE notes_fts MATCH ?)`)
		args = append(args, f.Match)
	}
	for _, t := range f.Tags {
		where = append(where, `EXISTS (SELECT 1 FROM note_tags t WHERE t.note_id = n.id AND t.tag = ?)`)
		args = append(args, t)
	}
	if f.App != "" {
		where = append(where, `n.app_name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(f.App)+"%")
	}
	if f.Since > 0 {
		where = append(where, `n.created_at_epoch >= ?`)
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		where = append(where, `n.created_at_epoch < ?`)
		args = append(args, f.Until)
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY n.created_at_epoch DESC, n.id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := db.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return err
		}
		if !fn(n) {
			return nil
		}
	}
	return rows.Err()
}

// Get returns one note, or ErrNotFound.
func (db *DB) Get(id int64) (model.Note, error) {
	n, err := scanNote(db.sql.QueryRow(`SELECT `+noteColumns+` FROM notes n WHERE n.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// Count is how many notes there are.
func (db *DB) Count() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n)
	return n, err
}

// Update replaces a note's app, title, text and tags. A note the server
// already has is marked dirty so the next sync sends the change.
func (db *DB) Update(n model.Note) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(
		`UPDATE notes SET app_name = ?, window_title = ?, body = ?, updated_at_epoch = ?,
		        dirty = CASE WHEN remote_id IS NULL THEN 0 ELSE 1 END WHERE id = ?`,
		n.AppName, n.WindowTitle, n.Text, time.Now().Unix(), n.ID)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM note_tags WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	for _, t := range n.Tags {
		if _, err := tx.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, ?)`, n.ID, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes a note. If the server has a copy, its id is remembered in
// a tombstone until SendDeletes (internal/remote) has told the server.
func (db *DB) Delete(id int64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var remoteID sql.NullString
	if err := tx.QueryRow(`SELECT remote_id FROM notes WHERE id = ?`, id).Scan(&remoteID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if remoteID.Valid && remoteID.String != "" {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO note_tombstones (remote_id, deleted_at_epoch) VALUES (?, ?)`,
			remoteID.String, time.Now().Unix()); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Dirty lists notes the server has but whose copy there is stale.
func (db *DB) Dirty(limit int) ([]model.Note, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.sql.Query(`SELECT `+noteColumns+` FROM notes n
		WHERE n.remote_id IS NOT NULL AND n.dirty = 1 ORDER BY n.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []model.Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Tombstones lists the server ids of notes deleted here but not yet there.
func (db *DB) Tombstones() ([]string, error) {
	rows, err := db.sql.Query(`SELECT remote_id FROM note_tombstones ORDER BY deleted_at_epoch`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ClearTombstone forgets a deletion the server has acknowledged.
func (db *DB) ClearTombstone(remoteID string) error {
	_, err := db.sql.Exec(`DELETE FROM note_tombstones WHERE remote_id = ?`, remoteID)
	return err
}

// CountPending is how many changes the server has not seen: new notes,
// edits and deletions.
func (db *DB) CountPending() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT
		(SELECT COUNT(*) FROM notes WHERE remote_id IS NULL OR dirty = 1) +
		(SELECT COUNT(*) FROM note_tombstones)`).Scan(&n)
	return n, err
}

// AllTags lists every tag in use with its count, most used first.
func (db *DB) AllTags() ([]string, error) {
	rows, err := db.sql.Query(`SELECT tag FROM note_tags GROUP BY tag ORDER BY COUNT(*) DESC, tag`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// versioner holds one connection apart from the pool, whose
// PRAGMA data_version moves whenever any other connection (so: every write
// this process makes through the pool, and every write another process
// makes) commits. That one number is the cache's invalidation signal.
type versioner struct {
	once sync.Once
	conn *sql.Conn
	err  error
}

var versioners sync.Map // *DB -> *versioner

// DataVersion returns a number that changes whenever the database does.
func (db *DB) DataVersion(ctx context.Context) (int64, error) {
	v, _ := versioners.LoadOrStore(db, &versioner{})
	vv := v.(*versioner)
	vv.once.Do(func() { vv.conn, vv.err = db.sql.Conn(context.Background()) })
	if vv.err != nil {
		return 0, vv.err
	}
	var n int64
	err := vv.conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&n)
	return n, err
}

// ForgetRemote clears a note's server id (the server lost it), so it is
// sent again as a new note.
func (db *DB) ForgetRemote(id int64) error {
	_, err := db.sql.Exec(`UPDATE notes SET remote_id = NULL, remote_synced_at = NULL, dirty = 0 WHERE id = ?`, id)
	return err
}
