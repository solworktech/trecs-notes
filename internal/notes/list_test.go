package notes

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSnippet(t *testing.T) {
	if got := Snippet("\n  \n\thello\tworld  \nsecond"); got != "hello world" {
		t.Errorf("got %q", got)
	}
	if got := Snippet(strings.Repeat("é", 300)); len([]rune(got)) != 200 {
		t.Errorf("not cut by runes: %d", len([]rune(got)))
	}
	if Snippet(" \n ") != "" {
		t.Error("blank note")
	}
}

func TestProblemText(t *testing.T) {
	if ProblemText(nil) != "" {
		t.Error("nil")
	}
	err := fmt.Errorf("%w: %v", ErrBadRegex, "missing closing )")
	if got := ProblemText(err); got != "not a regular expression yet: missing closing )" {
		t.Errorf("got %q", got)
	}
	if ProblemText(errors.New("disk full")) != "disk full" {
		t.Error("other errors pass through")
	}
}
