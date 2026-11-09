package model

import "testing"

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags(SplitTagInput("Ops, k8s  ops"))
	if err != nil || len(got) != 2 || got[0] != "ops" || got[1] != "k8s" {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range []string{"a", "Bad_Tag", "-x-", "x--y"} {
		if _, err := NormalizeTags([]string{bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
