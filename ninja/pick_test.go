package ninja_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/ninja"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"
)

const buildXML = `<PathOfBuilding><Build className="Witch" ascendClassName="Necromancer" level="98"/>` +
	`<Tree activeSpec="1"><Spec treeVersion="3_29"/></Tree></PathOfBuilding>`

func picked(skill string) ninja.Candidate {
	c := cand(compare.KindMinion, skill, "Necromancer", compare.Mercenary)
	c.Char = ninja.Character{Account: "acc-1", Name: "Char", Level: 98, BaseClass: "Witch"}
	c.Snapshot = ninja.Snapshot{Overview: "allflame", Name: "Allflame", Version: "0208"}
	c.XML = []byte(buildXML)
	return c
}

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, "builds", "3.29", n), 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkdirs(t, root, "01-witch-L1-start", "35-scion-L90-ascendant-chaos-inoculation")
	dirs, err := ninja.Write(root, "3.29", []ninja.Candidate{picked("Raise Zombie"), picked("Summon Skeletons")})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(dirs))
	for _, d := range dirs {
		got = append(got, filepath.Base(d))
	}
	want := []string{"36-witch-L98-necromancer-raise-zombie", "37-witch-L98-necromancer-summon-skeletons"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dirs (-want +got):\n%s", diff)
	}
	m, err := compare.ReadMeta(dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	wantMeta := compare.Meta{Kind: compare.KindMinion, Contains: []compare.Content{compare.Mercenary}, Origin: &compare.Origin{
		URL: "https://poe.ninja/poe1/builds/allflame/character/acc-1/Char", League: "Allflame", Snapshot: "0208",
	}}
	if diff := cmp.Diff(wantMeta, m); diff != "" {
		t.Errorf("build.json (-want +got):\n%s", diff)
	}
	if b, err := os.ReadFile(filepath.Join(dirs[0], pob.FullXML)); err != nil || string(b) != buildXML {
		t.Errorf("pob.xml %q, %v", b, err)
	}
	// A second run numbers on past the highest number, so it never writes into an existing
	// build, and leaves the first builds as they are.
	again, err := ninja.Write(root, "3.29", []ninja.Candidate{picked("Raise Zombie")})
	if err != nil || filepath.Base(again[0]) != "38-witch-L98-necromancer-raise-zombie" {
		t.Errorf("second write %v, %v", again, err)
	}
	if m2, err := compare.ReadMeta(dirs[0]); err != nil || !cmp.Equal(m, m2) {
		t.Errorf("first build changed: %v", err)
	}
}

func TestDirName(t *testing.T) {
	t.Parallel()
	c := picked("Ice Shot of Penetration")
	c.Char.BaseClass, c.Ascendancy = "Scion", "Scion"
	if got := ninja.DirName(7, c); got != "07-scion-L98-ice-shot-of-penetration" {
		t.Errorf("got %s", got)
	}
}

func TestExisting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkdirs(t, root, "01-witch-L1-start", "02-witch-L90-necromancer-zombies")
	files := map[string]string{
		"01-witch-L1-start/build.json":                `{"smoke": true}`,
		"01-witch-L1-start/pob.xml":                   buildXML,
		"02-witch-L90-necromancer-zombies/build.json": `{"kind": "minion", "contains": ["cluster-jewel"]}`,
		"02-witch-L90-necromancer-zombies/pob.xml":    buildXML,
		"02-witch-L90-necromancer-zombies/pob.json":   `{"full": {"mainSkill": {"group": 1, "id": "RaiseZombie", "name": "Raise Zombie"}}}`,
	}
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(root, "builds", "3.29", n), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ninja.Existing(root, "3.29")
	if err != nil {
		t.Fatal(err)
	}
	want := []ninja.Build{{Kind: compare.KindMinion, Skill: "Raise Zombie", Ascendancy: "Necromancer", Contains: []compare.Content{compare.ClusterJewel}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("existing (-want +got):\n%s", diff)
	}
}

// Classify reads PoB's results from its cache and runs no PoB for them: the runtime here does
// not exist. It takes the kind and main skill from PoB, adds a mercenary poe.ninja shows, and
// leaves out a build PoB failed on and one whose main skill PoB gives no damage, the minion's
// for a minion skill.
func TestClassifyCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zombie := &pob.MainSkill{Group: 1, Name: "Raise Zombie", Minion: &pob.Minion{Name: "Zombie"}}
	cached := []map[string]any{
		{"result": pob.Result{MainSkill: zombie, Outputs: map[string]float64{"Minion.CombinedDPS": 100}}},
		{"result": pob.Result{MainSkill: &pob.MainSkill{Group: 1, Name: "Raise Spectre"}}},
		{"result": pob.Result{MainSkill: zombie, Outputs: map[string]float64{"CombinedDPS": 100}}},
		{"error": "boom"},
	}
	cands := make([]ninja.Candidate, 0, len(cached))
	for i, r := range cached {
		x := []byte(buildXML + string(rune('a'+i)))
		sum := sha256.Sum256(x)
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		cands = append(cands, ninja.Candidate{XML: x, Char: ninja.Character{Class: "Necromancer"}})
	}
	cands[0].Char.Mercenary = json.RawMessage(`{"name": "Karielle"}`)
	if err := ninja.Classify(t.Context(), runtime.Paths{LuaJIT: "/nonexistent"}, dir, cands); err != nil {
		t.Fatal(err)
	}
	want := ninja.Build{Kind: compare.KindMinion, Skill: "Raise Zombie", Ascendancy: "Necromancer", Contains: []compare.Content{compare.Mercenary}}
	if diff := cmp.Diff(want, cands[0].Build); diff != "" || cands[0].Err != nil {
		t.Errorf("first (-want +got):\n%s%v", diff, cands[0].Err)
	}
	for i, want := range []error{ninja.ErrNoDamage, ninja.ErrNoDamage, ninja.ErrLoad} {
		if !errors.Is(cands[i+1].Err, want) {
			t.Errorf("candidate %d: got %v, want %v", i+1, cands[i+1].Err, want)
		}
	}
}

// A character poe.ninja no longer has is a candidate with the error, not the end of the run,
// and a rerun asks for it no more.
func TestFetchMissingCharacter(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("name") == "gone" {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", "character.json"))
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	var s sleeps
	a := &ninja.API{Client: &ninja.Client{Base: srv.URL, Dir: t.TempDir(), Sleep: s.sleep}}
	leads := []ninja.Lead{{Row: ninja.Row{Account: "a", Name: "gone", Class: "Witch"}}, {Row: ninja.Row{Account: "a", Name: "here"}}}
	for range 2 {
		cands, err := ninja.Fetch(t.Context(), a, leads)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(cands[0].Err, ninja.ErrStatus) || cands[0].Char.Name != "gone" || cands[1].Err != nil || len(cands[1].XML) == 0 {
			t.Errorf("got %v, %v", cands[0].Err, cands[1].Err)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("%d requests, want 2", hits.Load())
	}
}
