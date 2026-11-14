package compare_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
)

func TestApply(t *testing.T) {
	t.Parallel()
	v := func(f float64) *float64 { return &f }
	ms := []compare.Mismatch{
		{League: "3.29", Build: "05-marauder-L90-juggernaut", Output: "player.life", Ours: 1150, PoB: 1166, HasPoB: true},
		{League: "3.28", Build: "05-marauder-L90-juggernaut", Output: "player.life", Ours: 1150, PoB: 1166, HasPoB: true},
		{League: "3.29", Build: "22-witch-L90-elementalist", Output: "player.mana", Ours: 600, PoB: 610, HasPoB: true},
		{League: "3.29", Build: "23-witch-L90-elementalist", Output: "player.mana", Ours: 601, PoB: 610, HasPoB: true},
		{League: "3.29", Build: "24-witch-L90-elementalist", Output: "player.armour", Ours: 5},
	}
	es := []compare.Entry{
		{ID: "life-deviation", Build: "*-marauder-*", Output: "player.life", League: "3.29", Ours: v(1150), PoB: v(1166)},
		{ID: "mana", Build: "22-*", Output: "player.mana", Ours: v(600)},
		// pinned to the old value 600: build 23's 601 does not match
		{ID: "mana-old-value", Build: "23-*", Output: "player.mana", Ours: v(600)},
		{ID: "armour-no-pob", Build: "24-*", Output: "player.armour", PoB: v(0)}, // PoB has no value: a PoB value never matches
		{ID: "templar", Build: "*-templar-*", Output: "player.life"},
	}
	stale := compare.Apply(ms, es)
	got := make([]string, 0, len(ms))
	for _, m := range ms {
		got = append(got, m.Triage)
	}
	if diff := cmp.Diff([]string{"life-deviation", "", "mana", "", ""}, got); diff != "" {
		t.Errorf("triage (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"mana-old-value", "armour-no-pob", "templar"}, stale); diff != "" {
		t.Errorf("stale (-want +got):\n%s", diff)
	}
}

func TestReadTriage(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, data, want string
	}{
		{"valid", `[{"id": "mana", "kind": "pending", "build": "*", "output": "player.mana", "reason": "r"}]`, ""},
		{"valid warning", `[{"id": "m", "kind": "pending", "build": "*", "output": "warning:unchecked-condition", "subject": "stat from *", "reason": "r"}]`, ""},
		{"catch-all", `[{"id": "all", "kind": "pending", "build": "*", "reason": "not modelled"}]`, "without an output"},
		{"warning without a subject", `[{"id": "m", "kind": "pending", "build": "*", "output": "warning:unchecked-condition", "reason": "r"}]`, "needs a subject"},
		{"subject on a value output", `[{"id": "m", "kind": "pending", "build": "*", "output": "player.mana", "subject": "x", "reason": "r"}]`, "needs a subject"},
		{"unknown field", `[{"id": "mana", "kind": "pending", "build": "*", "reason": "r", "x": 1}]`, "unknown field"},
		{
			"repeated id",
			`[{"id": "mana", "kind": "pending", "build": "*", "output": "player.mana", "reason": "r"},
			{"id": "mana", "kind": "pending", "build": "*", "output": "player.mana", "reason": "r"}]`,
			"missing or repeated",
		},
		{"bad kind", `[{"id": "mana", "kind": "maybe", "build": "*", "output": "player.mana", "reason": "r"}]`, `kind "maybe"`},
		{"no reason", `[{"id": "mana", "kind": "pending", "build": "*", "output": "player.mana"}]`, "no reason"},
		{"bad pattern", `[{"id": "mana", "kind": "pending", "build": "[", "output": "player.mana", "reason": "r"}]`, "build pattern"},
		{"gear layer", `[{"id": "m", "kind": "pending", "build": "*", "output": "player.mana", "layer": "gear", "reason": "r"}]`, ""},
		{"other layer", `[{"id": "m", "kind": "pending", "build": "*", "output": "player.mana", "layer": "full", "reason": "r"}]`, "layer"},
		{"layer on a warning", `[{"id": "m", "kind": "pending", "build": "*", "output": "warning:x", "subject": "s", "layer": "gear", "reason": "r"}]`, "layer"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := compare.ReadTriage(fstest.MapFS{compare.TriageFile: {Data: []byte(testCase.data)}})
			switch {
			case testCase.want == "" && err != nil:
				t.Errorf("got %v, want nil", err)
			case testCase.want != "" && (!errors.Is(err, compare.ErrTriage) || !strings.Contains(err.Error(), testCase.want)):
				t.Errorf("got %v, want %v containing %q", err, compare.ErrTriage, testCase.want)
			}
		})
	}
}

