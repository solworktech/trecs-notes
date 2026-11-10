package remote

import (
	"testing"
	"time"

	"github.com/solworktech/trecs-notes/internal/model"
)

func TestRequestCarriesEditTime(t *testing.T) {
	n := model.Note{Text: "x", CreatedAt: time.Unix(100, 0)}
	if r := request(n); r.UpdatedAt != 100 {
		t.Fatalf("a note with no edit time was last edited when saved: %d", r.UpdatedAt)
	}
	n.UpdatedAt = time.Unix(250, 0)
	if r := request(n); r.CreatedAt != 100 || r.UpdatedAt != 250 {
		t.Fatalf("%+v", r)
	}
}
