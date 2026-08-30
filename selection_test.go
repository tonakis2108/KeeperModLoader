package main

import "testing"

func TestValidSelectionAcceptsCurrentRow(t *testing.T) {
	if !validSelection(0, 1) {
		t.Fatal("expected the highlighted row to be accepted")
	}
	if !validSelection(1, 2) {
		t.Fatal("expected a valid non-first row to be accepted")
	}
}

func TestValidSelectionRejectsMissingOrStaleRow(t *testing.T) {
	for _, test := range []struct {
		name         string
		currentIndex int
		itemCount    int
	}{
		{name: "no current row", currentIndex: -1, itemCount: 1},
		{name: "empty model", currentIndex: 0, itemCount: 0},
		{name: "stale index", currentIndex: 1, itemCount: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validSelection(test.currentIndex, test.itemCount) {
				t.Fatal("expected selection to be rejected")
			}
		})
	}
}
