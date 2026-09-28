package identity

import "testing"

func TestNewDemoKeyChangesBetweenRuns(t *testing.T) {
	first, err := NewDemoKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDemoKey()
	if err != nil {
		t.Fatal(err)
	}
	if first.Equals(second) {
		t.Fatal("two demo runs reused the same key")
	}
}
