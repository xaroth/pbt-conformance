package pob_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
)

func TestRecord(t *testing.T) {
	t.Parallel()
	cache := os.Getenv("PBTC_CACHE")
	if cache == "" {
		t.Skip("PBTC_CACHE not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "builds", "3.29", "01-marauder-life-node")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, fixture := range map[string]string{pob.FullXML: "marauder-totem.xml", pob.TreeOnlyXML: "marauder-life-node.xml"} {
		xml, err := os.ReadFile(filepath.Join("..", "testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), xml, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", true); !errors.Is(err, pob.ErrStale) {
		t.Fatalf("check before recording: got %v, want %v", err, pob.ErrStale)
	}
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", false); err != nil {
		t.Fatal(err)
	}
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", true); err != nil {
		t.Fatalf("check after recording: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
	if err != nil {
		t.Fatal(err)
	}
	var v pob.Values
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.PoB != f.Games["poe1"].Leagues["3.29"].PoB || v.Full.Outputs["Life"] != 1150 || v.Gear.Outputs["Life"] != 1150 ||
		v.TreeOnly.Outputs["Life"] != 1150 {
		t.Errorf("values: got %+v", v)
	}
	checkLayers(t, &v)
	for _, name := range []string{pob.ValuesFile, pob.GearXML, pob.SkillXML} {
		path := filepath.Join(dir, name)
		stored, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", true); !errors.Is(err, pob.ErrStale) {
			t.Errorf("check after tampering with %s: got %v, want %v", name, err, pob.ErrStale)
		}
		if err := os.WriteFile(path, stored, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// checkLayers checks that skill outputs are recorded for the full and skill builds only, which
// have skills, and delivery ones for their totem.
func checkLayers(t *testing.T, v *pob.Values) {
	t.Helper()
	checkMainSkills(t, v)
	for _, k := range []string{"TotalDPS", "TotemLife", "Calcs.FireMax"} {
		_, full := v.Full.Outputs[k]
		_, skill := v.Skill.Outputs[k]
		_, gear := v.Gear.Outputs[k]
		_, tree := v.TreeOnly.Outputs[k]
		if !full || !skill || gear || tree {
			t.Errorf("%s recorded in full, skill, gear, tree-only: %v, %v, %v, %v; want full and skill", k, full, skill, gear, tree)
		}
	}
	if _, ok := v.TreeOnly.Outputs["BlockChance"]; !ok {
		t.Errorf("tree-only: no defence output BlockChance")
	}
}

// checkMainSkills checks that the full and skill layers record the totem's main skill with its
// support, and the other layers none.
func checkMainSkills(t *testing.T, v *pob.Values) {
	t.Helper()
	for _, ms := range []*pob.MainSkill{v.Full.MainSkill, v.Skill.MainSkill} {
		if ms == nil || ms.ID != "Fireball" || !slices.ContainsFunc(ms.Supports, func(s pob.Support) bool { return s.ID == "SupportSpellTotem" }) {
			t.Errorf("main skill: got %+v, want Fireball supported by Spell Totem", ms)
		}
	}
	if v.Gear.MainSkill != nil || v.TreeOnly.MainSkill != nil {
		t.Errorf("main skills: got %+v, %+v in gear, tree-only", v.Gear.MainSkill, v.TreeOnly.MainSkill)
	}
}

// A build with a party file is recorded with its ally's buffs too; a party file naming no build
// of the league is an error.
func TestRecordParty(t *testing.T) {
	t.Parallel()
	cache := os.Getenv("PBTC_CACHE")
	if cache == "" {
		t.Skip("PBTC_CACHE not set")
	}
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	league := filepath.Join(root, "builds", "3.29")
	builds := map[string]string{"01-marauder-life-node": "marauder-life-node.xml", "02-marauder-vitality": "marauder-vitality.xml"}
	for name, fixture := range builds {
		xml, err := os.ReadFile(filepath.Join("..", "testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(league, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{pob.FullXML, pob.TreeOnlyXML} {
			if err := os.WriteFile(filepath.Join(dir, file), xml, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	party := filepath.Join(league, "01-marauder-life-node", pob.PartyFile)
	if err := os.WriteFile(party, []byte(`{"ally": "02-marauder-vitality"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", false); err != nil {
		t.Fatal(err)
	}
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", true); err != nil {
		t.Fatalf("check after recording: %v", err)
	}
	for name, wantParty := range map[string]bool{"01-marauder-life-node": true, "02-marauder-vitality": false} {
		b, err := os.ReadFile(filepath.Join(league, name, pob.ValuesFile))
		if err != nil {
			t.Fatal(err)
		}
		var v pob.Values
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		if (v.Party != nil) != wantParty {
			t.Fatalf("%s: party recorded %v, want %v", name, v.Party != nil, wantParty)
		}
		// recorded as the full layer: with its main skill
		if wantParty && v.Party.MainSkill == nil {
			t.Errorf("%s: party layer has no main skill", name)
		}
		if wantParty && v.Party.Outputs["LifeRegen"] <= v.Full.Outputs["LifeRegen"] {
			t.Errorf("%s: life regeneration with ally %v, alone %v", name, v.Party.Outputs["LifeRegen"], v.Full.Outputs["LifeRegen"])
		}
	}
	if err := os.WriteFile(party, []byte(`{"ally": "03-missing"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pob.Record(ctx, root, cache, f, "poe1", "3.29", true); !errors.Is(err, pob.ErrParty) {
		t.Errorf("missing ally: got %v, want %v", err, pob.ErrParty)
	}
}

// TestSkillLayerMainSkill checks that every build's skill layer calculates the full layer's main
// skill, with the same level, quality and supports; only its group number may differ.
func TestSkillLayerMainSkill(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("..", "builds", "*", "*", pob.ValuesFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var v pob.Values
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		full, skill := v.Full.MainSkill, v.Skill.MainSkill
		if full == nil || skill == nil {
			t.Errorf("%s: main skill full %+v, skill %+v", f, full, skill)
			continue
		}
		if diff := cmp.Diff(*full, *skill, cmpopts.IgnoreFields(pob.MainSkill{}, "Group")); diff != "" {
			t.Errorf("%s: main skill (-full +skill):\n%s", f, diff)
		}
	}
}
