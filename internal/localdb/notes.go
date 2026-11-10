package localdb

import (
	"database/sql"
	"strings"
	"time"

	"github.com/solworktech/trecs-notes/internal/model"
)

// Insert saves a new note and its tags in one transaction, and fills in
// n.ID and n.CreatedAt (if it was zero, it is set to now). Tags are expected
// to already be normalized (see model.NormalizeTags) - this package doesn't
// re-validate them, so the UI's validation errors stay in one place.
func (db *DB) Insert(n *model.Note) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	n.UpdatedAt = n.CreatedAt
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	res, err := tx.Exec(
		`INSERT INTO notes (app_name, window_title, body, created_at_epoch, updated_at_epoch) VALUES (?, ?, ?, ?, ?)`,
		n.AppName, n.WindowTitle, n.Text, n.CreatedAt.Unix(), n.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, t := range n.Tags {
		if _, err := tx.Exec(`INSERT INTO note_tags (note_id, tag) VALUES (?, ?)`, id, t); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	n.ID = id
	return nil
}

// SetRemoteSync records the outcome of a dual-write attempt: remoteID and
// "now" on success, or an empty remoteID and errMsg on failure. Called
// whether or not sync is currently enabled - a note saved while sync was off
// has no remote state to set, so the caller simply doesn't call this.
func (db *DB) SetRemoteSync(noteID int64, remoteID string, errMsg string) error {
	var syncedAt *int64
	if remoteID != "" {
		now := time.Now().Unix()
		syncedAt = &now
	}
	_, err := db.sql.Exec(
		`UPDATE notes SET remote_id = ?, remote_synced_at = ?, remote_sync_error = ?, dirty = CASE WHEN ? = '' THEN dirty ELSE 0 END WHERE id = ?`,
		nullableString(remoteID), syncedAt, nullableString(errMsg), remoteID, noteID,
	)
	return err
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ListFilter narrows List: a substring to match (case-insensitively) against
// the note's body, app name and window title, and/or a tag it must have.
// Either may be left zero to not filter on it.
type ListFilter struct {
	Query string
	Tag   string
	Limit int // 0: a sensible default (200)
}

// List returns notes newest-first, each with its tags filled in.
func (db *DB) List(f ListFilter) ([]model.Note, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	query := `SELECT DISTINCT n.id, n.app_name, n.window_title, n.body, n.created_at_epoch,
	                  n.remote_id, n.remote_synced_at, n.remote_sync_error
	          FROM notes n`
	var args []any
	var where []string
	if f.Tag != "" {
		query += ` JOIN note_tags t ON t.note_id = n.id AND t.tag = ?`
		args = append(args, f.Tag)
	}
	if f.Query != "" {
		where = append(where, `(n.body LIKE ? ESCAPE '\' OR n.app_name LIKE ? ESCAPE '\' OR n.window_title LIKE ? ESCAPE '\')`)
		like := "%" + likeEscape(f.Query) + "%"
		args = append(args, like, like, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY n.created_at_epoch DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var notes []model.Note
	for rows.Next() {
		var n model.Note
		var created int64
		var remoteID, syncErr sql.NullString
		var syncedAt sql.NullInt64
		if err := rows.Scan(&n.ID, &n.AppName, &n.WindowTitle, &n.Text, &created, &remoteID, &syncedAt, &syncErr); err != nil {
			return nil, err
		}
		n.CreatedAt = time.Unix(created, 0)
		n.RemoteID = remoteID.String
		n.RemoteSyncError = syncErr.String
		if syncedAt.Valid {
			t := time.Unix(syncedAt.Int64, 0)
			n.RemoteSyncedAt = &t
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range notes {
		tags, err := db.tagsFor(notes[i].ID)
		if err != nil {
			return nil, err
		}
		notes[i].Tags = tags
	}
	return notes, nil
}

func (db *DB) tagsFor(noteID int64) ([]string, error) {
	rows, err := db.sql.Query(`SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag`, noteID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tags := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// TagSuggestions lists tags already used, most-used first, that start with
// prefix - for the capture dialog's tag field to offer as the person types.
// Mirrors trecs-server's store.TagSuggestions, minus the "public only"
// restriction (these are the person's own local notes; there is no
// visibility concept to filter by).
func (db *DB) TagSuggestions(prefix string, limit int) ([]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := db.sql.Query(
		`SELECT tag FROM note_tags WHERE tag LIKE ? ESCAPE '\' GROUP BY tag ORDER BY COUNT(*) DESC, tag LIMIT ?`,
		likeEscape(prefix)+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UnsyncedSince lists notes that have never synced (RemoteID empty), oldest
// first - what a "retry failed syncs" action would walk.
func (db *DB) Unsynced(limit int) ([]model.Note, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.sql.Query(
		`SELECT id, app_name, window_title, body, created_at_epoch, COALESCE(updated_at_epoch, created_at_epoch) FROM notes
		 WHERE remote_id IS NULL ORDER BY created_at_epoch ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var notes []model.Note
	for rows.Next() {
		var n model.Note
		var created, updated int64
		if err := rows.Scan(&n.ID, &n.AppName, &n.WindowTitle, &n.Text, &created, &updated); err != nil {
			return nil, err
		}
		n.CreatedAt = time.Unix(created, 0)
		n.UpdatedAt = time.Unix(updated, 0)
		tags, err := db.tagsFor(n.ID)
		if err != nil {
			return nil, err
		}
		n.Tags = tags
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// CountUnsynced is how many notes have no copy on the server (RemoteID empty).
func (db *DB) CountUnsynced() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM notes WHERE remote_id IS NULL`).Scan(&n)
	return n, err
}
