package web

import (
	"io/fs"
	"testing"
)

func TestUIAlwaysHasIndex(t *testing.T) {
	ui, _ := UI()
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		t.Fatalf("index.html missing from embedded UI: %v", err)
	}
}
