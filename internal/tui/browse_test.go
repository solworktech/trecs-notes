package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/notes"
)

// newTestApp builds the main page on a simulated screen, with a few notes.
func newTestApp(t *testing.T) *app {
	t.Helper()
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, n := range []model.Note{
		{AppName: "xterm", Text: "kubectl get pods", Tags: []string{"k8s"}, CreatedAt: time.Unix(1700000000, 0)},
		{AppName: "Firefox", WindowTitle: "Kubernetes docs", Text: "pods are units", CreatedAt: time.Unix(1700000100, 0)},
		{AppName: "Mail", Text: "invoice #4521", CreatedAt: time.Unix(1700000200, 0)},
	} {
		n := n
		if err := db.Insert(&n); err != nil {
			t.Fatal(err)
		}
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(110, 30)
	a := &app{svc: &notes.Service{DB: db}, tv: tview.NewApplication().SetScreen(screen)}
	a.status = tview.NewTextView()
	a.browse = newBrowser(a)
	a.pages = tview.NewPages().AddPage(mainPage, a.browse.primitive(), true, true)
	a.edit = newEditModal(a)
	a.tv.SetRoot(a.pages, true)
	done := make(chan struct{})
	go func() { _ = a.tv.Run(); close(done) }()
	t.Cleanup(func() { a.tv.Stop(); <-done })
	return a
}

// onScreen runs fn on the screen's goroutine and waits for it.
func onScreen[T any](a *app, fn func() T) T {
	ch := make(chan T, 1)
	a.tv.QueueUpdate(func() { ch <- fn() })
	return <-ch
}

func waitRows(t *testing.T, a *app, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := onScreen(a, func() int { return len(a.browse.rows) })
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("list has %d rows, want %d", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBrowseSearchAndSwitches(t *testing.T) {
	a := newTestApp(t)
	onScreen(a, func() int { a.browse.searchNow(); return 0 })
	waitRows(t, a, 3)

	onScreen(a, func() int { a.browse.find.SetText("pods"); return 0 })
	waitRows(t, a, 2)

	// the regression: the switch must be on by the time the search reads it
	onScreen(a, func() int { a.browse.find.SetText(`kube\w+`); return 0 })
	waitRows(t, a, 0)
	onScreen(a, func() int { a.browse.regex.SetChecked(true); return 0 })
	waitRows(t, a, 2)

	onScreen(a, func() int { a.browse.find.SetText("tag:k8s"); a.browse.regex.SetChecked(false); return 0 })
	waitRows(t, a, 1)
	n := onScreen(a, func() model.Note { n, _ := a.browse.current(); return n })
	if n.AppName != "xterm" {
		t.Errorf("selected %+v", n)
	}
}

func TestBrowseEditAndDelete(t *testing.T) {
	a := newTestApp(t)
	onScreen(a, func() int { a.browse.searchNow(); return 0 })
	waitRows(t, a, 3)
	n := onScreen(a, func() model.Note { n, _ := a.browse.current(); return n })
	if n.AppName != "Mail" {
		t.Fatalf("newest first: got %q", n.AppName)
	}

	n.Text = "invoice #9999"
	if err := a.svc.Update(n); err != nil {
		t.Fatal(err)
	}
	onScreen(a, func() int { a.browse.afterChange(n.ID); return 0 })
	var cur model.Note
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		cur = onScreen(a, func() model.Note { c, _ := a.browse.current(); return c })
		if cur.Text == "invoice #9999" {
			break
		}
	}
	if cur.ID != n.ID || cur.Text != "invoice #9999" {
		t.Errorf("after edit: %+v", cur)
	}

	if err := a.svc.Delete(n.ID); err != nil {
		t.Fatal(err)
	}
	onScreen(a, func() int { a.browse.afterChange(0); return 0 })
	waitRows(t, a, 2)

	if err := a.svc.Update(model.Note{ID: cur.ID, Text: "  "}); err != notes.ErrEmptyNote {
		t.Errorf("empty text: %v", err)
	}
}
