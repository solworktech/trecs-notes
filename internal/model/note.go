package model

import "time"

// Note is a captured snippet: a terminal command, a paragraph from a web
// page or an email, or anything else the person marked and asked to keep.
type Note struct {
	// ID is the local row id (see internal/localdb). Zero until saved.
	ID int64

	// AppName and WindowTitle describe where the text came from - e.g.
	// AppName "Firefox", WindowTitle "Trecs - Use case: DevOps - Mozilla
	// Firefox". Either may be empty: some apps and some window managers
	// don't expose this, and on Wayland in particular it is often simply
	// not obtainable (see internal/capture). An empty field is not an
	// error; it is shown as blank for the person to fill in by hand.
	AppName     string
	WindowTitle string

	// Text is the marked text itself. Required.
	Text string

	// Tags are free-form labels, normalized by NormalizeTags before the
	// note is saved (lowercase, 2-32 chars, letters/digits/hyphens, at
	// most MaxTags).
	Tags []string

	// CreatedAt is when the note was saved, truncated to the second: this
	// is the "epoch" field mentioned in the spec. Rendering it in the
	// person's locale is a presentation concern (internal/ui), not stored.
	CreatedAt time.Time

	// UpdatedAt is when the note was last edited here (the same as CreatedAt
	// for a note nobody has edited). It is sent to the server, so that an
	// edit keeps the time it was made even when it is synced later.
	UpdatedAt time.Time

	// RemoteID, RemoteSyncedAt and RemoteSyncError track the optional copy
	// in the trecs DB (see internal/api). RemoteID is empty until a sync
	// succeeds; RemoteSyncError holds the last failure's message so the
	// "browse notes" view can show which notes still need to go up.
	RemoteID        string
	RemoteSyncedAt  *time.Time
	RemoteSyncError string

	// Dirty is set when the note was edited after the server last saw it
	// (it has a RemoteID and the server's copy is stale).
	Dirty bool
}

// CreatedAtEpoch is CreatedAt as Unix seconds, the form the local DB and the
// trecs API both store.
func (n Note) CreatedAtEpoch() int64 { return n.CreatedAt.Unix() }

// UpdatedAtEpoch is UpdatedAt in epoch seconds; CreatedAt for a note with no edit time.
func (n Note) UpdatedAtEpoch() int64 {
	if n.UpdatedAt.IsZero() {
		return n.CreatedAt.Unix()
	}
	return n.UpdatedAt.Unix()
}
