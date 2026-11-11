package compare_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pob"
)

// layers are a tree layer whose sheet passes and a full layer whose skill group has status full.
func layers(full compare.Status) []compare.Layer {
	return []compare.Layer{
		{Name: "tree", Groups: []compare.GroupResult{{Group: pob.GroupSheet, Status: compare.StatusPass}}},
		{Name: "full", Groups: []compare.GroupResult{{Group: pob.GroupSkill, Status: full}}},
	}
}

func scoreBuilds() []compare.Build {
	stat := compare.Blocker{Kind: compare.BlockerStat, Name: "spell_damage_+%"}
	gem := compare.Blocker{Kind: compare.BlockerGem, Name: "Cruelty"}
	return []compare.Build{
		{League: "3.29", Name: "01-a", Meta: compare.Meta{Kind: compare.Untagged}, Layers: layers(compare.StatusPass),
			Blockers: []compare.Blocker{stat}},
		{League: "3.29", Name: "02-b", Meta: compare.Meta{Kind: compare.KindAttack, Contains: []compare.Content{compare.ClusterJewel}},
			Layers: layers(compare.StatusNotCompared), Blockers: []compare.Blocker{gem, stat}},
		{League: "3.29", Name: "03-c", Meta: compare.Meta{Kind: compare.KindAttack},
			Mismatches: []compare.Mismatch{{League: "3.29", Build: "03-c", Output: "warning:node-unknown"}},
			Layers:     layers(compare.StatusPass)},
		{League: "3.28", Name: "02-s", Meta: compare.Meta{Kind: compare.Smoke}},
		{League: "3.28", Name: "01-a", Meta: compare.Meta{Kind: compare.Untagged}, Blockers: []compare.Blocker{stat},
			Mismatches: []compare.Mismatch{{League: "3.28", Build: "01-a", Output: "player.life", Triage: "life"}},
			Layers: []compare.Layer{{Name: "tree", Groups: []compare.GroupResult{
				{Group: pob.GroupSheet, Status: compare.StatusIncomplete, Missing: []string{"player.unreserved_life"}},
			}}}},
	}
}

func TestScore(t *testing.T) {
	t.Parallel()
	builds := scoreBuilds()
	s := compare.Score(builds, compare.Mismatches(builds), nil, map[string]string{"spell_damage_+%": "Damage, spell"})
	wantGroups := []compare.GroupRow{
		{League: "3.28", Layer: "tree", Group: pob.GroupSheet, Builds: 1, Fail: 1, Incomplete: 1},
		{League: "3.28", Layer: "tree", Group: compare.GroupChecks, Builds: 2, Pass: 2},
		{League: "3.29", Layer: "tree", Group: pob.GroupSheet, Builds: 3, Pass: 3},
		{League: "3.29", Layer: "tree", Group: compare.GroupChecks, Builds: 3, Pass: 2, Fail: 1, Mismatch: 1},
		{League: "3.29", Layer: "full", Group: pob.GroupSkill, Builds: 3, Pass: 2, NotCompared: 1},
	}
	if diff := cmp.Diff(wantGroups, s.Groups); diff != "" {
		t.Errorf("groups (-want +got):\n%s", diff)
	}
	wantKinds := []compare.KindRow{
		{League: "3.28", Kind: "untagged", Builds: 1},
		{League: "3.28", Kind: "smoke", Builds: 1, PassAll: 1},
		{League: "3.29", Kind: "untagged", Builds: 1, PassAll: 1},
		{League: "3.29", Kind: "attack", Builds: 2},
		{League: "3.29", Kind: "contains:cluster-jewel", Builds: 1},
	}
	if diff := cmp.Diff(wantKinds, s.Kinds); diff != "" {
		t.Errorf("kinds (-want +got):\n%s", diff)
	}
	wantMissing := []compare.MissingRow{{Group: pob.GroupSheet, Output: "player.unreserved_life", Builds: 1}}
	if diff := cmp.Diff(wantMissing, s.Missing); diff != "" {
		t.Errorf("missing (-want +got):\n%s", diff)
	}
	wantBlockers := []compare.BlockerRow{
		{Kind: compare.BlockerStat, Name: "spell_damage_+%", Builds: 3,
			Leagues: map[string]int{"3.28": 1, "3.29": 2}, Hint: "Damage, spell"},
		{Kind: compare.BlockerGem, Name: "Cruelty", Builds: 1, Leagues: map[string]int{"3.29": 1}},
	}
	if diff := cmp.Diff(wantBlockers, s.Blockers); diff != "" {
		t.Errorf("blockers (-want +got):\n%s", diff)
	}
}

