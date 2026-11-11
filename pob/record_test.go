package pob_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	for _, name := range []string{pob.ValuesFile, pob.GearXML} {
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

// checkLayers checks that skill outputs are recorded for the full build only, which has skills,
// and delivery ones for its totem.
func checkLayers(t *testing.T, v *pob.Values) {
	t.Helper()
	if v.Full.MainSkill == nil || v.Full.MainSkill.ID != "Fireball" || v.Gear.MainSkill != nil || v.TreeOnly.MainSkill != nil {
		t.Errorf("main skills: got %+v, %+v, %+v", v.Full.MainSkill, v.Gear.MainSkill, v.TreeOnly.MainSkill)
	}
	for _, k := range []string{"TotalDPS", "TotemLife"} {
		_, full := v.Full.Outputs[k]
		_, gear := v.Gear.Outputs[k]
		_, tree := v.TreeOnly.Outputs[k]
		if !full || gear || tree {
			t.Errorf("%s recorded in full, gear, tree-only: %v, %v, %v; want only full", k, full, gear, tree)
		}
	}
	if _, ok := v.TreeOnly.Outputs["BlockChance"]; !ok {
		t.Errorf("tree-only: no defence output BlockChance")
	}
}
