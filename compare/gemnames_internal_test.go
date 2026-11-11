package compare

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
)

const gemDir = "Metadata/Items/Gems/"

// gemStore is a dataset with a gem with a transfigured variant, a support, and a gem with no
// English name.
func gemStore(t *testing.T) *dataset.Store {
	t.Helper()
	effect := func(id string) dataset.GemEffect {
		return dataset.GemEffect{ID: id, GrantedEffect: id, Style: dataset.DefaultStyle}
	}
	granted := func(id, active string) dataset.GrantedEffect {
		return dataset.GrantedEffect{ID: id, Support: active == "", ActiveSkill: active, Levels: []dataset.EffectLevel{{Level: 1}}}
	}
	d := &dataset.Dataset{
		Manifest: dataset.Manifest{Game: "poe1", Version: "3.29.3.6"},
		Texts: []dataset.Text{{
			Language: "en",
			GemNames: []dataset.Name{
				{ID: gemDir + "SkillGemGroundSlam", Name: "Ground Slam"},
				{ID: gemDir + "SupportGemMeleeSplash", Name: "Melee Splash Support"},
			},
			SkillNames: []dataset.Name{
				{ID: "ground_slam", Name: "Ground Slam"},
				{ID: "ground_slam_alt_y", Name: "Ground Slam of Earthshaking"},
			},
		}},
		Skills: &dataset.Skills{
			Types: []string{"Attack"},
			Gems: []dataset.Gem{
				{ID: gemDir + "SkillGemGroundSlam", Colour: dataset.Red, Effects: []dataset.GemEffect{effect("GroundSlam"), effect("GroundSlamAltY")}},
				{ID: gemDir + "SupportGemMeleeSplash", Colour: dataset.Red, Support: true, Effects: []dataset.GemEffect{effect("SupportMeleeSplash")}},
				{ID: gemDir + "SkillGemNameless", Colour: dataset.Red, Effects: []dataset.GemEffect{effect("Nameless")}},
			},
			GrantedEffects: []dataset.GrantedEffect{
				granted("GroundSlam", "ground_slam"), granted("GroundSlamAltY", "ground_slam_alt_y"),
				granted("SupportMeleeSplash", ""), granted("Nameless", "nameless"),
			},
			ActiveSkills: []dataset.ActiveSkill{{ID: "ground_slam"}, {ID: "ground_slam_alt_y"}, {ID: "nameless"}},
		},
	}
	dir := t.TempDir()
	if _, err := dataset.Write(dir, d); err != nil {
		t.Fatal(err)
	}
	s, err := dataset.Open(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGemNames(t *testing.T) {
	t.Parallel()
	s := gemStore(t)
	gem := func(id, effect string) build.Gem {
		return build.Gem{ID: gemDir + id, Effect: effect, Level: 20}
	}
	skills := build.Skills{Groups: []build.SkillGroup{
		{Gems: []build.Gem{gem("SkillGemGroundSlam", "GroundSlamAltY"), gem("SupportGemMeleeSplash", "")}},
		{Gems: []build.Gem{gem("SkillGemGroundSlam", ""), gem("SkillGemGroundSlam", "GroundSlam"), gem("SkillGemNameless", "")}},
	}}
	got, err := gemNames(skills, s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Ground Slam of Earthshaking", "Melee Splash", "Ground Slam", "Ground Slam", gemDir + "SkillGemNameless"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("names (-want +got):\n%s", diff)
	}
	if got, err := gemNames(build.Skills{}, s); err != nil || got != nil {
		t.Errorf("no skills: got %v, %v; want nil, nil", got, err)
	}
}
