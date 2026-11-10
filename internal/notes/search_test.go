package notes

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/localdb"
	"github.com/solworktech/trecs-notes/internal/model"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := localdb.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := &Service{DB: db}
	day := func(d string) time.Time {
		x, _ := time.ParseInLocation("2006-01-02", d, time.Local)
		return x.Add(12 * time.Hour)
	}
	for _, n := range []model.Note{
		{AppName: "xterm", WindowTitle: "ssh prod", Text: "kubectl get pods -A", Tags: []string{"k8s", "ops"}, CreatedAt: day("2026-01-10")},
		{AppName: "Firefox", WindowTitle: "Go blog", Text: "Use context.WithCancel to stop work", Tags: []string{"go"}, CreatedAt: day("2026-02-10")},
		{AppName: "Firefox", WindowTitle: "Kubernetes docs", Text: "Pods are the smallest unit. Deploy with Kubectl apply", Tags: []string{"k8s"}, CreatedAt: day("2026-03-10")},
		{AppName: "Thunderbird", Text: "invoice #4521 due 2026-04-01", CreatedAt: day("2026-04-10")},
	} {
		n := n
		if err := db.Insert(&n); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func texts(ns []model.Note) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Text[:6])
	}
	return out
}

func TestSearchWordsAndFilters(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cases := []struct {
		q    Query
		want int
	}{
		{Query{}, 4},
		{Query{Text: "pods"}, 2},
		{Query{Text: "pod"}, 2}, // the last word may be a beginning
		{Query{Text: "pods smallest"}, 1},
		{Query{Text: `"smallest unit"`}, 1},
		{Query{Text: `"unit smallest"`}, 0},
		{Query{Text: "tag:k8s"}, 2},
		{Query{Text: "tag:k8s tag:ops"}, 1},
		{Query{Text: "tag:k8s pods app:firefox"}, 1},
		{Query{Text: "since:2026-03-01"}, 2},
		{Query{Text: "until:2026-02-10"}, 2}, // inclusive
		{Query{Text: "since:2026-02-01 until:2026-03-31"}, 2},
		{Query{Text: "firefox"}, 2},                  // app names are indexed
		{Query{Text: "kubernetes"}, 1},               // so are window titles
		{Query{Text: "kubectl"}, 2},                  // case-insensitive
		{Query{Text: "Kubectl"}, 1},                  // a capital makes it count
		{Query{Text: "kubectl", MatchCase: true}, 1}, // lowercase, case on
		{Query{Text: "-"}, 4},                        // nothing searchable: no restriction
		{Query{Text: "4521"}, 1},
		{Query{Text: "pods", Limit: 1}, 1},
	}
	for _, c := range cases {
		got, err := s.Search(ctx, c.q)
		if err != nil {
			t.Errorf("%+v: %v", c.q, err)
			continue
		}
		if len(got) != c.want {
			t.Errorf("%+v: got %d %v, want %d", c.q, len(got), texts(got), c.want)
		}
	}
	got, _ := s.Search(ctx, Query{Text: "pods"})
	if got[0].CreatedAt.Before(got[1].CreatedAt) {
		t.Error("not newest first")
	}
}

func TestSearchRegex(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cases := []struct {
		q    Query
		want int
	}{
		{Query{Text: `invoice #\d+`, Regex: true}, 1},
		{Query{Text: `kube\w+`, Regex: true}, 2}, // body and window title, any case
		{Query{Text: `Kube\w+`, Regex: true}, 1}, // a capital: case counts
		{Query{Text: `kubectl`, Regex: true, MatchCase: true}, 1},
		{Query{Text: `^Use `, Regex: true}, 1}, // ^ is a line start
		{Query{Text: `pod`, Regex: true}, 2},   // substring, not whole word
		{Query{Text: `pod tag:ops`, Regex: true}, 1},
		{Query{Text: `thunder`, Regex: true}, 1},
	}
	for _, c := range cases {
		got, err := s.Search(ctx, c.q)
		if err != nil || len(got) != c.want {
			t.Errorf("%+v: got %d err=%v, want %d", c.q, len(got), err, c.want)
		}
	}
	if _, err := s.Search(ctx, Query{Text: `(`, Regex: true}); !errors.Is(err, ErrBadRegex) {
		t.Errorf("bad regex: %v", err)
	}
	if _, err := s.Search(ctx, Query{Text: "since:yesterday"}); err == nil {
		t.Error("bad date accepted")
	}
}

func TestRegexCacheFollowsWrites(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	q := Query{Text: `zebra`, Regex: true}
	if got, _ := s.Search(ctx, q); len(got) != 0 {
		t.Fatal("unexpected hit")
	}
	n := model.Note{Text: "a zebra crossing"}
	if err := s.DB.Insert(&n); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Search(ctx, q); len(got) != 1 {
		t.Fatalf("cache stale after insert: %d", len(got))
	}
	n.Text = "a horse"
	if err := s.DB.Update(n); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Search(ctx, q); len(got) != 0 {
		t.Fatalf("cache stale after edit: %d", len(got))
	}
	_ = s.DB.Delete(n.ID)
}

func TestSearchCancel(t *testing.T) {
	s := testService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Search(ctx, Query{Text: "pod", Regex: true}); !errors.Is(err, context.Canceled) {
		t.Errorf("want cancel, got %v", err)
	}
}

func TestRegexFallsBackToScanWhenTooBig(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	s.cache.mu.Lock()
	s.cache.tooBig, s.cache.valid = true, true
	v, _ := s.DB.DataVersion(ctx)
	s.cache.version = v
	s.cache.mu.Unlock()
	got, err := s.Search(ctx, Query{Text: `pod`, Regex: true})
	if err != nil || len(got) != 2 {
		t.Fatalf("scan fallback: %d %v", len(got), err)
	}
}
