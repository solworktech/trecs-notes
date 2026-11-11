package notes

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/localdb"
)

func TestAutoSyncStopsWhenAsked(t *testing.T) {
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s := &Service{DB: db} // syncing is off: a round does nothing
	stop := s.StartAutoSync(time.Hour)
	done := make(chan struct{})
	go func() { stop(); stop(); close(done) }() // and stopping twice is harmless
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
}
