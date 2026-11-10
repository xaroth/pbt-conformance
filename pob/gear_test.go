package pob_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/pob"
)

func TestGear(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name, full, want string
	}{
		{
			name: "drops skills, config, flasks; resets choices",
			full: `<?xml version="1.0" encoding="UTF-8"?>
<PathOfBuilding>
	<Build level="90" className="Witch" bandit="Alira" pantheonMajorGod="Lunaris" pantheonMinorGod="Abberath" viewMode="ITEMS"/>
	<Tree activeSpec="1">
		<Spec nodes="1,2">
			<Sockets>
				<Socket nodeId="2" itemId="3"/>
			</Sockets>
		</Spec>
	</Tree>
	<Notes>a note</Notes>
	<Items activeItemSet="1">
		<Item id="1">
Rarity: MAGIC
Divine Life Flask
		</Item>
		<Item id="2">
Rarity: RARE
Doom Crown
Iron Hat
+50 to maximum Life
		</Item>
		<Item id="3">
Rarity: RARE
Rift Eye
Crimson Jewel
7% increased maximum Life
		</Item>
		<ItemSet id="1">
			<Slot name="Flask 1" itemId="1" active="true"/>
			<Slot name="Helmet" itemId="2"/>
		</ItemSet>
	</Items>
	<Skills activeSkillSet="1">
		<SkillSet id="1">
			<Skill enabled="true"><Gem nameSpec="Determination"/></Skill>
		</SkillSet>
	</Skills>
	<Config activeConfigSet="1">
		<ConfigSet id="1"><Input name="bandit" string="Alira"/></ConfigSet>
	</Config>
</PathOfBuilding>
`,
			want: `<?xml version="1.0" encoding="UTF-8"?>
<PathOfBuilding>
	<Build level="90" className="Witch" bandit="None" pantheonMajorGod="None" pantheonMinorGod="None" viewMode="ITEMS"/>
	<Tree activeSpec="1">
		<Spec nodes="1,2">
			<Sockets>
				<Socket nodeId="2" itemId="3"/>
			</Sockets>
		</Spec>
	</Tree>
	<Items activeItemSet="1">
		<Item id="2">
Rarity: RARE
Doom Crown
Iron Hat
+50 to maximum Life
		</Item>
		<Item id="3">
Rarity: RARE
Rift Eye
Crimson Jewel
7% increased maximum Life
		</Item>
		<ItemSet id="1">
			<Slot name="Helmet" itemId="2"/>
		</ItemSet>
	</Items>
</PathOfBuilding>
`,
		},
		{
			name: "no items",
			full: "<PathOfBuilding>\n\t<Build level=\"1\"/>\n\t<Tree activeSpec=\"1\"/>\n</PathOfBuilding>\n",
			want: "<PathOfBuilding>\n\t<Build level=\"1\"/>\n\t<Tree activeSpec=\"1\"/>\n</PathOfBuilding>\n",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := pob.Gear([]byte(testCase.full))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(testCase.want, string(got)); diff != "" {
				t.Errorf("(-want +got)\n%s", diff)
			}
		})
	}
}

func TestGearInvalid(t *testing.T) {
	t.Parallel()
	for _, full := range []string{"<PathOfBuilding><Build>", "<Other/>", ""} {
		if _, err := pob.Gear([]byte(full)); err == nil {
			t.Errorf("%q: got no error", full)
		}
	}
}

// TestGearFiles checks that every stored gear XML is what Gear derives from its build, so a
// changed pob.xml cannot go unrecorded without the PoB runtime.
func TestGearFiles(t *testing.T) {
	t.Parallel()
	fulls, err := filepath.Glob(filepath.Join("..", "builds", "*", "*", pob.FullXML))
	if err != nil {
		t.Fatal(err)
	}
	for _, full := range fulls {
		b, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		want, err := pob.Gear(b)
		if err != nil {
			t.Fatal(err)
		}
		gear := filepath.Join(filepath.Dir(full), pob.GearXML)
		got, err := os.ReadFile(gear)
		if err != nil || string(got) != string(want) {
			t.Errorf("%s is missing or stale, run pbtc pob: %v", gear, err)
		}
	}
}
