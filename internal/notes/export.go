package notes

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/solworktech/trecs-notes/internal/config"
	"github.com/solworktech/trecs-notes/internal/model"
)

// ErrNothingToExport is returned, and no file is made, when the search finds
// no notes.
var ErrNothingToExport = errors.New("there are no notes to export")

// ErrExportExists is returned when the file is there and overwriting was not
// asked for.
var ErrExportExists = errors.New("the file already exists")

// ExportName is the file name offered for an export made at t:
// notes_02_01_2006.csv, the day as day_month_year.
func ExportName(t time.Time) string { return t.Format("notes_02_01_2006") + ".csv" }

// ExportDir is where exports go unless the person says otherwise: the
// folder holding the database, $XDG_CONFIG_HOME/trecs/notes.
func ExportDir() (string, error) { return config.NotesDir() }

// DefaultExportPath is ExportDir/ExportName(t).
func DefaultExportPath(t time.Time) (string, error) {
	dir, err := ExportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ExportName(t)), nil
}

// ResolveExportPath turns what the person typed into the file to write: empty
// is the default; "~/" is the home folder; a folder (one that exists, or a
// path ending in a slash) gets the default file name; a name with no folder
// goes in the default folder.
func ResolveExportPath(input string, t time.Time) (string, error) {
	input = strings.TrimSpace(input)
	def, err := DefaultExportPath(t)
	if err != nil {
		return "", err
	}
	if input == "" {
		return def, nil
	}
	if input == "~" || strings.HasPrefix(input, "~/") || strings.HasPrefix(input, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot find your home folder: %w", err)
		}
		input = filepath.Join(home, input[1:])
	}
	asFolder := strings.HasSuffix(input, "/") || strings.HasSuffix(input, string(filepath.Separator))
	if !filepath.IsAbs(input) {
		input = filepath.Join(filepath.Dir(def), input)
	}
	if st, err := os.Stat(input); asFolder || (err == nil && st.IsDir()) {
		return filepath.Join(input, ExportName(t)), nil
	}
	return filepath.Clean(input), nil
}

// exportHeader is the first row of an export.
var exportHeader = []string{"id", "created", "updated", "app", "title", "tags", "text"}

// csvRecord is one note as a row. Times carry their offset (RFC 3339); tags
// are separated by spaces, which they cannot hold.
func csvRecord(n model.Note) []string {
	updated := n.UpdatedAt
	if updated.IsZero() {
		updated = n.CreatedAt
	}
	return []string{
		strconv.FormatInt(n.ID, 10),
		n.CreatedAt.Format(time.RFC3339),
		updated.Format(time.RFC3339),
		spreadsheetSafe(n.AppName),
		spreadsheetSafe(n.WindowTitle),
		strings.Join(n.Tags, " "),
		spreadsheetSafe(n.Text),
	}
}

// spreadsheetSafe stops a cell that starts with = + - @ (or a tab or
// carriage return) from being run as a formula when the file is opened in a
// spreadsheet: notes are text copied from anywhere, a web page included, and
// "=HYPERLINK(...)" is text too. A single quote in front makes it plain
// text.
func spreadsheetSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ExportCSV writes the notes that q finds, newest first, to path as CSV, and
// says how many. q's Limit is ignored: the person asked for what the search
// finds, not for the first page of it. An existing file is replaced only
// when overwrite is set, and then only once the export is complete: a
// failure leaves it as it was. The folder is made if it is not there. The
// file is readable by you alone.
func (s *Service) ExportCSV(ctx context.Context, q Query, path string, overwrite bool) (int, error) {
	if !overwrite {
		if _, err := os.Lstat(path); err == nil {
			return 0, fmt.Errorf("%s: %w", path, ErrExportExists)
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".notes-export-*")
	if err != nil {
		return 0, err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	w := csv.NewWriter(tmp)
	if err := w.Write(exportHeader); err != nil {
		return 0, err
	}
	q.Limit = 0
	count := 0
	var werr error
	err = s.SearchStream(ctx, q, func(n model.Note) bool {
		if werr = w.Write(csvRecord(n)); werr != nil {
			return false
		}
		count++
		return true
	})
	if err == nil {
		err = werr
	}
	w.Flush()
	if err == nil {
		err = w.Error()
	}
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, ErrNothingToExport
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		// on Windows this is what a file open in another program (Excel) gives
		return 0, fmt.Errorf("cannot write %s (is it open in another program?): %w", path, err)
	}
	done = true
	return count, nil
}

// ExportMessage is the sentence for a frontend to say after an export.
func ExportMessage(n int, path string) string {
	what := "1 note"
	if n != 1 {
		what = strconv.Itoa(n) + " notes"
	}
	return "Exported " + what + " to " + path + "."
}
