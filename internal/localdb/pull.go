package localdb

import (
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"
)

// RemoteNote is a note as the server has it.
type RemoteNote struct {
	ID                   string
	AppName, WindowTitle string
	Text                 string
	Tags                 []string
	CreatedAt, UpdatedAt int64 // epoch seconds
}

// PullStats counts what a pull did to the local database.
type PullStats struct {
	Added, Updated, Removed int
}

// Cursor is how far the changes of one server have been taken in (0: none).
// scope names the server.
func (db *DB) Cursor(scope string) (uint64, error) {
	var v string
	err := db.sql.QueryRow(`SELECT value FROM sync_state WHERE key = ?`, "cursor:"+scope).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, 10, 64)
}

// SetCursor records how far the server's changes have been taken in.
func (db *DB) SetCursor(scope string, v uint64) error {
	_, err := db.sql.Exec(`INSERT INTO sync_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, "cursor:"+scope, strconv.FormatUint(v, 10))
	return err
}

// ApplyRemote takes in a note as the server has it, by these rules:
//
//   - a note deleted here and not yet on the server stays deleted;
//   - a note this database has not got is added;
//   - a note it has, and has not edited since the server last saw it, is
//     replaced by the server's copy;
//   - a note it has edited since: the later edit wins - the server's if it
//     was made later (and the local edit is dropped), else the local one,
//     which stays marked to be sent.
//
// Nothing is written when nothing differs, so that a note this machine sent
// and now hears of again does not look like a change.
func (db *DB) ApplyRemote(r RemoteNote, st *PullStats) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var gone int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM note_tombstones WHERE remote_id = ?`, r.ID).Scan(&gone); err != nil {
		return err
	}
	if gone > 0 {
		return nil
	}
	now := time.Now().Unix()

	var id, updated int64
	var dirty int
	var app, title, text string
	err = tx.QueryRow(`SELECT id, dirty, COALESCE(updated_at_epoch, created_at_epoch), app_name, window_title, body
		FROM notes WHERE remote_id = ?`, r.ID).Scan(&id, &dirty, &updated, &app, &title, &text)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.Exec(`INSERT INTO notes (app_name, window_title, body, created_at_epoch, updated_at_epoch,
			remote_id, remote_synced_at, dirty) VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			r.AppName, r.WindowTitle, r.Text, r.CreatedAt, r.UpdatedAt, r.ID, now)
		if err != nil {
			return err
		}
		nid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := setTags(tx, nid, r.Tags); err != nil {
			return err
		}
		st.Added++
		return tx.Commit()
	case err != nil:
		return err
	}

	if dirty != 0 && r.UpdatedAt <= updated {
		return nil // the edit made here is the later one: it stays, to be sent
	}
	tags, err := tagsOf(tx, id)
	if err != nil {
		return err
	}
	want := slices.Clone(r.Tags)
	slices.Sort(want)
	same := app == r.AppName && title == r.WindowTitle && text == r.Text && slices.Equal(tags, want)
	if same && dirty == 0 && updated == r.UpdatedAt {
		return nil
	}
	if _, err := tx.Exec(`UPDATE notes SET app_name = ?, window_title = ?, body = ?, updated_at_epoch = ?,
		remote_synced_at = ?, remote_sync_error = NULL, dirty = 0 WHERE id = ?`,
		r.AppName, r.WindowTitle, r.Text, r.UpdatedAt, now, id); err != nil {
		return err
	}
	if !same {
		if err := setTags(tx, id, r.Tags); err != nil {
			return err
		}
		st.Updated++
	}
	return tx.Commit()
}

// ApplyRemoteDelete takes in the server's deletion of a note: the note goes
// - unless it was edited here and not yet sent, in which case the edit wins
// and the note is sent again as a new one.
func (db *DB) ApplyRemoteDelete(remoteID string, st *PullStats) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM note_tombstones WHERE remote_id = ?`, remoteID); err != nil {
		return err // it is gone from the server: nothing left to tell it
	}
	var id int64
	var dirty int
	err = tx.QueryRow(`SELECT id, dirty FROM notes WHERE remote_id = ?`, remoteID).Scan(&id, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if dirty != 0 {
		if _, err := tx.Exec(`UPDATE notes SET remote_id = NULL, remote_synced_at = NULL, dirty = 0 WHERE id = ?`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.Exec(`DELETE FROM notes WHERE id = ?`, id); err != nil {
		return err
	}
	st.Removed++
	return tx.Commit()
}

func setTags(tx *sql.Tx, noteID int64, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM note_tags WHERE note_id = ?`, noteID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO note_tags (note_id, tag) VALUES (?, ?)`, noteID, t); err != nil {
			return err
		}
	}
	return nil
}

func tagsOf(tx *sql.Tx, noteID int64) ([]string, error) {
	rows, err := tx.Query(`SELECT tag FROM note_tags WHERE note_id = ? ORDER BY tag`, noteID)
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
