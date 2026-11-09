package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xaroth/pbt-conformance/pins"
)

var errInterrupted = errors.New("interrupted")

func TestLocate(t *testing.T) {
	t.Parallel()
	rt := pins.Runtime{LuaJIT: pins.Source{Commit: "aa"}, LuaUTF8: pins.Source{Commit: "bb"}}
	got := Locate("/c", rt, pins.Release{Tag: "v1", Commit: "cc"})
	want := Paths{LuaJIT: "/c/luajit/aa/bin/luajit", CPath: "/c/luautf8/aa-bb/?.so", PoBSrc: "/c/pob/cc/src"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if rel := Locate("c", rt, pins.Release{Commit: "cc"}); rel.LuaJIT != filepath.Join(wd, "c/luajit/aa/bin/luajit") {
		t.Errorf("relative cache: got %s, want it under %s", rel.LuaJIT, wd)
	}
}

func TestPartRedoesUnfinished(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "p")
	builds := 0
	build := func(d string) error {
		builds++
		return os.WriteFile(filepath.Join(d, "x"), nil, filePerm)
	}
	if err := part(dir, func(string) error { return errInterrupted }); err == nil {
		t.Fatal("failed build: got nil error")
	}
	for range 2 {
		if err := part(dir, build); err != nil {
			t.Fatal(err)
		}
	}
	if builds != 1 {
		t.Errorf("got %d builds, want 1: the failed one redone, the finished one kept", builds)
	}
}

// TestSetup builds the repo's pinned runtime in $PBTC_CACHE, given relative to the working
// directory, and checks each PoB release against its tree; skipped when the variable is not
// set, since it needs network, git, make and cc.
func TestSetup(t *testing.T) {
	t.Parallel()
	cache := os.Getenv("PBTC_CACHE")
	if cache == "" {
		t.Skip("PBTC_CACHE not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := Setup(context.Background(), rel, f); err != nil {
		t.Fatal(err)
	}
	l := f.Games["poe1"].Leagues[f.Games["poe1"].Current]
	err = checkTree(context.Background(), Locate(rel, f.Runtime, l.PoB), "2_6")
	if !errors.Is(err, ErrWrongTree) {
		t.Errorf("wrong tree: got %v, want %v", err, ErrWrongTree)
	}
}
