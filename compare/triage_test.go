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
		{ID: "D1", Build: "*-marauder-*", Output: "player.life", League: "3.29", Ours: v(1150), PoB: v(1166)},
		{ID: "P1", Build: "22-*", Output: "player.mana", Ours: v(600)},
		// pinned to the old value 600: build 23's 601 does not match
		{ID: "P2", Build: "23-*", Output: "player.mana", Ours: v(600)},
		{ID: "P3", Build: "24-*", PoB: v(0)}, // PoB has no value: a PoB value never matches
		{ID: "P4", Build: "*-templar-*"},
	}
	stale := compare.Apply(ms, es)
	got := make([]string, 0, len(ms))
	for _, m := range ms {
		got = append(got, m.Triage)
	}
	if diff := cmp.Diff([]string{"D1", "", "P1", "", ""}, got); diff != "" {
		t.Errorf("triage (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"P2", "P3", "P4"}, stale); diff != "" {
		t.Errorf("stale (-want +got):\n%s", diff)
	}
}

func TestReadTriage(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, data, want string
	}{
		{"valid", `[{"id": "P1", "kind": "pending", "build": "*", "reason": "r"}]`, ""},
		{"unknown field", `[{"id": "P1", "kind": "pending", "build": "*", "reason": "r", "x": 1}]`, "unknown field"},
		{
			"repeated id",
			`[{"id": "P1", "kind": "pending", "build": "*", "reason": "r"}, {"id": "P1", "kind": "pending", "build": "*", "reason": "r"}]`,
			"missing or repeated",
		},
		{"bad kind", `[{"id": "P1", "kind": "maybe", "build": "*", "reason": "r"}]`, `kind "maybe"`},
		{"no reason", `[{"id": "P1", "kind": "pending", "build": "*"}]`, "no reason"},
		{"bad pattern", `[{"id": "P1", "kind": "pending", "build": "[", "reason": "r"}]`, "build pattern"},
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
