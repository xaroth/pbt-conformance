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
			name: "keeps granted groups disabled; drops other skills, config, flasks; resets choices",
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
	<Skills activeSkillSet="2">
		<SkillSet id="1">
			<Skill enabled="true" source="Item:4:Ring"><Gem skillId="Anger" enabled="true"/></Skill>
		</SkillSet>
		<SkillSet id="2">
			<Skill enabled="true"><Gem nameSpec="Determination"/></Skill>
			<Skill enabled="true" source="Item:2:Doom Crown">
				<Gem skillId="Anger" enabled="true"/>
				<Gem skillId="SupportGenerosity"/>
			</Skill>
			<Skill enabled="true" source="Tree:123"><Gem skillId="Herald"/></Skill>
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
	<Skills activeSkillSet="2">
		<SkillSet id="2">
			<Skill enabled="false" source="Item:2:Doom Crown">
				<Gem skillId="Anger" enabled="false"/>
				<Gem enabled="false" skillId="SupportGenerosity"/>
			</Skill>
			<Skill enabled="false" source="Tree:123"><Gem enabled="false" skillId="Herald"/></Skill>
		</SkillSet>
	</Skills>
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

func TestSkill(t *testing.T) {
	t.Parallel()
	full := `<PathOfBuilding>
	<Build level="90" bandit="Alira" mainSocketGroup="3"/>
	<Tree activeSpec="1"/>
	<Items activeItemSet="1">
		<Item id="1">
Divine Life Flask
		</Item>
		<ItemSet id="1">
			<Slot name="Flask 1" itemId="1"/>
		</ItemSet>
	</Items>
	<Skills activeSkillSet="2">
		<SkillSet id="1">
			<Skill enabled="true"><Gem nameSpec="Fireball"/></Skill>
		</SkillSet>
		<SkillSet id="2">
			<Skill enabled="true"><Gem nameSpec="Determination"/></Skill>
			<Skill enabled="true" source="Item:2:Doom Crown" slot="Helmet"><Gem skillId="HeraldOfBreach"/></Skill>
			<Skill enabled="true" slot="Body Armour"><Gem nameSpec="Arc" skillPart="2"/><Gem nameSpec="Spell Echo Support"/></Skill>
			<Skill enabled="false"><Gem skillId="FlameDash"/></Skill>
			<Skill enabled="true" slot="Gloves">
				<Gem skillId="Clarity" enabled="true"/>
				<Gem skillId="SupportElementalPenetration" enabled="true"/>
			</Skill>
		</SkillSet>
	</Skills>
	<Config activeConfigSet="1">
		<ConfigSet id="1" title="Default">
			<Input name="enemyIsBoss" string="Uber"/>
			<Input name="conditionStationary" boolean="true"/>
			<Placeholder name="enemyLevel" number="84"/>
			<CustomModifierBlock>+1 to Level of all Spell Skill Gems</CustomModifierBlock>
		</ConfigSet>
		<ConfigSet id="2" title="Other">
			<Input name="enemyLevel" number="70"/>
		</ConfigSet>
	</Config>
	<Party/>
</PathOfBuilding>
`
	want := `<PathOfBuilding>
	<Build level="90" bandit="None" mainSocketGroup="3"/>
	<Tree activeSpec="1"/>
	<Items activeItemSet="1">
		<ItemSet id="1">
		</ItemSet>
	</Items>
	<Skills activeSkillSet="2">
		<SkillSet id="2">
			<Skill enabled="true"><Gem enabled="false" nameSpec="Determination"/></Skill>
			<Skill enabled="false" source="Item:2:Doom Crown" slot="Helmet"><Gem skillId="HeraldOfBreach"/></Skill>
			<Skill enabled="true" slot="Body Armour"><Gem nameSpec="Arc" skillPart="2"/><Gem nameSpec="Spell Echo Support"/></Skill>
			<Skill enabled="true" slot="Gloves">
				<Gem skillId="Clarity" enabled="false"/>
				<Gem skillId="SupportElementalPenetration" enabled="true"/>
			</Skill>
		</SkillSet>
	</Skills>
	<Config activeConfigSet="1">
		<ConfigSet id="1" title="Default">
			<Input name="enemyIsBoss" string="Uber"/>
		</ConfigSet>
	</Config>
</PathOfBuilding>
`
	got, err := pob.Skill([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("(-want +got)\n%s", diff)
	}
}

func TestDeriveInvalid(t *testing.T) {
	t.Parallel()
	for _, full := range []string{"<PathOfBuilding><Build>", "<Other/>", ""} {
		for name, derive := range pob.Derived() {
			if _, err := derive([]byte(full)); err == nil {
				t.Errorf("%s of %q: got no error", name, full)
			}
		}
	}
}

// TestDerivedFiles checks that every stored derived XML is what pob derives from its
// build, so a changed pob.xml cannot go unrecorded without the PoB runtime.
func TestDerivedFiles(t *testing.T) {
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
		for name, derive := range pob.Derived() {
			want, err := derive(b)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(full), name)
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(want) {
				t.Errorf("%s is missing or stale, run pbtc pob: %v", path, err)
			}
		}
	}
}

func TestTreeOnly(t *testing.T) {
	t.Parallel()
	full := `<PathOfBuilding>
	<Build level="90" bandit="Alira" pantheonMajorGod="Lunaris" pantheonMinorGod="Abberath"/>
	<Tree activeSpec="1">
		<Spec nodes="1,2">
			<Sockets>
				<Socket nodeId="2" itemId="1"/>
			</Sockets>
		</Spec>
	</Tree>
	<Items activeItemSet="1">
		<Item id="1">
Rarity: RARE
Rift Eye
Crimson Jewel
		</Item>
	</Items>
	<Skills/>
	<Config/>
</PathOfBuilding>
`
	want := `<PathOfBuilding>
	<Build level="90" bandit="None" pantheonMajorGod="None" pantheonMinorGod="None"/>
	<Tree activeSpec="1">
		<Spec nodes="1,2">
			<Sockets>
			</Sockets>
		</Spec>
	</Tree>
</PathOfBuilding>
`
	got, err := pob.TreeOnly([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("(-want +got)\n%s", diff)
	}
}
