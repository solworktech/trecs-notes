package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/api"
	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

// a server whose feed is a fixed list, paged as the real one pages it
func feedServer(t *testing.T, changes []api.NoteChange, calls *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sync/notes" {
			http.NotFound(w, r)
			return
		}
		*calls = append(*calls, r.URL.RawQuery)
		since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit > 2 {
			limit = 2 // a small page, to see paging
		}
		out := api.NoteChanges{Cursor: since, Changes: []api.NoteChange{}}
		for _, c := range changes {
			if c.Seq > since && len(out.Changes) < limit {
				out.Changes = append(out.Changes, c)
				out.Cursor = c.Seq
			} else if c.Seq > since {
				out.More = true
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func login(t *testing.T, server string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	if err := (&api.Session{Server: server, Cookies: []api.Cookie{{Name: "s", Value: "v"}}}).Save(); err != nil {
		t.Fatal(err)
	}
}

func openDB(t *testing.T) *localdb.DB {
	t.Helper()
	db, err := localdb.Open(t.TempDir() + "/n.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPullTakesInTheFeedInPagesAndRemembersWhereItGot(t *testing.T) {
	var calls []string
	feed := []api.NoteChange{
		{Seq: 1, Note: &api.RemoteNote{ID: "r1", Body: "one", Tags: []string{}, CreatedAt: 100, UpdatedAt: 100}},
		{Seq: 2, Note: &api.RemoteNote{ID: "r2", Body: "two", Tags: []string{"tt"}, CreatedAt: 200, UpdatedAt: 200}},
		{Seq: 3, Note: &api.RemoteNote{ID: "r3", Body: "three", Tags: []string{}, CreatedAt: 300, UpdatedAt: 300}},
		{Seq: 4, Deleted: "r1"},
	}
	srv := feedServer(t, feed, &calls)
	login(t, srv.URL)
	db := openDB(t)

	res, err := Pull(db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 3 || res.Removed != 1 {
		t.Fatalf("%+v", res)
	}
	if len(calls) != 2 {
		t.Errorf("two pages of two: %v", calls)
	}
	if n, _ := db.Count(); n != 2 {
		t.Errorf("r1 was added, then deleted: %d notes", n)
	}
	if cur, _ := db.Cursor(srv.URL); cur != 4 {
		t.Errorf("cursor %d", cur)
	}

	// the next pull asks from where it stopped, and finds nothing
	calls = nil
	res, err = Pull(db)
	if err != nil || res != (PullResult{}) || len(calls) != 1 || calls[0] != "since=4&limit=200" {
		t.Fatalf("%v %+v %v", err, res, calls)
	}
}

func TestPullOnAServerWithoutTheFeedSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	login(t, srv.URL)
	if _, err := Pull(openDB(t)); err == nil || err.Error() != api.ErrSyncUnsupported.Error() {
		t.Fatalf("%v", err)
	}
}

func TestSyncSendsThenReceives(t *testing.T) {
	var puts int
	var calls []string
	feed := []api.NoteChange{{Seq: 1, Note: &api.RemoteNote{ID: "web1", Body: "made on the web", Tags: []string{}, CreatedAt: 100, UpdatedAt: 100}}}
	feedH := feedServer(t, feed, &calls)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/notes", func(w http.ResponseWriter, r *http.Request) {
		puts++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"mine1"}`))
	})
	mux.Handle("/v1/sync/notes", feedH.Config.Handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	login(t, srv.URL)
	db := openDB(t)
	_ = db.Insert(&model.Note{Text: "made here", CreatedAt: time.Unix(50, 0)})

	sent, failed, pulled, err := Sync(db)
	if err != nil || failed != 0 || sent != 1 || pulled.Added != 1 || puts != 1 {
		t.Fatalf("%d %d %+v %v", sent, failed, pulled, err)
	}
	if got := DescribeSync(sent, failed, pulled, err); got != "Sent 1. Received 1 new." {
		t.Errorf("%q", got)
	}
	if n, _ := db.Count(); n != 2 {
		t.Errorf("%d notes", n)
	}
	if got := DescribeSync(0, 0, PullResult{}, nil); got != "Everything is up to date." {
		t.Errorf("%q", got)
	}
}

func TestSyncDoesNotPullWhenLoggedOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	login(t, srv.URL)
	db := openDB(t)
	_ = db.Insert(&model.Note{Text: "x"})
	_, failed, _, err := Sync(db)
	if err == nil || failed != 1 {
		t.Fatalf("%d %v", failed, err)
	}
}