func TestApplyCases(t *testing.T) {
	t.Parallel()
	v := func(f float64) *float64 { return &f }
	ms := []compare.Mismatch{
		{League: "3.29", Build: "40-a", Output: "import:pob-override-ignored", Subject: "node 1"},
		// another tattoo on the same build: its own subject, not named by a case
		{League: "3.29", Build: "40-a", Output: "import:pob-override-ignored", Subject: "node 2"},
		{League: "3.29", Build: "40-a", Output: "player.life", Ours: 4000, PoB: 4100, HasPoB: true},
		// PoB's value tripled since the case was written: no longer accepted
		{League: "3.29", Build: "41-b", Output: "player.mana_regeneration", Ours: 10, PoB: 52.8, HasPoB: true},
		{League: "3.29", Build: "42-c", Output: "player.armour", Ours: 5},
		{League: "3.28", Build: "40-a", Output: "player.life", Ours: 4000, PoB: 4100, HasPoB: true},
	}
	es := []compare.Entry{{
		ID: "tattoos", League: "3.29", Cases: []compare.Case{
			{Build: "40-a", Output: "import:pob-override-ignored", Subject: "node 1"},
			{Build: "40-a", Output: "player.life", Ours: v(4000), PoB: v(4100)},
			{Build: "41-b", Output: "player.mana_regeneration", Ours: v(10), PoB: v(17.6)},
			{Build: "42-c", Output: "player.armour", Ours: v(5)},
		},
	}}
	stale := compare.Apply(ms, es)
	got := make([]string, 0, len(ms))
	for _, m := range ms {
		got = append(got, m.Triage)
	}
	if diff := cmp.Diff([]string{"tattoos", "", "tattoos", "", "tattoos", ""}, got); diff != "" {
		t.Errorf("triage (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"tattoos: 41-b player.mana_regeneration"}, stale); diff != "" {
		t.Errorf("stale (-want +got):\n%s", diff)
	}
}

func TestReadTriageCases(t *testing.T) {
	t.Parallel()
	const head = `[{"id": "tattoos", "kind": "pending", "reason": "r", `
	testCases := []struct {
		name, data, want string
	}{
		{"valid", head + `"cases": [{"build": "40-a", "output": "player.life", "ours": 1, "pob": 2},
			{"build": "40-a", "output": "import:pob-override-ignored", "subject": "node 1"}]}]`, ""},
		{"warning without a subject", head + `"cases": [{"build": "40-a", "output": "warning:unchecked-condition"}]}]`, "case"},
		{"subject on a value output", head + `"cases": [{"build": "40-a", "output": "player.life", "ours": 1, "subject": "x"}]}]`, "case"},
		{"no values on a value output", head + `"cases": [{"build": "40-a", "output": "player.life"}]}]`, "case"},
		{"values on an import output", head + `"cases": [{"build": "40-a", "output": "import:x", "subject": "s", "ours": 1}]}]`, "case"},
		{"no build", head + `"cases": [{"output": "player.life", "ours": 1}]}]`, "case"},
		{"repeated", head + `"cases": [{"build": "40-a", "output": "player.life", "ours": 1},
			{"build": "40-a", "output": "player.life", "ours": 2}]}]`, "case"},
		{"gear and tree case", head + `"cases": [{"build": "40-a", "output": "player.life", "ours": 1},
			{"build": "40-a", "output": "player.life", "layer": "gear", "ours": 1}]}]`, ""},
		{"other layer", head + `"cases": [{"build": "40-a", "output": "player.life", "layer": "skill", "ours": 1}]}]`, "case"},
		{"layer beside cases", head + `"layer": "gear", "cases": [{"build": "40-a", "output": "player.life", "ours": 1}]}]`, "beside cases"},
		{"pattern beside cases", head + `"build": "*", "output": "player.life", "cases": [{"build": "40-a", "output": "player.life", "ours": 1}]}]`, "beside cases"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := compare.ReadTriage(fstest.MapFS{compare.TriageFile: {Data: []byte(testCase.data)}})
			switch {
			case testCase.want == "" && err != nil:
				t.Errorf("got %v, want nil", err)
			case testCase.want != "" && (!errors.Is(err, compare.ErrTriage) || !strings.Contains(err.Error(), testCase.want)):
				t.Errorf("got %v, want %v containing %q", err, compare.ErrTriage, testCase.want)
			}
		})
	}
}

// An entry or case without a layer names the layer an output is compared on first; only gear
// mismatches name theirs, so a cause on the tree does not accept the gear build's values.
func TestApplyLayers(t *testing.T) {
	t.Parallel()
	v := func(f float64) *float64 { return &f }
	ms := []compare.Mismatch{
		{League: "3.29", Build: "40-a", Output: "player.life", Layer: compare.LayerTree, Ours: 4000, PoB: 4100, HasPoB: true},
		{League: "3.29", Build: "40-a", Output: "player.life", Layer: compare.LayerGear, Ours: 4000, PoB: 4100, HasPoB: true},
		{League: "3.29", Build: "40-a", Output: "player.reserved_mana", Layer: compare.LayerSkill, Ours: 10, PoB: 11, HasPoB: true},
		{League: "3.29", Build: "40-a", Output: "player.reserved_mana", Layer: compare.LayerGear, Ours: 10, PoB: 11, HasPoB: true},
		{League: "3.29", Build: "41-b", Output: "player.armour", Layer: compare.LayerGear, Ours: 5, PoB: 6, HasPoB: true},
	}
	es := []compare.Entry{
		{ID: "tree", Cases: []compare.Case{
			{Build: "40-a", Output: "player.life", Ours: v(4000), PoB: v(4100)},
			{Build: "40-a", Output: "player.reserved_mana", Ours: v(10), PoB: v(11)},
		}},
		{ID: "gear", Cases: []compare.Case{{Build: "40-a", Output: "player.life", Layer: compare.LayerGear, Ours: v(4000), PoB: v(4100)}}},
		{ID: "armour", Build: "*", Output: "player.armour", Layer: compare.LayerGear},
		{ID: "armour-tree", Build: "*", Output: "player.armour"},
	}
	stale := compare.Apply(ms, es)
	got := make([]string, 0, len(ms))
	for _, m := range ms {
		got = append(got, m.Triage)
	}
	if diff := cmp.Diff([]string{"tree", "gear", "tree", "", "armour"}, got); diff != "" {
		t.Errorf("triage (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"armour-tree"}, stale); diff != "" {
		t.Errorf("stale (-want +got):\n%s", diff)
	}
}
