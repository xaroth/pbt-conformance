package pob_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

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

// The main skill and the outputs of its groups: a minion's are read from the minion, a totem's
// count is recorded.
func TestRunMainSkill(t *testing.T) {
	t.Parallel()
	res, err := pob.Run(context.Background(), paths(t), []string{abs(t, "marauder-zombie.xml"), abs(t, "marauder-totem.xml")})
	if err != nil {
		t.Fatal(err)
	}
	wantSkills := []*pob.MainSkill{
		{Group: 1, ID: "RaiseZombie", Name: "Raise Zombie", Minion: &pob.Minion{Name: "Raised Zombie", Skill: "Default Attack"}},
		{Group: 1, ID: "Fireball", Name: "Fireball", Totem: true},
	}
	for i, want := range wantSkills {
		if diff := cmp.Diff(want, res[i].MainSkill); diff != "" {
			t.Errorf("main skill (-want +got):\n%s", diff)
		}
	}
	wantKeys := [][]string{
		{"Life", "Minion.Life", "Minion.MainHand.AverageHit", "Minion.HitChance", "Minion.CombinedDPS"},
		{"TotemLife", "TotemsSummoned", "AverageHit", "HitChance", "TotalDPS", "CombinedDPS"},
	}
	for i, keys := range wantKeys {
		for _, k := range keys {
			if _, ok := res[i].Outputs[k]; !ok {
				t.Errorf("%s: no %s in %v", wantSkills[i].Name, k, res[i].Outputs)
			}
		}
	}
	if res[0].Outputs["Minion.Life"] == res[0].Outputs["Life"] {
		t.Errorf("minion life is the player's: %v", res[0].Outputs["Life"])
	}
	if _, ok := res[0].Outputs["TotemLife"]; ok {
		t.Errorf("totem life recorded for a minion skill")
	}
}

// Viper Strike hits with both hands: the average hit is recorded per hand, the hit chance
// combined as PoB gives it.
func TestRunDualWield(t *testing.T) {
	t.Parallel()
	file, err := filepath.Abs(filepath.Join("..", "builds", "3.29", "20-shadow-L90-assassin-chaos-inoculation", pob.FullXML))
	if err != nil {
		t.Fatal(err)
	}
	res, err := pob.Run(context.Background(), paths(t), []string{file})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range pob.Outputs() {
		if _, ok := o.Value(res[0].Outputs); ok {
			got[o.Name] = true
		}
	}
	want := map[string]bool{"skill.average_hit": false, "skill.main_hand_average_hit": true, "skill.off_hand_average_hit": true, "skill.hit_chance": true}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s given: got %v, want %v", name, got[name], w)
		}
	}
}

// A build run with an ally gets the ally's aura: Vitality's life regeneration reaches the
// life-node Marauder, who has none of his own, and the same on every run.
func TestRunJobsParty(t *testing.T) {
	t.Parallel()
	build, ally := abs(t, "marauder-life-node.xml"), abs(t, "marauder-vitality.xml")
	jobs := []pob.Job{{File: build}, {File: build, Ally: ally}, {File: build, Ally: ally}}
	res, err := pob.RunJobs(context.Background(), paths(t), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Outputs["LifeRegen"] != 0 {
		t.Errorf("alone: life regeneration %v, want 0", res[0].Outputs["LifeRegen"])
	}
	if res[1].Outputs["LifeRegen"] <= 0 {
		t.Errorf("with ally: life regeneration %v, want more than 0", res[1].Outputs["LifeRegen"])
	}
	if res[1].Outputs["LifeRegen"] != res[2].Outputs["LifeRegen"] {
		t.Errorf("runs differ: %v, %v", res[1].Outputs["LifeRegen"], res[2].Outputs["LifeRegen"])
	}
	if res[1].Outputs["Life"] != res[0].Outputs["Life"] {
		t.Errorf("life: got %v, want %v", res[1].Outputs["Life"], res[0].Outputs["Life"])
	}
}

// Two conformance builds as a stand-in pair: the Ascendant's Determination raises the Slayer's
// armour, the same in a second PoB process.
func TestRunJobsPartyStandIn(t *testing.T) {
	t.Parallel()
	p := paths(t)
	dir := func(name string) string {
		t.Helper()
		f, err := filepath.Abs(filepath.Join("..", "builds", "3.29", name, pob.FullXML))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	slayer, ascendant := dir("10-duelist-L90-slayer-resolute-technique"), dir("35-scion-L90-ascendant-chaos-inoculation")
	runs := make([]map[string]float64, 0, 2)
	for range 2 {
		res, err := pob.RunJobs(context.Background(), p, []pob.Job{{File: slayer}, {File: slayer, Ally: ascendant}})
		if err != nil {
			t.Fatal(err)
		}
		if res[1].Outputs["Armour"] <= res[0].Outputs["Armour"] {
			t.Errorf("armour with ally %v, alone %v", res[1].Outputs["Armour"], res[0].Outputs["Armour"])
		}
		runs = append(runs, res[1].Outputs)
	}
	if diff := cmp.Diff(runs[0], runs[1]); diff != "" {
		t.Errorf("runs differ (-first +second):\n%s", diff)
	}
}

func TestRunMissingFile(t *testing.T) {
	t.Parallel()
	_, err := pob.Run(context.Background(), paths(t), []string{abs(t, "missing.xml")})
	if !errors.Is(err, pob.ErrBuild) {
		t.Errorf("got %v, want %v", err, pob.ErrBuild)
	}
}
