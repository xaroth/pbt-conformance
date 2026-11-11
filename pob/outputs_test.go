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

// dualWield is an attack with both hands, as PoB gives Viper Strike: per hand, never combined.
func dualWield() map[string]float64 {
	return map[string]float64{"MainHand.AverageHit": 756.78, "OffHand.AverageHit": 304.32, "AverageDamage": 901.935, "HitChance": 85}
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
		{"spell hit", "skill.average_hit", map[string]float64{"AverageHit": 600, "AverageDamage": 540}, 600, true},
		{"one hand", "skill.average_hit", map[string]float64{"MainHand.AverageHit": 12635, "AverageDamage": 9602}, 12635, true},
		{"off hand only", "skill.average_hit", map[string]float64{"OffHand.AverageHit": 300}, 300, true},
		{"both hands: no average", "skill.average_hit", dualWield(), 0, false},
		{"both hands: main", "skill.main_hand_average_hit", dualWield(), 756.78, true},
		{"both hands: off", "skill.off_hand_average_hit", dualWield(), 304.32, true},
		{"one hand: no per hand", "skill.main_hand_average_hit", map[string]float64{"MainHand.AverageHit": 12635}, 0, false},
		{"minion hand", "minion.average_hit", map[string]float64{"Minion.MainHand.AverageHit": 162.65, "MainHand.AverageHit": 1}, 162.65, true},
		{"hit chance", "skill.hit_chance", map[string]float64{"HitChance": 85}, 85, true},
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
