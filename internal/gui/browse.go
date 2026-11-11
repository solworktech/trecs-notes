package gui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/instance"
	"github.com/solworktech/trecs-notes/internal/model"
	"github.com/solworktech/trecs-notes/internal/notes"
	"github.com/solworktech/trecs-notes/internal/render"
	"github.com/solworktech/trecs-notes/internal/ui"
)

var browseOpen sync.Mutex

// openBrowse runs the browse window as a child process, as the other windows
// are, so that GTK stays out of the tray process. The database is shared.
func (a *app) openBrowse() {
	if !browseOpen.TryLock() {
		return // already open
	}
	defer browseOpen.Unlock()

	exe, err := os.Executable()
	if err != nil {
		log.Printf("browse: %v", err)
		return
	}
	cmd := exec.Command(exe, "browse")
	cmd.Stderr = os.Stderr
	if err := ui.RunChild(cmd); err != nil {
		if exitCodeOf(err) == 2 {
			log.Println("no window for browsing notes is available here; run trecs-notes -tui")
			return
		}
		log.Printf("browse: %v", err)
	}
}

// RunBrowse is `trecs-notes browse` (internal): the browse window. Exit 2 =
// no window available.
func RunBrowse() int {
	if path, err := config.LockPath("browse.lock"); err == nil {
		lock, err := instance.TryLock(path)
		if errors.Is(err, instance.ErrHeld) {
			fmt.Fprintf(os.Stderr, "The notes window is already open%s.\n", instance.Describe(path))
			return 0
		}
		if err == nil {
			defer lock.Unlock()
		}
	}
	svc, err := notes.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "browse:", err)
		return 1
	}
	defer func() {
		svc.Flush(5 * time.Second) // a change made just before closing still goes
		_ = svc.Close()
	}()

	stopSync := svc.StartAutoSync(time.Minute)
	defer stopSync() // deferred after the Flush above, so it runs first

	ctx := context.Background()
	var lastVersion int64
	h := ui.BrowseHandler{
		Search: func(ctx context.Context, text string, regex, matchCase bool, emit func(ui.BrowseRow) bool) (bool, string) {
			count, capped := 0, false
			err := svc.SearchStream(ctx, notes.Query{Text: text, Regex: regex, MatchCase: matchCase, Limit: notes.ListCap + 1},
				func(n model.Note) bool {
					if count++; count > notes.ListCap {
						capped = true
						return false
					}
					return emit(rowOf(svc, n))
				})
			if err != nil && !errors.Is(err, context.Canceled) {
				return false, notes.ProblemText(err)
			}
			return capped, ""
		},
		Get: func(id int64) (ui.BrowseNote, error) {
			n, err := svc.Get(id)
			if err != nil {
				return ui.BrowseNote{}, err
			}
			state, _ := svc.NoteStatus(n)
			return ui.BrowseNote{
				ID: n.ID, App: n.AppName, Title: n.WindowTitle, Tags: joinTags(n.Tags), Text: n.Text,
				Created: n.CreatedAt.Format("Mon 2 Jan 2006 15:04"), Sync: state, Markdown: render.LooksLikeMarkdown(n.Text),
			}, nil
		},
		Save: func(n ui.BrowseNote) (string, error) {
			old, err := svc.Get(n.ID)
			if err != nil {
				return "", err
			}
			old.AppName, old.WindowTitle, old.Text = n.App, n.Title, n.Text
			old.Tags = model.SplitTagInput(n.Tags)
			if err := svc.Update(old); err != nil {
				return "", err
			}
			return "Note saved.", nil
		},
		Delete: svc.Delete,
		Sync: func() (string, bool) {
			r := notes.SyncNow(svc.DB)
			return r.Message, r.OK
		},
		Status: svc.StatusLine,
		SyncOn: func() bool { return svc.Cfg.RemoteSyncEnabled },
		Changed: func() bool {
			v, err := svc.DB.DataVersion(ctx)
			if err != nil || v == lastVersion {
				return false
			}
			first := lastVersion == 0
			lastVersion = v
			return !first
		},
		Tags: func() []string {
			t, _ := svc.DB.AllTags()
			return t
		},
	}
	err = ui.ShowBrowse(ui.BrowseSize{Width: svc.Cfg.Width + 400, Height: svc.Cfg.Height + 120}, h)
	if ui.IsUnavailable(err) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "browse:", err)
		return 1
	}
	return 0
}

func rowOf(svc *notes.Service, n model.Note) ui.BrowseRow {
	mark := ""
	if svc.Unsent(n) {
		mark = "↑"
	}
	return ui.BrowseRow{
		ID: n.ID, Mark: mark, Date: n.CreatedAt.Format("2006-01-02 15:04"),
		App: n.AppName, Snippet: notes.Snippet(n.Text),
	}
}

func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += ", "
		}
		out += t
	}
	return out
}
