package export_test

import (
	"reflect"
	"testing"

	"github.com/xaroth/pbt-conformance/export"
	"github.com/xaroth/pbt-conformance/pob"
)

func TestCompare(t *testing.T) {
	t.Parallel()
	skill := func(group int) *pob.MainSkill {
		return &pob.MainSkill{Group: group, ID: "Fireball", Name: "Fireball", Level: 20}
	}
	rec := pob.Result{
		Allocated: 100, MainSkill: skill(1),
		Outputs: map[string]float64{"Life": 5000, "TotalDPS": 10000, "CombinedDPS": 10000},
	}
	testCases := []struct {
		name   string
		export pob.Result
		want   []export.Diff
	}{
		{"equal", rec, nil},
		{
			"within 0.1%",
			pob.Result{Allocated: 100, MainSkill: skill(1), Outputs: map[string]float64{"Life": 5004, "TotalDPS": 10009, "CombinedDPS": 9991}},
			nil,
		},
		{
			"past 0.1%",
			pob.Result{Allocated: 100, MainSkill: skill(1), Outputs: map[string]float64{"Life": 5006, "TotalDPS": 10011, "CombinedDPS": 10000}},
			[]export.Diff{{Output: "player.life", Export: "5006", Record: "5000"}, {Output: "skill.hit_dps", Export: "10011", Record: "10000"}},
		},
		{
			"an output only one gives",
			pob.Result{Allocated: 100, MainSkill: skill(1), Outputs: map[string]float64{"TotalDPS": 10000, "CombinedDPS": 10000}},
			[]export.Diff{{Output: "player.life", Record: "5000"}},
		},
		{
			"nodes and main skill",
			pob.Result{Allocated: 99, MainSkill: skill(2), Outputs: rec.Outputs},
			[]export.Diff{
				{Output: export.AllocatedOutput, Export: "99", Record: "100"},
				{
					Output: export.MainSkillOutput,
					Export: `{"group":2,"id":"Fireball","name":"Fireball","level":20}`,
					Record: `{"group":1,"id":"Fireball","name":"Fireball","level":20}`,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := export.Compare(tc.export, rec); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
