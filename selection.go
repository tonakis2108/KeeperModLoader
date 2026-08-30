package main

// validSelection keeps the single-selection game list's current-row handling
// testable outside the Windows UI build. A Walk ListBox can visually highlight
// CurrentIndex while SelectedIndexes remains empty when MultiSelection is false.
func validSelection(currentIndex, itemCount int) bool {
	return currentIndex >= 0 && currentIndex < itemCount
}
