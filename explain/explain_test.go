// Package explain_test checks pbt-engine's explanations on the conformance builds: for every
// output of every build, the value the explanation shows is the value the simulation gives.
package explain_test

import (
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/dataset"
	pobimport "github.com/xaroth/pbt-engine/pob"
	"github.com/xaroth/pbt-engine/simulator"
)

func TestExplanationsAddUp(t *testing.T) {
	t.Parallel()
	datasets := os.Getenv("PBT_DATASETS")
	if datasets == "" {
		t.Skip("PBT_DATASETS not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := dataset.OpenRoot(os.DirFS(datasets))
	if reg == nil {
		t.Fatal(err)
	}
	game := "poe1"
	leagues := f.Games[game].Leagues
	for _, league := range slices.Sorted(maps.Keys(leagues)) {
		t.Run(league, func(t *testing.T) {
			t.Parallel()
			s, err := compare.OpenVersion(datasets, game, leagues[league].Dataset.Version)
			if err != nil {
				t.Fatal(err)
			}
			sim, err := simulator.New(s)
			if err != nil {
				t.Fatal(err)
			}
			dirs, err := pob.BuildDirs("..", league)
			if err != nil || len(dirs) == 0 {
				t.Fatalf("no builds: %v", err)
			}
			for _, dir := range dirs {
				t.Run(filepath.Base(dir), func(t *testing.T) {
					t.Parallel()
					checkBuild(t, reg, sim, dir)
				})
			}
		})
	}
}

func checkBuild(t *testing.T, reg *dataset.Registry, sim *simulator.Sim, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, pob.FullXML))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := pobimport.FromXML(data, reg)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sim.Simulate(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range st.Keys() {
		v, _ := st.Get(k)
		ex, err := st.Explain(k, simulator.Tree)
		if err != nil {
			t.Errorf("%s: %v", k, err)
			continue
		}
		if math.Float64bits(ex.Value) != math.Float64bits(v) {
			t.Errorf("%s: explained %v, got %v", k, ex.Value, v)
		}
	}
}
