// Package notes is everything trecs-notes does that is not drawing: it is
// what both frontends - the tray app with its GTK windows (internal/gui) and
// the terminal interface (internal/tui) - call, so that neither has a version
// of its own of anything. Saving and syncing a note, the settings and what
// they mean, logging in to a server: if a frontend needs it, it lives here.
package notes

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/capture"
	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/remote"
)

// Service is the notes database and the settings it works under.
type Service struct {
	DB  *localdb.DB
	Cfg config.Config

	cache corpus
	bg    sync.WaitGroup // background syncs in flight
}

// Open reads the settings and opens (creating if needed) the database.
func Open() (*Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	path, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	db, err := localdb.Open(path)
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, Cfg: cfg}, nil
}

// Close closes the database.
func (s *Service) Close() error { return s.DB.Close() }

// Reload re-reads the settings, which another process (the Settings window,
// the other frontend) may have changed. The old ones stay if they cannot be
// read.
func (s *Service) Reload() {
	if cfg, err := config.Load(); err == nil {
		s.Cfg = cfg
	}
}

// Save stores a note and, when syncing is on, sends it to the server in the
// background; the local copy is never held up by, or lost to, that.
func (s *Service) Save(note model.Note) error {
	if err := s.DB.Insert(&note); err != nil {
		return err
	}
	if s.Cfg.RemoteSyncEnabled {
		go func() {
			if err := remote.SyncNote(s.DB, note); err != nil {
				log.Printf("sync: %v", err)
			}
		}()
	}
	return nil
}

// QuickSave stores what a capture found, with no tags. It reports false, and
// stores nothing, when the capture holds no text.
func (s *Service) QuickSave(snap capture.Snapshot) (stored bool, err error) {
	note, ok := snap.QuickNote()
	if !ok {
		return false, nil
	}
	if err := s.Save(note); err != nil {
		return false, err
	}
	return true, nil
}

// Get returns one stored note.
func (s *Service) Get(id int64) (model.Note, error) { return s.DB.Get(id) }

// ErrEmptyNote is returned when an edit would leave a note with no text.
var ErrEmptyNote = errors.New("a note needs some text")

// Update stores an edited note (app, window title, text and tags) and, when
// syncing is on, sends the change to the server in the background.
func (s *Service) Update(note model.Note) error {
	if strings.TrimSpace(note.Text) == "" {
		return ErrEmptyNote
	}
	tags, err := model.NormalizeTags(note.Tags)
	if err != nil {
		return err
	}
	note.Tags = tags
	if err := s.DB.Update(note); err != nil {
		return err
	}
	s.syncInBackground()
	return nil
}

// Delete removes a note and, when syncing is on, tells the server.
func (s *Service) Delete(id int64) error {
	if err := s.DB.Delete(id); err != nil {
		return err
	}
	s.syncInBackground()
	return nil
}

// PendingChanges is how many changes (new notes, edits, deletions) the
// server has not seen; zero when syncing is off.
func (s *Service) PendingChanges() int {
	if !s.Cfg.RemoteSyncEnabled {
		return 0
	}
	n, err := s.DB.CountPending()
	if err != nil {
		return 0
	}
	return n
}

func (s *Service) syncInBackground() {
	if !s.Cfg.RemoteSyncEnabled {
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		remote.SyncPending(s.DB)
	}()
}

// StartAutoSync syncs both ways now and then every interval, for as long as
// a window or terminal that shows notes is open, so that what is changed on
// the website or on another computer turns up in the list by itself. Failures
// (no network, logged out) are left for the next round; the manual Sync says
// what went wrong. The returned function stops it, and waits for a sync in
// progress to finish.
func (s *Service) StartAutoSync(interval time.Duration) (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	round := func() {
		if s.Cfg.RemoteSyncEnabled {
			_, _, _, _ = remote.Sync(s.DB)
		}
	}
	go func() {
		defer close(done)
		round()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				round()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(quit) })
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
}

// Flush waits, up to d, for background syncs to finish: a short-lived
// process (the browse window) calls it on the way out, so that a change made
// just before closing still reaches the server.
func (s *Service) Flush(d time.Duration) {
	done := make(chan struct{})
	go func() { s.bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}
