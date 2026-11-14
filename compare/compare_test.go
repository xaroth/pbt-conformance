package compare_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	pobimport "github.com/xaroth/pbt-engine/pob"
)

// env returns the variable or skips the test when it is not set.
func env(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skip(name + " not set")
	}
	return v
}

func repoPins(t *testing.T) *pins.File {
	t.Helper()
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", "marauder-life-node.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func store329(t *testing.T) *dataset.Store {
	t.Helper()
	v := repoPins(t).Games["poe1"].Leagues["3.29"].Dataset.Version
	s, err := compare.OpenVersion(env(t, "PBT_DATASETS"), "poe1", v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// writeBuild writes a build directory under root with the given tree-only and full XML and
// empty PoB values, and returns root.
func writeBuild(t *testing.T, name, treeOnly, full string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "builds", "3.29", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	values, err := json.Marshal(pob.Values{})
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string][]byte{pob.TreeOnlyXML: []byte(treeOnly), pob.FullXML: []byte(full), pob.ValuesFile: values} {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func outputs(ms []compare.Mismatch, prefix string) []string {
	var out []string
	for _, m := range ms {
		if strings.HasPrefix(m.Output, prefix) {
			out = append(out, m.Output)
		}
	}
	slices.Sort(out)
	return out
}

// Import mismatches: tree-only warnings, full-file warnings for more than it holds beside the
// tree, and a full file whose tree differs.
func TestRunImports(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	items := `<Items><Item id="1">Rarity: RARE
Doom Crown
Royal Burgonet</Item></Items></PathOfBuilding>`
	withItems := strings.Replace(string(fixture(t)), "</PathOfBuilding>", items, 1)
	testCases := []struct {
		name, treeOnly, full string
		want                 []string
	}{
		{"items only", string(fixture(t)), withItems, nil},
		{"bandit differs", string(fixture(t)), strings.Replace(withItems, `bandit="None"`, `bandit="Alira"`, 1), nil},
		{"cluster node in full only", string(fixture(t)), strings.Replace(withItems, `nodes="47175,31628"`, `nodes="47175,31628,65601"`, 1),
			[]string{"import:full-tree"}},
		{"other tree in full", string(fixture(t)), strings.Replace(withItems, `nodes="47175,31628"`, `nodes="47175"`, 1),
			[]string{"import:full-tree"}},
		{"config in tree-only", strings.Replace(string(fixture(t)), "<Tree", `<Config><Input name="usePowerCharges" boolean="true"/></Config><Tree`, 1),
			string(fixture(t)), []string{"import:pob-config-ignored"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := writeBuild(t, "01-marauder", testCase.treeOnly, testCase.full)
			bs, err := compare.Run(root, datasets, repoPins(t), "poe1", "3.29")
			if err != nil {
				t.Fatal(err)
			}
			if got := outputs(compare.Mismatches(bs), "import:"); !slices.Equal(got, testCase.want) {
				t.Errorf("import mismatches %v, want %v", got, testCase.want)
			}
		})
	}
	other := strings.Replace(string(fixture(t)), "3_29", "3_28", 1)
	if _, err := compare.Run(writeBuild(t, "01-marauder", other, other), datasets, repoPins(t), "poe1", "3.29"); !errors.Is(err, compare.ErrLeague) {
		t.Errorf("3.28 tree under 3.29: got %v, want %v", err, compare.ErrLeague)
	}
}

// TestRun records PoB's values for the fixture and compares: a Marauder with one life node
// has no mismatches, and a Spell Totem Fireball in its full file shows as not compared.
func TestRun(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	cache := env(t, "PBTC_CACHE")
	f := repoPins(t)
	root := t.TempDir()
	dir := filepath.Join(root, "builds", "3.29", "01-marauder-life-node")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	totem, err := os.ReadFile(filepath.Join("..", "testdata", "marauder-totem.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, xml := range map[string][]byte{pob.FullXML: totem, pob.TreeOnlyXML: fixture(t)} {
		if err := os.WriteFile(filepath.Join(dir, name), xml, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pob.Record(context.Background(), root, cache, f, "poe1", "3.29", false); err != nil {
		t.Fatal(err)
	}
	bs, err := compare.Run(root, datasets, f, "poe1", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 {
		t.Fatalf("got %d builds, want 1", len(bs))
	}
	// The engine does not apply Spell Totem's 50% less damage yet, so the totem's Fireball hit is
	// twice PoB's.
	ms := compare.Mismatches(bs)
	outputs := make([]string, 0, len(ms))
	for _, m := range ms {
		outputs = append(outputs, m.Output)
	}
	if diff := cmp.Diff([]string{"skill.average_hit", "skill.fire_minimum", "skill.fire_maximum"}, outputs); diff != "" {
		t.Errorf("mismatches (-want +got):\n%s", diff)
	}
	// The engine gives the sheet of the tree and gear layers and, of their defences, spell block,
	// reservation, a skill's costs and a spell's hit; nothing else.
	skillGroups := []compare.GroupResult{
		{Group: pob.GroupSheet, Status: compare.StatusNotCompared}, {Group: pob.GroupReservation, Status: compare.StatusPass},
		{Group: pob.GroupSkill, Status: compare.StatusMismatch, Missing: []string{"skill.hits_per_second", "skill.hit_dps", "skill.dot_dps", "skill.total_dps"}},
		{Group: pob.GroupCost, Status: compare.StatusPass},
		{Group: pob.GroupHitTypes, Status: compare.StatusMismatch}, {Group: pob.GroupDefence, Status: compare.StatusNotCompared},
		{Group: pob.GroupDelivery, Status: compare.StatusNotCompared},
	}
	sheetGroups := []compare.GroupResult{
		{Group: pob.GroupSheet, Status: compare.StatusPass},
		{Group: pob.GroupReservation, Status: compare.StatusPass},
		{Group: pob.GroupDefence, Status: compare.StatusIncomplete, Missing: []string{
			"player.block_chance", "player.spell_suppression_chance",
			"player.life_regeneration_recovery", "player.energy_shield_regeneration_recovery",
			"player.life_leech_rate", "player.energy_shield_leech_rate", "player.energy_shield_recharge",
		}},
	}
	want := []compare.Layer{
		{Name: "tree", Groups: sheetGroups},
		{Name: "gear", Groups: sheetGroups},
		{Name: "skill", Groups: skillGroups},
		{Name: "full", Groups: skillGroups},
	}
	if diff := cmp.Diff(want, bs[0].Layers, cmpopts.IgnoreFields(compare.Layer{}, "Compared")); diff != "" {
		t.Errorf("layers (-want +got):\n%s", diff)
	}
	// no party file, no party layer
	if n := len(bs[0].Layers); n != 4 {
		t.Errorf("got %d layers, want 4", n)
	}
	if bs[0].Meta.Kind != compare.Untagged {
		t.Errorf("kind: got %q, want %q", bs[0].Meta.Kind, compare.Untagged)
	}
	for _, b := range []compare.Blocker{{Kind: compare.BlockerGem, Name: "Spell Totem"}, {Kind: compare.BlockerMechanic, Name: "skills"}} {
		if !slices.Contains(bs[0].Blockers, b) {
			t.Errorf("no blocker %+v in %+v", b, bs[0].Blockers)
		}
	}
	if _, err := compare.Run(root, t.TempDir(), f, "poe1", "3.29"); !errors.Is(err, compare.ErrDataset) {
		t.Errorf("missing dataset: got %v, want %v", err, compare.ErrDataset)
	}
}

func TestAllocatedCountsStarts(t *testing.T) {
	t.Parallel()
	s := store329(t)
	reg, err := dataset.OpenRoot(os.DirFS(env(t, "PBT_DATASETS")))
	if reg == nil {
		t.Fatal(err)
	}
	b, _, err := pobimport.FromXML(fixture(t), reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ascendancy string
		nodes            []uint32
		want             int
	}{
		{"class start only", "", nil, 1},
		{"with ascendancy", "Juggernaut", nil, 2},
		{"start listed, not counted twice", "", b.Nodes, len(b.Nodes)},
	} {
		nb := *b
		nb.Ascendancy, nb.Nodes = tc.ascendancy, tc.nodes
		c, _, err := build.Check(&nb, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := compare.Allocated(c); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

// A build the check fixes shows its warning as a mismatch to triage, so a dropped node or
// mastery choice cannot pass as another entry's cause.
func TestRunReportsWarnings(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	root := t.TempDir()
	dir := filepath.Join(root, "builds", "3.29", "01-marauder-unknown-node")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	xml := strings.Replace(string(fixture(t)), `nodes="47175,31628"`, `nodes="47175,31628,2"`, 1)
	for _, name := range []string{pob.FullXML, pob.TreeOnlyXML} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(xml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	values, err := json.Marshal(pob.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pob.ValuesFile), values, 0o600); err != nil {
		t.Fatal(err)
	}
	bs, err := compare.Run(root, datasets, repoPins(t), "poe1", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	ms := compare.Mismatches(bs)
	if !slices.ContainsFunc(ms, func(m compare.Mismatch) bool {
		return m.Output == "warning:node-unknown" && strings.HasPrefix(m.Detail, "node 2: ")
	}) {
		t.Errorf("no node-unknown warning in %+v", ms)
	}
}

// An override on a node the build has not allocated is dropped, as PoB does; it is no mismatch.
func TestRunSkipsOverrideNotAllocated(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	override := `<Overrides><Override dn="Tattoo of the Hinekora Shaman" nodeId="18182"/></Overrides></Spec>`
	xml := strings.Replace(string(fixture(t)), `nodes="47175,31628"/>`, `nodes="47175,31628">`+override, 1)
	root := writeBuild(t, "01-marauder-override", xml, xml)
	bs, err := compare.Run(root, datasets, repoPins(t), "poe1", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	ms := compare.Mismatches(bs)
	if got := append(outputs(ms, "warning:"), outputs(ms, "import:")...); len(got) != 0 {
		t.Errorf("got %v, want no warning or import mismatches", got)
	}
}

// A build recorded with an ally has a party layer, not compared: the engine does not simulate
// party buffs yet. A build without one has no party layer.
func TestRunPartyLayer(t *testing.T) {
	t.Parallel()
	datasets := env(t, "PBT_DATASETS")
	root := writeBuild(t, "01-marauder", string(fixture(t)), string(fixture(t)))
	party := &pob.Result{Outputs: map[string]float64{"Life": 1150, "BlockChance": 0}, MainSkill: &pob.MainSkill{ID: "Vitality", Name: "Vitality"}}
	values, err := json.Marshal(pob.Values{Party: party})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "builds", "3.29", "01-marauder", pob.ValuesFile), values, 0o600); err != nil {
		t.Fatal(err)
	}
	bs, err := compare.Run(root, datasets, repoPins(t), "poe1", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	ls := bs[0].Layers
	want := compare.Layer{Name: "party", Groups: []compare.GroupResult{
		{Group: pob.GroupSheet, Status: compare.StatusNotCompared}, {Group: pob.GroupDefence, Status: compare.StatusNotCompared},
	}}
	if len(ls) != 5 {
		t.Fatalf("got %d layers, want 5: %+v", len(ls), ls)
	}
	if diff := cmp.Diff(want, ls[4]); diff != "" {
		t.Errorf("party layer (-want +got):\n%s", diff)
	}
}
