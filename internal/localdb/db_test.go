package localdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/model"
)

func TestInsertListSearchTags(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	n := model.Note{AppName: "xterm", WindowTitle: "ssh prod", Text: "kubectl get pods -A", Tags: []string{"k8s", "ops"}}
	if err := db.Insert(&n); err != nil || n.ID == 0 {
		t.Fatalf("insert: id=%d err=%v", n.ID, err)
	}
	got, err := db.List(ListFilter{Tag: "k8s", Query: "pods"})
	if err != nil || len(got) != 1 || len(got[0].Tags) != 2 {
		t.Fatalf("list: %v %+v", err, got)
	}
	if s, _ := db.TagSuggestions("k", 5); len(s) != 1 || s[0] != "k8s" {
		t.Fatalf("suggestions: %v", s)
	}
	if err := db.SetRemoteSync(n.ID, "abc", ""); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.Unsynced(10); len(u) != 0 {
		t.Fatalf("unsynced should be empty, got %d", len(u))
	}
}

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func scanAll(t *testing.T, db *DB, f Filter) []model.Note {
	t.Helper()
	var out []model.Note
	if err := db.Scan(context.Background(), f, func(n model.Note) bool { out = append(out, n); return true }); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFTSFollowsEditsAndDeletes(t *testing.T) {
	db := openTest(t)
	n := model.Note{AppName: "Firefox", Text: "kubectl rollout restart", Tags: []string{"k8s"}}
	if err := db.Insert(&n); err != nil {
		t.Fatal(err)
	}
	if got := scanAll(t, db, Filter{Match: `"rollout"`}); len(got) != 1 {
		t.Fatalf("fts insert: %d", len(got))
	}
	n.Text = "helm upgrade"
	n.Tags = []string{"helm"}
	if err := db.Update(n); err != nil {
		t.Fatal(err)
	}
	if got := scanAll(t, db, Filter{Match: `"rollout"`}); len(got) != 0 {
		t.Fatalf("stale fts hit after edit: %d", len(got))
	}
	got := scanAll(t, db, Filter{Match: `"helm"`, Tags: []string{"helm"}})
	if len(got) != 1 || got[0].Tags[0] != "helm" || got[0].Dirty {
		t.Fatalf("after edit: %+v", got)
	}
	if err := db.Delete(n.ID); err != nil {
		t.Fatal(err)
	}
	if got := scanAll(t, db, Filter{Match: `"helm"`}); len(got) != 0 {
		t.Fatalf("fts after delete: %d", len(got))
	}
	if _, err := db.Get(n.ID); err != ErrNotFound {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestEditDeleteTrackedForSync(t *testing.T) {
	db := openTest(t)
	a := model.Note{Text: "one"}
	b := model.Note{Text: "two"}
	_ = db.Insert(&a)
	_ = db.Insert(&b)
	_ = db.SetRemoteSync(a.ID, "ra", "")
	_ = db.SetRemoteSync(b.ID, "rb", "")
	if c, _ := db.CountPending(); c != 0 {
		t.Fatalf("pending %d", c)
	}
	a.Text = "one!"
	_ = db.Update(a)
	if d, _ := db.Dirty(10); len(d) != 1 || d[0].RemoteID != "ra" {
		t.Fatalf("dirty: %+v", d)
	}
	_ = db.SetRemoteSync(a.ID, "ra", "")
	if d, _ := db.Dirty(10); len(d) != 0 {
		t.Fatalf("still dirty: %+v", d)
	}
	_ = db.Delete(b.ID)
	if ts, _ := db.Tombstones(); len(ts) != 1 || ts[0] != "rb" {
		t.Fatalf("tombstones: %v", ts)
	}
	_ = db.ClearTombstone("rb")
	if c, _ := db.CountPending(); c != 0 {
		t.Fatalf("pending after clear %d", c)
	}
	// a never-synced note leaves no tombstone
	c := model.Note{Text: "three"}
	_ = db.Insert(&c)
	_ = db.Delete(c.ID)
	if ts, _ := db.Tombstones(); len(ts) != 0 {
		t.Fatalf("tombstone for unsynced note: %v", ts)
	}
}

func TestDataVersionMovesOnWrite(t *testing.T) {
	db := openTest(t)
	v1, err := db.DataVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := model.Note{Text: "x"}
	_ = db.Insert(&n)
	v2, _ := db.DataVersion(context.Background())
	v3, _ := db.DataVersion(context.Background())
	if v2 == v1 || v3 != v2 {
		t.Fatalf("versions %d %d %d", v1, v2, v3)
	}
}

func TestMigrateV1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n := model.Note{Text: "survives the upgrade"}
	_ = db.Insert(&n)
	// pretend this was a v1 file: drop the v2 additions
	for _, s := range []string{`DROP TRIGGER notes_ai`, `DROP TRIGGER notes_ad`, `DROP TRIGGER notes_au`, `DROP TABLE notes_fts`, `DROP TABLE note_tombstones`, `ALTER TABLE notes DROP COLUMN updated_at_epoch`, `ALTER TABLE notes DROP COLUMN dirty`, `UPDATE schema_version SET version = 1`} {
		if _, err := db.sql.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if got := scanAll(t, db, Filter{Match: `"upgrade"`}); len(got) != 1 {
		t.Fatalf("backfill: %d", len(got))
	}
}

func TestUpdatedAtFollowsEdits(t *testing.T) {
	db := openTest(t)
	n := model.Note{Text: "one", CreatedAt: time.Unix(1700000000, 0)}
	_ = db.Insert(&n)
	got, err := db.Get(n.ID)
	if err != nil || got.UpdatedAt.Unix() != 1700000000 {
		t.Fatalf("a note nobody edited was last edited when saved: %+v %v", got.UpdatedAt, err)
	}
	got.Text = "one!"
	if err := db.Update(got); err != nil {
		t.Fatal(err)
	}
	got, _ = db.Get(n.ID)
	if got.UpdatedAt.Unix() <= 1700000000 || got.CreatedAt.Unix() != 1700000000 {
		t.Fatalf("an edit is stamped now and keeps the capture time: created %v updated %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestMigrateFillsUpdatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n := model.Note{Text: "old", CreatedAt: time.Unix(1700000000, 0)}
	_ = db.Insert(&n)
	for _, s := range []string{`UPDATE notes SET updated_at_epoch = NULL`, `UPDATE schema_version SET version = 2`} {
		if _, err := db.sql.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var nulls int
	_ = db.sql.QueryRow(`SELECT COUNT(*) FROM notes WHERE updated_at_epoch IS NULL`).Scan(&nulls)
	if nulls != 0 {
		t.Fatalf("%d notes have no edit time", nulls)
	}
}
