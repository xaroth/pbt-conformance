package compare_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/xaroth/pbt-conformance/compare"
)

// pobWith returns a full PoB file with the active spec's tree version and sockets, and the items
// and slots given as their XML.
func pobWith(treeVersion, sockets, items string) string {
	return `<PathOfBuilding><Build level="90"/><Tree activeSpec="2"><Spec treeVersion="3_28"/>` +
		`<Spec treeVersion="` + treeVersion + `"><Sockets>` + sockets + `</Sockets></Spec></Tree>` +
		`<Items activeItemSet="1">` + items + `</Items></PathOfBuilding>`
}

const (
	clusterJewel = `<Item id="1">
Rarity: MAGIC
Fevered Large Cluster Jewel of Potency
Item Level: 84
		</Item>`
	timelessJewel = `<Item id="2">
Rarity: UNIQUE
Glorious Vanity
Timeless Jewel
		</Item>`
	skillHelmet = `<Item id="3">
Rarity: UNIQUE
Black Sun Crest
Lacquered Helmet
{crafted}Grants Level 20 Summon Stone Golem
		</Item>`
	plainHelmet = `<Item id="3">
Rarity: RARE
Doom Crown
Iron Hat
+50 to maximum Life
		</Item>`
)

func TestDetectContents(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, file string
		want       []compare.Content
	}{
		{"nothing", pobWith("3_29", "", plainHelmet+`<ItemSet id="1"><Slot name="Helmet" itemId="3"/></ItemSet>`), nil},
		{"socketed jewels", pobWith("3_29", `<Socket nodeId="1" itemId="1"/><Socket nodeId="2" itemId="2"/>`, clusterJewel+timelessJewel),
			[]compare.Content{compare.ClusterJewel, compare.TimelessJewel}},
		{"jewels not socketed in the active tree", pobWith("3_29", "", clusterJewel+timelessJewel), nil},
		{"ruthless tree", pobWith("3_29_ruthless", "", ""), []compare.Content{compare.TreeVariant}},
		{"alternate tree", pobWith("3_28_alternate", "", ""), []compare.Content{compare.TreeVariant}},
		{"item grants a skill", pobWith("3_29", "", skillHelmet+`<ItemSet id="1"><Slot name="Helmet" itemId="3"/></ItemSet>`),
			[]compare.Content{compare.ItemSkill}},
		{"item with a skill in another set", pobWith("3_29", "", skillHelmet+`<ItemSet id="2"><Slot name="Helmet" itemId="3"/></ItemSet>`), nil},
		{"item with a skill in a flask slot", pobWith("3_29", "", skillHelmet+`<ItemSet id="1"><Slot name="Flask 1" itemId="3"/></ItemSet>`), nil},
		{"graft", pobWith("3_29", "", plainHelmet+`<ItemSet id="1"><Slot name="Graft 1" itemId="3"/></ItemSet>`),
			[]compare.Content{compare.ItemSkill}},
		{"older file without item sets", pobWith("3_29", "", skillHelmet+`<Slot name="Helmet" itemId="3"/>`),
			[]compare.Content{compare.ItemSkill}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := compare.DetectContents([]byte(testCase.file))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(testCase.want, got); diff != "" {
				t.Errorf("contents (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDetectContentsInvalid(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"<PathOfBuilding>", `<PathOfBuilding><Tree activeSpec="2"><Spec/></Tree></PathOfBuilding>`} {
		if _, err := compare.DetectContents([]byte(file)); err == nil {
			t.Errorf("%q: got no error", file)
		}
	}
}

// TestBuildContents checks that each tagged build lists exactly the contents its pob.xml holds,
// a mercenary apart, which PoB's file does not show.
func TestBuildContents(t *testing.T) {
	t.Parallel()
	metas, err := filepath.Glob(filepath.Join("..", "builds", "*", "*", compare.MetaFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range metas {
		dir := filepath.Dir(path)
		m, err := compare.ReadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		full, err := os.ReadFile(filepath.Join(dir, "pob.xml"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := compare.DetectContents(full)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		tagged := slices.DeleteFunc(slices.Clone(m.Contains), func(c compare.Content) bool { return c == compare.Mercenary })
		slices.SortFunc(tagged, func(a, b compare.Content) int {
			return slices.Index(compare.Contents(), a) - slices.Index(compare.Contents(), b)
		})
		if diff := cmp.Diff(got, tagged, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("%s: contains (-pob.xml +build.json):\n%s", dir, diff)
		}
	}
}

func TestDetectContentsError(t *testing.T) {
	t.Parallel()
	_, err := compare.DetectContents([]byte(`<PathOfBuilding><Tree activeSpec="1"></Tree></PathOfBuilding>`))
	if !errors.Is(err, compare.ErrMeta) {
		t.Errorf("got %v, want %v", err, compare.ErrMeta)
	}
}
