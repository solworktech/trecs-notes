package notes

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

var day = time.Date(2026, time.November, 7, 16, 43, 0, 0, time.UTC)

func TestExportNameIsDayMonthYear(t *testing.T) {
	if got := ExportName(day); got != "notes_07_11_2026.csv" {
		t.Errorf("got %q", got)
	}
}

func TestResolveExportPath(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // where Windows looks for it
	dir := filepath.Join(cfg, "trecs", "notes")
	folder := t.TempDir()
	for _, c := range []struct{ in, want string }{
		{"", filepath.Join(dir, "notes_07_11_2026.csv")},
		{"  ", filepath.Join(dir, "notes_07_11_2026.csv")},
		{"mine.csv", filepath.Join(dir, "mine.csv")},
		{"~/out/n.csv", filepath.Join(home, "out", "n.csv")},
		{"~/", filepath.Join(home, "notes_07_11_2026.csv")},
		{folder, filepath.Join(folder, "notes_07_11_2026.csv")},
		{folder + "/new/", filepath.Join(folder, "new", "notes_07_11_2026.csv")},
		{filepath.Join(folder, "x.csv"), filepath.Join(folder, "x.csv")},
	} {
		got, err := ResolveExportPath(c.in, day)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestFormulasAreMadePlain(t *testing.T) {
	for in, want := range map[string]string{
		"=HYPERLINK(\"http://x\")": "'=HYPERLINK(\"http://x\")",
		"+1":                       "'+1",
		"-rw-r--r--":               "'-rw-r--r--",
		"@SUM(A1)":                 "'@SUM(A1)",
		"\tx":                      "'\tx",
		"plain = text":             "plain = text",
		"":                         "",
	} {
		if got := spreadsheetSafe(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestARecordIsOneRowWhateverTheTextHolds(t *testing.T) {
	n := model.Note{ID: 7, AppName: "Terminal", WindowTitle: "a, \"b\"", Text: "line 1\nline 2, \"quoted\"\r\nend",
		Tags: []string{"go", "ops-notes"}, CreatedAt: day, UpdatedAt: day.Add(time.Hour)}
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(csvRecord(n))
	w.Flush()
	rows, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil || len(rows) != 1 || len(rows[0]) != len(exportHeader) {
		t.Fatalf("%v %q", err, rows)
	}
	r := rows[0]
	if r[0] != "7" || r[1] != "2026-11-07T16:43:00Z" || r[2] != "2026-11-07T17:43:00Z" || r[5] != "go ops-notes" {
		t.Errorf("%q", r)
	}
	if r[4] != n.WindowTitle || r[6] != strings.ReplaceAll(n.Text, "\r\n", "\n") && r[6] != n.Text {
		t.Errorf("title/text came back as %q / %q", r[4], r[6])
	}
}

func exportService(t *testing.T) *Service {
	t.Helper()
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Service{DB: db}
}

func TestExportCSVWritesWhatTheSearchFinds(t *testing.T) {
	s := exportService(t)
	for _, n := range []model.Note{
		{AppName: "Firefox", Text: "kubectl get pods", Tags: []string{"k8s"}},
		{AppName: "Terminal", Text: "docker compose up -d", Tags: []string{"docker"}},
		{AppName: "Firefox", Text: "=cmd|' /C calc'!A0"},
	} {
		if err := s.Save(n); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "sub", "n.csv")
	n, err := s.ExportCSV(context.Background(), Query{Text: "app:firefox"}, out, false)
	if err != nil || n != 2 {
		t.Fatalf("%d, %v", n, err)
	}
	// read it whole and let go of it: Windows will not replace a file that is open
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) != 3 || rows[0][0] != "id" {
		t.Fatalf("%v %q", err, rows)
	}
	for _, r := range rows[1:] {
		if r[3] != "Firefox" {
			t.Errorf("a note from %q was exported", r[3])
		}
		if strings.HasPrefix(r[6], "=") {
			t.Errorf("formula left live: %q", r[6])
		}
	}
	if st, _ := os.Stat(out); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 { // Windows has no such bits
		t.Errorf("mode %v", st.Mode().Perm())
	}

	// not over a file that is there, unless asked
	if _, err := s.ExportCSV(context.Background(), Query{}, out, false); !errors.Is(err, ErrExportExists) {
		t.Errorf("got %v", err)
	}
	if n, err := s.ExportCSV(context.Background(), Query{}, out, true); err != nil || n != 3 {
		t.Errorf("overwrite: %d %v", n, err)
	}
	// nothing found: no file, and the old one is not touched
	other := filepath.Join(t.TempDir(), "none.csv")
	if _, err := s.ExportCSV(context.Background(), Query{Text: "app:nosuchapp"}, other, false); !errors.Is(err, ErrNothingToExport) {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Error("an empty export made a file")
	}
	if ents, _ := os.ReadDir(filepath.Dir(other)); len(ents) != 0 {
		t.Errorf("left behind: %v", ents)
	}
}

func TestExportIgnoresTheListCap(t *testing.T) {
	s := exportService(t)
	if err := s.Save(model.Note{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "n.csv")
	if n, err := s.ExportCSV(context.Background(), Query{Limit: 1}, out, false); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
