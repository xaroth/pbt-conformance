package pob_test

import (
	"testing"

	"github.com/xaroth/pbt-conformance/pob"
)

func output(t *testing.T, name string) pob.Output {
	t.Helper()
	for _, o := range pob.Outputs() {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("no output %s", name)
	return pob.Output{}
}

func TestOutputsNamedOnce(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, o := range pob.Outputs() {
		if seen[o.Name] {
			t.Errorf("%s listed twice", o.Name)
		}
		seen[o.Name] = true
	}
}

func TestOutputValue(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, output string
		outputs      map[string]float64
		want         float64
		has          bool
	}{
		{"key", "player.life", map[string]float64{"Life": 1150}, 1150, true},
		{"key absent", "player.life", map[string]float64{"Mana": 50}, 0, false},
		{"key zero", "skill.hit_dps", map[string]float64{"TotalDPS": 0}, 0, true},
		{"sum", "player.maximum_fire_resistance", map[string]float64{"FireResist": -60, "MissingFireResist": 135}, 75, true},
		{"sum half", "player.maximum_fire_resistance", map[string]float64{"FireResist": -60}, 0, false},
		{"first", "skill.hits_per_second", map[string]float64{"HitSpeed": 0.75, "Speed": 2}, 0.75, true},
		{"first fallback", "skill.hits_per_second", map[string]float64{"Speed": 2}, 2, true},
		{"minion", "minion.life", map[string]float64{"Life": 1150, "Minion.Life": 7648}, 7648, true},
		{"totems", "delivery.total_dps", map[string]float64{"CombinedDPS": 150, "TotalDPS": 100, "TotemsSummoned": 3}, 350, true},
		{"trap", "delivery.total_dps", map[string]float64{"CombinedDPS": 150, "TotalDPS": 100}, 150, true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			o := output(t, testCase.output)
			got, has := o.Value(testCase.outputs)
			if has != testCase.has || (has && got != testCase.want) {
				t.Errorf("got %v, %v, want %v, %v", got, has, testCase.want, testCase.has)
			}
		})
	}
}
