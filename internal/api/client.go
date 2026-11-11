// Package api is the optional second write: when the person has opted in
// (internal/config.Config.RemoteSyncEnabled), a saved note is also POSTed
// to the trecs server, alongside its always-on local copy.
//
// This deliberately mirrors solworktech/trecs's own cloud package (the
// `trecs upload` client) rather than inventing a different auth flow:
// e-mail+password login against POST /v1/auth/login, a session cookie
// saved to disk, sent by hand on every request. trecs-server also has an
// unused device_authorizations table (RFC 8628 device flow) that would
// suit a tray app better than typing a password into a modal - worth
// revisiting once there's a UI for it, but out of scope for this first
// pass, which does what `trecs upload` already does today.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
)

// ErrNotLoggedIn mirrors cloud.ErrNotLoggedIn: no usable session.
var ErrNotLoggedIn = errors.New("not logged in to a trecs server: use Settings to log in")

// Cookie is one cookie the server set at login.
type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Session is what Login saves.
type Session struct {
	Server  string    `json:"server"`
	Email   string    `json:"email,omitempty"`
	Cookies []Cookie  `json:"cookies"`
	Expires time.Time `json:"expires,omitempty"`
}

func sessionPath() (string, error) {
	dir, err := config.NotesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session"), nil
}

// LoadSession reads the saved session, or ErrNotLoggedIn if there is none or
// it has expired.
func LoadSession() (*Session, error) {
	path, err := sessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil || s.Server == "" || len(s.Cookies) == 0 {
		return nil, fmt.Errorf("%s is not a valid session; log in again", path)
	}
	if !s.Expires.IsZero() && time.Now().After(s.Expires) {
		return nil, ErrNotLoggedIn
	}
	return &s, nil
}

// Save writes the session, owner-only, the same way cloud.Session.Save does.
func (s *Session) Save() error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DeleteSession removes the saved session; there being none is not an error.
func DeleteSession() error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Logout ends the session on the server (best effort - the caller deletes
// the local copy either way).
func (c *Client) Logout(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server+"/v1/auth/logout", nil)
	if err != nil {
		return err
	}
	for _, ck := range c.Cookies {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// ResolveServer picks the server to use, in the order `trecs login` does:
// the one the person set in Settings, else the server of the saved login,
// else $TRECS_SERVER, else the built-in default. "" means none is known.
func ResolveServer(configured string) string {
	if s := strings.TrimSpace(configured); s != "" {
		return strings.TrimRight(s, "/")
	}
	if sess, err := LoadSession(); err == nil {
		return sess.Server
	}
	if s := strings.TrimSpace(os.Getenv("TRECS_SERVER")); s != "" {
		return strings.TrimRight(s, "/")
	}
	return config.DefaultServer
}

// NormalizeServer checks a server address the way `trecs login` does:
// scheme://host[/path], and plain http:// only for this machine, because the
// password and the session cookie would otherwise cross the network in the clear.
func NormalizeServer(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%q is not a server address: use something like https://trecs.run", raw)
	}
	if u.Scheme == "http" {
		h := u.Hostname()
		ip := net.ParseIP(h)
		local := strings.EqualFold(h, "localhost") || (ip != nil && ip.IsLoopback())
		if !local {
			return "", fmt.Errorf("%q is not encrypted: use https://", raw)
		}
	}
	return raw, nil
}

// Client talks to one trecs server.
type Client struct {
	Server  string
	Cookies []Cookie
	HTTP    *http.Client
}

// FromSession returns a client logged in as the saved session.
func FromSession(s *Session) *Client {
	return &Client{Server: s.Server, Cookies: s.Cookies}
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Login logs in and returns the session to save.
func Login(ctx context.Context, server, email, password string) (*Session, error) {
	payload, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/v1/auth/login", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("login failed (%s): %s", resp.Status, string(body))
	}
	s := &Session{Server: server, Email: email}
	for _, ck := range resp.Cookies() {
		if ck.Value == "" || ck.MaxAge < 0 {
			continue
		}
		s.Cookies = append(s.Cookies, Cookie{Name: ck.Name, Value: ck.Value})
		if !ck.Expires.IsZero() && (s.Expires.IsZero() || ck.Expires.Before(s.Expires)) {
			s.Expires = ck.Expires
		}
	}
	if len(s.Cookies) == 0 {
		return nil, errors.New("the server accepted the login but set no session cookie")
	}
	return s, nil
}

// NoteRequest is the JSON body of POST /v1/notes (see the server-side patch,
// internal/app/notes.go there, for the matching shape).
type NoteRequest struct {
	AppName     string   `json:"app_name"`
	WindowTitle string   `json:"window_title"`
	Body        string   `json:"body"`
	Tags        []string `json:"tags"`       // always present, [] when there are none
	CreatedAt   int64    `json:"created_at"` // epoch seconds, same field the local DB uses
	UpdatedAt   int64    `json:"updated_at"` // epoch seconds: when the note was last edited here
}

// NoteResponse is what the server returns.
type NoteResponse struct {
	ID string `json:"id"`
}

// ErrRemoteGone means the server has no note with that id (it was deleted
// there, or the server was reset).
var ErrRemoteGone = errors.New("the server no longer has this note")

// call makes one authenticated request. body may be nil. It returns the
// response body of a 2xx answer.
func (c *Client) call(ctx context.Context, method, path string, body any, what string) ([]byte, int, error) {
	if len(c.Cookies) == 0 {
		return nil, 0, ErrNotLoggedIn
	}
	var rd io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Server+path, rd)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for _, ck := range c.Cookies {
		req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, resp.StatusCode, ErrNotLoggedIn
	case resp.StatusCode/100 != 2:
		return nil, resp.StatusCode, fmt.Errorf("%s failed (%s): %s", what, resp.Status, strings.TrimSpace(string(raw)))
	}
	return raw, resp.StatusCode, nil
}

