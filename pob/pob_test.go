package pob_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"
)

// paths returns the runtime of the current poe1 league in $PBTC_CACHE, set up by pbtc setup,
// and skips the test when the variable is not set.
func paths(t *testing.T) runtime.Paths {
	t.Helper()
	cache := os.Getenv("PBTC_CACHE")
	if cache == "" {
		t.Skip("PBTC_CACHE not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	g := f.Games["poe1"]
	return runtime.Locate(cache, f.Runtime, g.Leagues[g.Current].PoB)
}

func abs(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	t.Parallel()
	file := abs(t, "marauder-life-node.xml")
	res, err := pob.Run(context.Background(), paths(t), []string{file, file})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	r := res[0]
	// class start and one life node; 38 + 12 x 90 + 16 from the node + 32 strength / 2
	want := map[string]float64{"Life": 1150, "Str": 32, "FireResist": -60, "MissingFireResist": 135}
	for k, v := range want {
		if r.Outputs[k] != v {
			t.Errorf("%s: got %v, want %v", k, r.Outputs[k], v)
		}
	}
	if r.Allocated != 2 {
		t.Errorf("allocated: got %d, want 2", r.Allocated)
	}
}

func TestRunMissingFile(t *testing.T) {
	t.Parallel()
	_, err := pob.Run(context.Background(), paths(t), []string{abs(t, "missing.xml")})
	if !errors.Is(err, pob.ErrBuild) {
		t.Errorf("got %v, want %v", err, pob.ErrBuild)
	}
}
