package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A note with no tags, app or title must still carry those keys: the server
// treats a missing key as "is required".
func TestCreateNoteSendsEmptyFields(t *testing.T) {
	var got map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()
	c := &Client{Server: srv.URL, Cookies: []Cookie{{Name: "s", Value: "v"}}}
	id, err := c.CreateNote(context.Background(), NoteRequest{Body: "hi", CreatedAt: 1})
	if err != nil || id != "abc" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	for _, k := range []string{"app_name", "window_title", "body", "tags", "created_at"} {
		if _, ok := got[k]; !ok {
			t.Errorf("key %q missing from the request", k)
		}
	}
	if string(got["tags"]) != "[]" {
		t.Errorf("tags = %s, want []", got["tags"])
	}
}

func TestUpdateAndDeleteNote(t *testing.T) {
	var method, path string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := &Client{Server: srv.URL, Cookies: []Cookie{{Name: "s", Value: "v"}}}
	ctx := context.Background()

	if err := c.UpdateNote(ctx, "id-1", NoteRequest{Body: "x"}); err != nil || method != http.MethodPut || path != "/v1/notes/id-1" {
		t.Fatalf("update: %v %s %s", err, method, path)
	}
	status = http.StatusNotFound
	if err := c.UpdateNote(ctx, "id-1", NoteRequest{Body: "x"}); !errors.Is(err, ErrRemoteGone) {
		t.Fatalf("update of a lost note: %v", err)
	}
	if err := c.DeleteNote(ctx, "id-1"); err != nil {
		t.Fatalf("delete of a lost note should be fine: %v", err)
	}
	status = http.StatusNoContent
	if err := c.DeleteNote(ctx, "id-1"); err != nil || method != http.MethodDelete {
		t.Fatalf("delete: %v %s", err, method)
	}
	status = http.StatusUnauthorized
	if err := c.DeleteNote(ctx, "id-1"); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("401: %v", err)
	}
}

func TestNoteChangesSince(t *testing.T) {
	var gotPath string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"changes":[{"seq":5,"note":{"id":"a","body":"x","tags":[],"created_at":1,"updated_at":2}},{"seq":6,"deleted":"b"}],"cursor":6,"more":true}`))
		}
	}))
	defer srv.Close()
	c := &Client{Server: srv.URL, Cookies: []Cookie{{Name: "s", Value: "v"}}}
	got, err := c.NoteChangesSince(context.Background(), 4, 200)
	if err != nil || gotPath != "/v1/sync/notes?since=4&limit=200" {
		t.Fatalf("%v %s", err, gotPath)
	}
	if got.Cursor != 6 || !got.More || len(got.Changes) != 2 || got.Changes[0].Note.UpdatedAt != 2 || got.Changes[1].Deleted != "b" {
		t.Fatalf("%+v", got)
	}
	status = http.StatusNotFound
	if _, err := c.NoteChangesSince(context.Background(), 0, 10); !errors.Is(err, ErrSyncUnsupported) {
		t.Errorf("%v", err)
	}
}
