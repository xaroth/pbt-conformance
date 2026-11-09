package compare_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/config"
	"github.com/xaroth/pbt-engine/dataset"
	"github.com/xaroth/pbt-engine/rules/poe1"
)

// env returns the variable or skips the test when it is not set.
func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skip(name + " not set")
	}
	return v
}

func repoPins(t *testing.T) *pins.File {
	t.Helper()
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "marauder-life-node.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func store329(t *testing.T) *dataset.Store {
	t.Helper()
	s, err := dataset.Open(os.DirFS(filepath.Join(env(t, "PBT_DATASETS"), "3.29.3.3")))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFromPoB(t *testing.T) {
	t.Parallel()
	s := store329(t)
	b, err := compare.FromPoB(fixture(t), "poe1", "3.29.3.3", "3_29", s)
	if err != nil {
		t.Fatal(err)
	}
	stage, _, _ := config.Get(b.Config, poe1.Campaign)
	bandits, _, _ := config.Get(b.Config, poe1.Bandits)
	if b.Class != "Metadata/Characters/Str/Str" || b.Level != 90 || len(b.Nodes) != 2 ||
		stage != poe1.CampaignComplete || bandits != poe1.BanditsKillAll || len(b.Masteries) != 0 {
		t.Errorf("got %+v", b)
	}
	jugg := strings.Replace(string(fixture(t)), `ascendClassName="None"`, `ascendClassName="Juggernaut"`, 1)
	penalty := `<Config activeConfigSet="1"><ConfigSet id="1"><Input name="resistancePenalty" number="-30"/></ConfigSet></Config>`
	jugg = strings.Replace(jugg, `<Tree`, penalty+`<Tree`, 1)
	if b, err = compare.FromPoB([]byte(jugg), "poe1", "3.29.3.3", "3_29", s); err != nil {
		t.Fatal(err)
	}
	stage, _, _ = config.Get(b.Config, poe1.Campaign)
	if b.Ascendancy != "Juggernaut" || stage != poe1.CampaignActs6To10 {
		t.Errorf("ascendancy and campaign: got %q, %q", b.Ascendancy, stage)
	}
	withMasteries := strings.Replace(string(fixture(t)), `<Spec `, `<Spec masteryEffects="{4139,47642},{12382,64381}" `, 1)
	if b, err = compare.FromPoB([]byte(withMasteries), "poe1", "3.29.3.3", "3_29", s); err != nil {
		t.Fatal(err)
	}
	if len(b.Masteries) != 2 || b.Masteries[4139] != 47642 || b.Masteries[12382] != 64381 {
		t.Errorf("masteries: got %v", b.Masteries)
	}
	for name, data := range map[string]string{
		"other tree":  strings.Replace(string(fixture(t)), "3_29", "3_28", 1),
		"bad class":   strings.Replace(string(fixture(t)), `className="Marauder"`, `className="Barbarian"`, 1),
		"bad node":    strings.Replace(string(fixture(t)), "31628", "x", 1),
		"bad bandit":  strings.Replace(string(fixture(t)), `bandit="None"`, `bandit="Eramir"`, 1),
		"bad mastery": strings.Replace(string(fixture(t)), `<Spec `, `<Spec masteryEffects="{4139}" `, 1),
		"not xml":     "<",
	} {
		if _, err := compare.FromPoB([]byte(data), "poe1", "3.29.3.3", "3_29", s); !errors.Is(err, compare.ErrPoBXML) {
			t.Errorf("%s: got %v, want %v", name, err, compare.ErrPoBXML)
		}
	}
}

// TestRun records PoB's values for the fixture and compares: a Marauder with one life node
// has no mismatches.
func TestRun(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	cache := env(t, "PBTC_CACHE")
	f := repoPins(t)
	root := t.TempDir()
	dir := filepath.Join(root, "builds", "3.29", "01-marauder-life-node")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{pob.FullXML, pob.TreeOnlyXML} {
		if err := os.WriteFile(filepath.Join(dir, name), fixture(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pob.Record(context.Background(), root, cache, f, "poe1", "3.29", false); err != nil {
		t.Fatal(err)
	}
	ms, err := compare.Run(root, datasets, f, "poe1", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("mismatches: %+v", ms)
	}
	if _, err := compare.Run(root, t.TempDir(), f, "poe1", "3.29"); !errors.Is(err, compare.ErrDataset) {
		t.Errorf("missing dataset: got %v, want %v", err, compare.ErrDataset)
	}
}

func TestAllocatedCountsStarts(t *testing.T) {
	t.Parallel()
	s := store329(t)
	b, err := compare.FromPoB(fixture(t), "poe1", "3.29.3.3", "3_29", s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ascendancy string
		nodes            []uint32
		want             int
	}{
		{"class start only", "", nil, 1},
		{"with ascendancy", "Juggernaut", nil, 2},
		{"start listed, not counted twice", "", b.Nodes, len(b.Nodes)},
	} {
		nb := *b
		nb.Ascendancy, nb.Nodes = tc.ascendancy, tc.nodes
		c, _, err := build.Check(&nb, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := compare.Allocated(c); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
