package localdb

import (
	"path/filepath"
	"testing"

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