// The text and the JSON hold the same rows, and the text repeats exactly on a second write.
func TestScoreboardWrite(t *testing.T) {
	t.Parallel()
	builds := scoreBuilds()
	s := compare.Score(builds, compare.Mismatches(builds), []string{"stale"}, nil)
	var text, again, js bytes.Buffer
	if err := s.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	if err := compare.Score(builds, compare.Mismatches(builds), []string{"stale"}, nil).WriteText(&again); err != nil {
		t.Fatal(err)
	}
	if text.String() != again.String() {
		t.Errorf("text differs between writes:\n%s\n%s", text.String(), again.String())
	}
	for _, want := range []string{
		"3.29    tree   checks  3       2     1     1         0           0",
		"3.29    contains:cluster-jewel  1       0",
		"player.unreserved_life  sheet  1",
		"3       1     2     stat     spell_damage_+%",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, text.String())
		}
	}
	for line := range strings.Lines(text.String()) {
		if strings.HasSuffix(strings.TrimSuffix(line, "\n"), " ") {
			t.Errorf("trailing space: %q", line)
		}
	}
	if err := s.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	var got compare.Scoreboard
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(s, &got); diff != "" {
		t.Errorf("json round trip (-want +got):\n%s", diff)
	}
}

func TestReadHints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good.json", `{
		"spell_damage_+%": "Damage, spell",
		"damage_+%": {"hint": "Damage INC", "pob": [{"source": "skillStatMap"}]}
	}`)
	h, err := compare.ReadHints(good)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"spell_damage_+%": "Damage, spell", "damage_+%": "Damage INC"}
	if diff := cmp.Diff(want, h); diff != "" {
		t.Errorf("good (-want +got):\n%s", diff)
	}
	for name, body := range map[string]string{
		"number":    `{"spell_damage_+%": 3}`,
		"no hint":   `{"spell_damage_+%": {"pob": []}}`,
		"hint type": `{"spell_damage_+%": {"hint": 3}}`,
		"list":      `["spell_damage_+%"]`,
	} {
		if _, err := compare.ReadHints(write(name+".json", body)); !errors.Is(err, compare.ErrHints) {
			t.Errorf("%s: got %v, want %v", name, err, compare.ErrHints)
		}
	}
}

// A stat's hint shows beside it in the blockers table and changes nothing else; a gem's name
// takes no hint.
func TestScoreboardHintsText(t *testing.T) {
	t.Parallel()
	builds := scoreBuilds()
	text := func(hints map[string]string) string {
		t.Helper()
		var b bytes.Buffer
		if err := compare.Score(builds, compare.Mismatches(builds), nil, hints).WriteText(&b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	with := text(map[string]string{"spell_damage_+%": "Damage INC Spell (skill stat map)", "Cruelty": "not a stat"})
	if !strings.Contains(with, "stat     spell_damage_+%  Damage INC Spell (skill stat map)\n") {
		t.Errorf("text lacks the hint:\n%s", with)
	}
	if strings.Contains(with, "not a stat") {
		t.Errorf("hint shown beside a gem:\n%s", with)
	}
	if without := text(nil); strings.Replace(with, "  Damage INC Spell (skill stat map)", "", 1) != without {
		t.Errorf("hints change more than the hint column:\n%s\n%s", with, without)
	}
}

// The party layer is scored for the builds that have it, after the other layers, and a build
// not compared on it does not pass everything.
func TestScorePartyLayer(t *testing.T) {
	t.Parallel()
	party := compare.Layer{Name: "party", Groups: []compare.GroupResult{{Group: pob.GroupSheet, Status: compare.StatusNotCompared}}}
	builds := []compare.Build{
		{League: "3.29", Name: "01-a", Meta: compare.Meta{Kind: compare.KindParty}, Layers: append(layers(compare.StatusPass), party)},
		{League: "3.29", Name: "02-b", Meta: compare.Meta{Kind: compare.KindParty}, Layers: layers(compare.StatusPass)},
	}
	s := compare.Score(builds, nil, nil, nil)
	want := []compare.GroupRow{
		{League: "3.29", Layer: "tree", Group: pob.GroupSheet, Builds: 2, Pass: 2},
		{League: "3.29", Layer: "tree", Group: compare.GroupChecks, Builds: 2, Pass: 2},
		{League: "3.29", Layer: "full", Group: pob.GroupSkill, Builds: 2, Pass: 2},
		{League: "3.29", Layer: "party", Group: pob.GroupSheet, Builds: 1, NotCompared: 1},
	}
	if diff := cmp.Diff(want, s.Groups); diff != "" {
		t.Errorf("groups (-want +got):\n%s", diff)
	}
	wantKinds := []compare.KindRow{{League: "3.29", Kind: "party", Builds: 2, PassAll: 1}}
	if diff := cmp.Diff(wantKinds, s.Kinds); diff != "" {
		t.Errorf("kinds (-want +got):\n%s", diff)
	}
}
