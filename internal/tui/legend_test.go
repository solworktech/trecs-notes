package tui

import "testing"

func TestFitLegendKeepsPriorityAndOrder(t *testing.T) {
	items := []legendItem{{"A", "alpha", 3}, {"B", "beta", 1}, {"C", "gamma", 2}}
	tail := []legendItem{{"F1", "help", 0}}
	// plenty of room: everything, in the order given
	shown, kept := fitLegendTail(items, tail, 200)
	if len(shown) != 3 || shown[0].key != "A" || shown[2].key != "C" || len(kept) != 1 {
		t.Fatalf("wide: %v %v", shown, kept)
	}
	// narrow: the help and the item of priority 1 only
	shown, kept = fitLegendTail(items, tail, len("F1 help")+len(legendGap)+len("B beta"))
	if len(shown) != 1 || shown[0].key != "B" || len(kept) != 1 {
		t.Fatalf("narrow: %v %v", shown, kept)
	}
	// too narrow even for the help: nothing
	if shown, kept = fitLegendTail(items, tail, 3); shown != nil || kept != nil {
		t.Fatalf("tiny: %v %v", shown, kept)
	}
}
