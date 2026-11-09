package api

import (
	"context"
	"encoding/json"
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
