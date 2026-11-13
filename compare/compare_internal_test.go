package compare

// Internal: the engine gives no skill outputs yet, so they are fed in as it will give them.

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/simulator"
)

// Skill outputs are compared with the skill layer, not the tree-only one, and judged on the
// skill and full layers.
func TestSkillOutputs(t *testing.T) {
	t.Parallel()
	outputs := []simulator.Output{{Key: "player.life"}, {Key: "skill.average_hit"}}
	ms := &pob.MainSkill{ID: "Fireball", Name: "Fireball"}
	v := &pob.Values{
		TreeOnly: pob.Result{Outputs: map[string]float64{"Life": 1150}},
		Skill:    pob.Result{Outputs: map[string]float64{"Life": 1300, "AverageHit": 610, "Calcs.FireMin": 400}, MainSkill: ms},
		Full:     pob.Result{Outputs: map[string]float64{"Life": 2000, "AverageHit": 900}, MainSkill: ms},
	}
	tree := map[string]float64{"player.life": 1150, "skill.average_hit": 0}

	got, err := compareOutputs(outputs, ours{tree: tree}, v, "3.29", "01-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("without skill values: got mismatches %+v", got)
	}

	o := ours{tree: tree, skill: map[string]float64{"skill.average_hit": 600}, full: map[string]float64{"skill.average_hit": 900}}
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
