package pi

import "testing"

// The chat renders live elapsed time for running tools, so every item
// must carry its entry timestamp.
func TestItemCarriesEntryTime(t *testing.T) {
	ix := testIndex(t)
	_, items, err := ix.Transcript("20261010_fixture", 100)
	if err != nil {
		t.Fatal(err)
	}
	var sawTime bool
	for _, it := range items {
		if it.Kind == "user" && it.Time > 0 {
			sawTime = true
		}
	}
	if !sawTime {
		t.Fatalf("no user item carries an entry time")
	}
}
