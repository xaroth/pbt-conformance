package compare

// Internal: the engine gives no skill outputs yet, so they are fed in as it will give them.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/simulator"
)

// Skill outputs are compared with the skill layer, not the tree-only one, and judged on the
// skill and full layers. An output with no PoB key is not compared.
func TestSkillOutputs(t *testing.T) {
	t.Parallel()
	outputs := []simulator.Output{{Key: "player.life"}, {Key: "skill.average_hit"}, {Key: "skill.physical_damage_modifier"}}
	ms := &pob.MainSkill{ID: "Fireball", Name: "Fireball"}
	v := &pob.Values{
		TreeOnly: pob.Result{Outputs: map[string]float64{"Life": 1150}},
		Skill:    pob.Result{Outputs: map[string]float64{"Life": 1300, "AverageHit": 610, "Calcs.FireMin": 400}, MainSkill: ms},
		Full:     pob.Result{Outputs: map[string]float64{"Life": 2000, "AverageHit": 900}, MainSkill: ms},
	}
	tree := map[string]float64{"player.life": 1150, "skill.average_hit": 0, "skill.physical_damage_modifier": 1}

	got, err := compareOutputs(outputs, ours{tree: tree}, v, "3.29", "01-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("without skill values: got mismatches %+v", got)
	}

	o := ours{
		tree:  tree,
		skill: map[string]float64{"skill.average_hit": 600, "skill.physical_damage_modifier": 1},
		full:  map[string]float64{"skill.average_hit": 900},
	}
	got, err = compareOutputs(outputs, o, v, "3.29", "01-x")
	if err != nil {
		t.Fatal(err)
	}
	want := []Mismatch{{League: "3.29", Build: "01-x", Output: "skill.average_hit", Ours: 600, PoB: 610, PoBFull: 900, HasPoB: true}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatches (-want +got):\n%s", diff)
	}

	ls := judgeLayers(v, o)
	notCompared := GroupResult{Group: pob.GroupSheet, Status: StatusNotCompared}
	wantLayers := map[string][]GroupResult{
		LayerSkill: {notCompared, {Group: pob.GroupSkill, Status: StatusMismatch}, {Group: pob.GroupHitTypes, Status: StatusNotCompared}},
		LayerFull:  {notCompared, {Group: pob.GroupSkill, Status: StatusPass}},
	}
	for _, l := range ls {
		if w, ok := wantLayers[l.Name]; ok {
			if diff := cmp.Diff(w, l.Groups); diff != "" {
				t.Errorf("%s layer (-want +got):\n%s", l.Name, diff)
			}
		}
	}
}

// An output with no PoB key fails the comparison unless it is a skill output, so a new sheet or
// defence output cannot go unchecked.
func TestUnmappedOutput(t *testing.T) {
	t.Parallel()
	v := &pob.Values{TreeOnly: pob.Result{Outputs: map[string]float64{"Life": 1150}}}
	o := ours{tree: map[string]float64{"player.life": 1150, "player.not_mapped": 1}}
	_, err := compareOutputs([]simulator.Output{{Key: "player.life"}, {Key: "player.not_mapped"}}, o, v, "3.29", "01-x")
	if !errors.Is(err, errNoKey) {
		t.Errorf("got %v, want %v", err, errNoKey)
	}
}

// An output the engine does not give for a build, such as one of a main skill, is left out
// rather than failing the build, so it is judged as missing.
func TestEngineValuesLeavesOutMissing(t *testing.T) {
	t.Parallel()
	datasets := os.Getenv("PBT_DATASETS")
	if datasets == "" {
		t.Skip("PBT_DATASETS not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	reg, s, err := openPinned(datasets, "poe1", f.Games["poe1"].Leagues["3.29"].Dataset)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := simulator.New(s)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := importFile(filepath.Join("..", "testdata"), "marauder-life-node.xml", reg)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sim.Simulate(b)
	if err != nil {
		t.Fatal(err)
	}
	got := engineValues([]simulator.Output{{Key: "player.life"}, {Key: "skill.not_given"}}, st)
	if _, ok := got["player.life"]; !ok || len(got) != 1 {
		t.Errorf("got %v, want player.life alone", got)
	}
}
