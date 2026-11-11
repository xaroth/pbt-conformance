package compare_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pob"
)

func TestReadMeta(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, file string
		want       compare.Meta
		err        error
	}{
		{"absent", "", compare.Meta{Kind: compare.Untagged}, nil},
		{"kind", `{"kind": "minion"}`, compare.Meta{Kind: compare.KindMinion}, nil},
		{"contents", `{"kind": "attack", "contains": ["cluster-jewel", "item-skill"]}`,
			compare.Meta{Kind: compare.KindAttack, Contains: []compare.Content{compare.ClusterJewel, compare.ItemSkill}}, nil},
		{"smoke", `{"smoke": true}`, compare.Meta{Kind: compare.Smoke}, nil},
		{"smoke with a kind", `{"smoke": true, "kind": "attack"}`, compare.Meta{}, compare.ErrMeta},
		{"smoke with contents", `{"smoke": true, "contains": ["mercenary"]}`, compare.Meta{}, compare.ErrMeta},
		{"smoke is no kind to give", `{"kind": "smoke"}`, compare.Meta{}, compare.ErrMeta},
		{"no kind", `{"contains": ["mercenary"]}`, compare.Meta{}, compare.ErrMeta},
		{"untagged is no kind to give", `{"kind": "untagged"}`, compare.Meta{}, compare.ErrMeta},
		{"unknown content", `{"kind": "attack", "contains": ["flask"]}`, compare.Meta{}, compare.ErrMeta},
		{"repeated content", `{"kind": "attack", "contains": ["mercenary", "mercenary"]}`, compare.Meta{}, compare.ErrMeta},
		{"origin", `{"kind": "attack", "origin": {"url": "https://poe.ninja/x", "league": "Allflame", "snapshot": "0208"}}`,
			compare.Meta{Kind: compare.KindAttack, Origin: &compare.Origin{URL: "https://poe.ninja/x", League: "Allflame", Snapshot: "0208"}}, nil},
		{"unknown field", `{"kind": "attack", "source": "poe.ninja"}`, compare.Meta{}, compare.ErrMeta},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if testCase.file != "" {
				if err := os.WriteFile(filepath.Join(dir, compare.MetaFile), []byte(testCase.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := compare.ReadMeta(dir)
			if !errors.Is(err, testCase.err) {
				t.Fatalf("got %v, want %v", err, testCase.err)
			}
			if diff := cmp.Diff(testCase.want, got); diff != "" {
				t.Errorf("meta (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDetectKind(t *testing.T) {
	t.Parallel()
	dps := map[string]float64{"CombinedDPS": 100, "TotalDPS": 80}
	dot := map[string]float64{"CombinedDPS": 100, "TotalDPS": 50}
	tests := []struct {
		name string
		r    pob.Result
		want compare.Kind
	}{
		{"default attack", pob.Result{MainSkill: &pob.MainSkill{}}, compare.Smoke},
		{"no skill", pob.Result{}, compare.Smoke},
		{"totem", pob.Result{MainSkill: &pob.MainSkill{Group: 1, Totem: true, Attack: true}, Outputs: dps}, compare.KindTotem},
		{"mine", pob.Result{MainSkill: &pob.MainSkill{Group: 1, Mine: true}, Outputs: dps}, compare.KindTrapMine},
		{"minion", pob.Result{MainSkill: &pob.MainSkill{Group: 1, Minion: &pob.Minion{}}}, compare.KindMinion},
		{"no damage", pob.Result{MainSkill: &pob.MainSkill{Group: 1}}, compare.KindOther},
		{"damage over time", pob.Result{MainSkill: &pob.MainSkill{Group: 1, Attack: true}, Outputs: dot}, compare.KindDoT},
		{"attack", pob.Result{MainSkill: &pob.MainSkill{Group: 1, Attack: true}, Outputs: dps}, compare.KindAttack},
		{"spell", pob.Result{MainSkill: &pob.MainSkill{Group: 1}, Outputs: dps}, compare.KindSpell},
	}
	for _, tc := range tests {
		if got := compare.DetectKind(tc.r); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
