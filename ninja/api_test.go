package ninja_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xaroth/pbt-conformance/ninja"
)

// recorded serves responses recorded from poe.ninja: the index state, one search (Winter Orb in
// Allflame, for every search), the class, gem and mercenary class dictionaries and one
// character.
func recorded(t *testing.T) *ninja.API {
	t.Helper()
	files := map[string]string{
		"/poe1/api/data/index-state": "index-state.json",
		"/poe1/api/builds/dictionary/1f8a7450649b94a3e6895acbd056902cad1bd353": "dictionary-class.bin",
		"/poe1/api/builds/dictionary/ae6775593403fb748fd5bf4b47663cef28435d99": "dictionary-gem.bin",
		"/poe1/api/builds/dictionary/289103373474aa523e7f0738c5014f40c8905766": "dictionary-mercenaryclass.bin",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := files[r.URL.Path]
		switch {
		case strings.HasSuffix(r.URL.Path, "/search"):
			name, ok = "search-winter-orb.bin", true
		case strings.HasSuffix(r.URL.Path, "/character"):
			name, ok = "character.json", true
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	var s sleeps
	return &ninja.API{Client: &ninja.Client{Base: srv.URL, Dir: t.TempDir(), Sleep: s.sleep}}
}

func TestSnapshot(t *testing.T) {
	t.Parallel()
	a := recorded(t)
	s, err := a.Snapshot(t.Context(), "allflame")
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "0208-20261111-47167" || s.PassiveTree != "PassiveTree-3.29" || s.Name != "Allflame" {
		t.Errorf("got %+v", s)
	}
	if _, err := a.Snapshot(t.Context(), "nowhere"); err == nil {
		t.Error("unknown league: no error")
	}
}

func TestSearch(t *testing.T) {
	t.Parallel()
	a := recorded(t)
	s, err := a.Snapshot(t.Context(), "allflame")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Search(t.Context(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 9580 || len(r.Rows) != 100 {
		t.Errorf("total %d, %d rows; want 9580, 100", r.Total, len(r.Rows))
	}
	if r.Skills[0] != (ninja.Count{Name: "Winter Orb", Count: 9580}) {
		t.Errorf("most popular skill %+v", r.Skills[0])
	}
	if len(r.Mercenaries) == 0 || r.Mercenaries[0].Count == 0 || r.Mercenaries[0].Name == "" {
		t.Errorf("mercenaries %+v", r.Mercenaries)
	}
	if got := r.Rows[1]; got.Name != "SunfishOrbing" || got.Class != "Elementalist" || got.Account == "" {
		t.Errorf("second row %+v", got)
	}
}

func TestCharacter(t *testing.T) {
	t.Parallel()
	a := recorded(t)
	s, err := a.Snapshot(t.Context(), "allflame")
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Character(t.Context(), s, "any", "any")
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasMercenary() || c.Level == 0 || c.BaseClass == "" {
		t.Errorf("got %s level %d %s, mercenary %v", c.Name, c.Level, c.BaseClass, c.HasMercenary())
	}
	x, err := c.XML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(x), "<?xml") || !strings.Contains(string(x), "<PathOfBuilding>") {
		t.Errorf("export is not a PoB build: %.80s", x)
	}
	if u := c.URL(s); !strings.HasPrefix(u, "https://poe.ninja/poe1/builds/allflame/character/") {
		t.Errorf("url %s", u)
	}
}

func TestLeads(t *testing.T) {
	t.Parallel()
	a := recorded(t)
	src := ninja.Source{Overview: "allflame", Variant: "allflamer", Skills: 1, Mercenaries: 1, PerSkill: 2, VariantRows: 2}
	leads, err := ninja.Leads(t.Context(), a, src, "3.29")
	if err != nil {
		t.Fatal(err)
	}
	// Every search answers with the same recorded rows, and each skips the characters taken
	// already: Winter Orb gives the first two, the mercenary class the next two, the variant
	// league the two after.
	want := []string{"allflame", "allflame", "allflame BrittlePaperBear", "allflame Jokers", "allflamer geoffrey_floydstein", "allflamer Nicos_mor"}
	got := make([]string, 0, len(leads))
	for _, l := range leads {
		// The first name is in Han script; the first two are checked by league only.
		if len(got) < 2 {
			got = append(got, l.Snapshot.Overview)
			continue
		}
		got = append(got, l.Snapshot.Overview+" "+l.Row.Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("leads %+v", leads)
	}
	if _, err := ninja.Leads(t.Context(), a, ninja.Source{Overview: "mirage", Skills: 1, PerSkill: 1}, "3.29"); err == nil {
		t.Error("league on another tree: no error")
	}
}
