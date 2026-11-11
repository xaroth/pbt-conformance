package compare_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pob"
)

// dotShare is the share of PoB's combined damage per second above which a build's kind is
// damage over time.
const dotShare = 0.5

// recordedKinds returns the kinds the README's rule allows for a build recorded as v: all but
// spell and attack follow from the recording; those two depend on the skill.
func recordedKinds(v *pob.Values) []compare.Kind {
	ms := v.Full.MainSkill
	o := v.Full.Outputs
	switch {
	case ms == nil || ms.Group == 0:
		return []compare.Kind{compare.Smoke}
	case ms.Totem:
		return []compare.Kind{compare.KindTotem}
	case ms.Trap || ms.Mine:
		return []compare.Kind{compare.KindTrapMine}
	case ms.Minion != nil:
		return []compare.Kind{compare.KindMinion}
	case o["CombinedDPS"] == 0:
		return []compare.Kind{compare.KindOther}
	case (o["CombinedDPS"]-o["TotalDPS"])/o["CombinedDPS"] >= dotShare:
		return []compare.Kind{compare.KindDoT}
	}
	return []compare.Kind{compare.KindSpell, compare.KindAttack}
}

// TestBuildKinds checks that every build of the current league has a metadata file and that its
// kind follows from its recording.
func TestBuildKinds(t *testing.T) {
	t.Parallel()
	dirs, err := pob.BuildDirs("..", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no 3.29 builds")
	}
	for _, dir := range dirs {
		m, err := compare.ReadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
		if err != nil {
			t.Fatal(err)
		}
		var v pob.Values
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if want := recordedKinds(&v); !slices.Contains(want, m.Kind) {
			t.Errorf("%s: kind %s, want one of %v", filepath.Base(dir), m.Kind, want)
		}
	}
}
