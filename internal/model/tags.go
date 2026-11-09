// Package model holds the shapes shared by every part of trecs-notes: the
// note itself, and its tags.
package model

import (
	"fmt"
	"regexp"
	"strings"
)

// Tag rules. These intentionally mirror trecs-server's recording tags
// (internal/recording/tags.go: MinTagLen, MaxTagLen, MaxTags and the same
// pattern) so a tag that is valid here is also valid once synced to the
// trecs API, and the other way around - the user never discovers a mismatch
// only after a sync fails.
const (
	MinTagLen = 2
	MaxTagLen = 32
	MaxTags   = 10
)

var tagPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// NormalizeTags checks a list of tags and returns it as it is kept: trimmed,
// lowercased, without repeats, in the order given. An unacceptable tag, or
// too many of them (after repeats are removed), is reported by name.
// nil and [] both mean no tags.
func NormalizeTags(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue // a blank entry from a comma-separated field is not an error, just skipped
		}
		switch {
		case len(t) < MinTagLen || len(t) > MaxTagLen:
			return nil, fmt.Errorf("the tag %q must be %d to %d characters", t, MinTagLen, MaxTagLen)
		case !tagPattern.MatchString(t):
			return nil, fmt.Errorf("the tag %q may only have lowercase letters, digits and single hyphens between them", t)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("a note may have at most %d tags", MaxTags)
	}
	return out, nil
}

// SplitTagInput turns what the capture dialog's free-text tags field holds -
// comma or whitespace separated - into a slice ready for NormalizeTags.
func SplitTagInput(raw string) []string {
	raw = strings.ReplaceAll(raw, ",", " ")
	return strings.Fields(raw)
}