// CreateNote sends one note. Returns the server's id for it.
func (c *Client) CreateNote(ctx context.Context, n NoteRequest) (string, error) {
	if n.Tags == nil {
		n.Tags = []string{} // "tags": [], not null
	}
	raw, _, err := c.call(ctx, http.MethodPost, "/v1/notes", n, "creating note")
	if err != nil {
		return "", err
	}
	var out NoteResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("saved remotely but the server's answer was not understood: %w", err)
	}
	return out.ID, nil
}

// UpdateNote replaces the server's copy of a note (app, title, text, tags;
// the creation time is not changed).
func (c *Client) UpdateNote(ctx context.Context, id string, n NoteRequest) error {
	if n.Tags == nil {
		n.Tags = []string{}
	}
	_, status, err := c.call(ctx, http.MethodPut, "/v1/notes/"+url.PathEscape(id), n, "updating note")
	if status == http.StatusNotFound {
		return ErrRemoteGone
	}
	return err
}

// RemoteNote is a note as the server returns it.
type RemoteNote struct {
	ID          string   `json:"id"`
	AppName     string   `json:"app_name"`
	WindowTitle string   `json:"window_title"`
	Body        string   `json:"body"`
	Tags        []string `json:"tags"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// NoteChange is one entry of the server's feed: a note as it now is, or the
// id of one that was deleted.
type NoteChange struct {
	Seq     uint64      `json:"seq"`
	Note    *RemoteNote `json:"note"`
	Deleted string      `json:"deleted"`
}

// NoteChanges is a page of the feed. Cursor is what to ask from next.
type NoteChanges struct {
	Changes []NoteChange `json:"changes"`
	Cursor  uint64       `json:"cursor"`
	More    bool         `json:"more"`
}

// ErrSyncUnsupported means the server has no change feed (it is older than
// two-way sync).
var ErrSyncUnsupported = errors.New("this server cannot send changes yet (it needs an upgrade for two-way sync)")

// NoteChangesSince asks what changed in the person's notes after cursor.
func (c *Client) NoteChangesSince(ctx context.Context, cursor uint64, limit int) (NoteChanges, error) {
	var out NoteChanges
	raw, status, err := c.call(ctx, http.MethodGet,
		fmt.Sprintf("/v1/sync/notes?since=%d&limit=%d", cursor, limit), nil, "fetching changes")
	if status == http.StatusNotFound {
		return out, ErrSyncUnsupported
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("the server's list of changes was not understood: %w", err)
	}
	return out, nil
}

// DeleteNote removes the server's copy. A note the server no longer has is
// not an error: the aim, that it be gone, is met.
func (c *Client) DeleteNote(ctx context.Context, id string) error {
	_, status, err := c.call(ctx, http.MethodDelete, "/v1/notes/"+url.PathEscape(id), nil, "deleting note")
	if status == http.StatusNotFound {
		return nil
	}
	return err
}
