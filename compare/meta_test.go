package compare_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
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
		{"no kind", `{"contains": ["mercenary"]}`, compare.Meta{}, compare.ErrMeta},
		{"untagged is no kind to give", `{"kind": "untagged"}`, compare.Meta{}, compare.ErrMeta},
		{"unknown content", `{"kind": "attack", "contains": ["flask"]}`, compare.Meta{}, compare.ErrMeta},
		{"repeated content", `{"kind": "attack", "contains": ["mercenary", "mercenary"]}`, compare.Meta{}, compare.ErrMeta},
		{"unknown field", `{"kind": "attack", "origin": "poe.ninja"}`, compare.Meta{}, compare.ErrMeta},
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
