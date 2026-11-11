package localdb

import (
	"reflect"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/model"
)

// a note that was sent to the server (so it has its id) and not edited since
func syncedNote(t *testing.T, db *DB, text, remoteID string, created int64) int64 {
	t.Helper()
	n := model.Note{Text: text, Tags: []string{"old"}, CreatedAt: time.Unix(created, 0)}
	if err := db.Insert(&n); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRemoteSync(n.ID, remoteID, ""); err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestPullAddsANoteTheServerHasAndUsNot(t *testing.T) {
	db := openTest(t)
	var st PullStats
	r := RemoteNote{ID: "r1", AppName: "Firefox", WindowTitle: "Docs", Text: "from the web", Tags: []string{"a1", "b2"}, CreatedAt: 1700000000, UpdatedAt: 1700000500}
	if err := db.ApplyRemote(r, &st); err != nil || st.Added != 1 {
		t.Fatalf("%v %+v", err, st)
	}
	got := scanAll(t, db, Filter{})
	if len(got) != 1 {
		t.Fatalf("%d notes", len(got))
	}
	n := got[0]
	if n.RemoteID != "r1" || n.Dirty || n.Text != "from the web" || n.AppName != "Firefox" || !reflect.DeepEqual(n.Tags, []string{"a1", "b2"}) ||
		n.CreatedAt.Unix() != 1700000000 || n.UpdatedAt.Unix() != 1700000500 {
		t.Fatalf("%+v", n)
	}
	// and it is searchable at once (the index follows the table)
	if hits := scanAll(t, db, Filter{Match: `"web"`}); len(hits) != 1 {
		t.Errorf("not in the index: %d", len(hits))
	}
	// hearing of it again changes nothing, and writes nothing
	v1, _ := db.DataVersion(t.Context())
	if err := db.ApplyRemote(r, &st); err != nil || st.Added != 1 || st.Updated != 0 {
		t.Fatalf("%v %+v", err, st)
	}
	if v2, _ := db.DataVersion(t.Context()); v2 != v1 {
		t.Errorf("the database was written to for nothing")
	}
}

func TestPullReplacesANoteNotEditedHere(t *testing.T) {
	db := openTest(t)
	id := syncedNote(t, db, "old text", "r1", 1700000000)
	var st PullStats
	err := db.ApplyRemote(RemoteNote{ID: "r1", AppName: "x", Text: "edited on the web", Tags: []string{"new1"}, CreatedAt: 1700000000, UpdatedAt: 1700009000}, &st)
	if err != nil || st.Updated != 1 || st.Added != 0 {
		t.Fatalf("%v %+v", err, st)
	}
	n, _ := db.Get(id)
	if n.Text != "edited on the web" || n.AppName != "x" || !reflect.DeepEqual(n.Tags, []string{"new1"}) || n.UpdatedAt.Unix() != 1700009000 || n.Dirty {
		t.Fatalf("%+v", n)
	}
	if c, _ := db.CountPending(); c != 0 {
		t.Errorf("%d pending: a pulled note is not something to send", c)
	}
}

func TestPullLaterEditWins(t *testing.T) {
	db := openTest(t)
	id := syncedNote(t, db, "old", "r1", 1700000000)
	n, _ := db.Get(id)
	n.Text = "edited here"
	if err := db.Update(n); err != nil { // stamped now, and dirty
		t.Fatal(err)
	}
	var st PullStats

	// the server's edit is older: ours stays, still waiting to be sent
	if err := db.ApplyRemote(RemoteNote{ID: "r1", Text: "web, earlier", CreatedAt: 1700000000, UpdatedAt: 1700000100}, &st); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Get(id); got.Text != "edited here" || !got.Dirty || st.Updated != 0 {
		t.Fatalf("the later, local edit stands: %+v %+v", got, st)
	}

	// the server's is later: it replaces ours, which is no longer to be sent
	later := time.Now().Add(time.Hour).Unix()
	if err := db.ApplyRemote(RemoteNote{ID: "r1", Text: "web, later", CreatedAt: 1700000000, UpdatedAt: later}, &st); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Get(id); got.Text != "web, later" || got.Dirty || st.Updated != 1 {
		t.Fatalf("the later, remote edit wins: %+v %+v", got, st)
	}
}

func TestPullDoesNotBringBackANoteDeletedHere(t *testing.T) {
	db := openTest(t)
	id := syncedNote(t, db, "x", "r1", 1700000000)
	if err := db.Delete(id); err != nil {
		t.Fatal(err)
	}
	var st PullStats
	if err := db.ApplyRemote(RemoteNote{ID: "r1", Text: "x", UpdatedAt: 1700000000, CreatedAt: 1700000000}, &st); err != nil || st.Added != 0 {
		t.Fatalf("%v %+v", err, st)
	}
	if c, _ := db.Count(); c != 0 {
		t.Errorf("it came back")
	}
	if ts, _ := db.Tombstones(); len(ts) != 1 {
		t.Errorf("the server is still to be told: %v", ts)
	}
}

func TestPullDeletion(t *testing.T) {
	db := openTest(t)
	id := syncedNote(t, db, "x", "r1", 1700000000)
	other := syncedNote(t, db, "y", "r2", 1700000100)
	var st PullStats

	if err := db.ApplyRemoteDelete("r1", &st); err != nil || st.Removed != 1 {
		t.Fatalf("%v %+v", err, st)
	}
	if _, err := db.Get(id); err == nil {
		t.Errorf("not removed")
	}
	if ts, _ := db.Tombstones(); len(ts) != 0 {
		t.Errorf("a deletion that came from the server is not one to send back: %v", ts)
	}
	if err := db.ApplyRemoteDelete("never-had-it", &st); err != nil || st.Removed != 1 {
		t.Fatalf("%v %+v", err, st)
	}

	// edited here and not sent: the edit wins, and the note will be sent again as new
	n, _ := db.Get(other)
	n.Text = "y, edited"
	_ = db.Update(n)
	if err := db.ApplyRemoteDelete("r2", &st); err != nil || st.Removed != 1 {
		t.Fatalf("%v %+v", err, st)
	}
	got, err := db.Get(other)
	if err != nil || got.RemoteID != "" || got.Text != "y, edited" {
		t.Fatalf("%+v %v", got, err)
	}
	if un, _ := db.Unsynced(10); len(un) != 1 {
		t.Errorf("it is to be sent as a new note: %d", len(un))
	}

	// a deletion made here and also there: the tombstone is not needed any more
	id3 := syncedNote(t, db, "z", "r3", 1700000200)
	_ = db.Delete(id3)
	if err := db.ApplyRemoteDelete("r3", &st); err != nil {
		t.Fatal(err)
	}
	if ts, _ := db.Tombstones(); len(ts) != 0 {
		t.Errorf("%v", ts)
	}
}

func TestCursorIsKeptPerServer(t *testing.T) {
	db := openTest(t)
	if v, err := db.Cursor("https://a"); err != nil || v != 0 {
		t.Fatalf("%d %v", v, err)
	}
	_ = db.SetCursor("https://a", 42)
	_ = db.SetCursor("https://a", 43)
	_ = db.SetCursor("https://b", 7)
	if v, _ := db.Cursor("https://a"); v != 43 {
		t.Errorf("%d", v)
	}
	if v, _ := db.Cursor("https://b"); v != 7 {
		t.Errorf("%d", v)
	}
}
