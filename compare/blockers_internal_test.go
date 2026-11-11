package compare

// Internal: the blockers are read from warning messages, built here as the engine writes them.

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-engine/build"
	pobimport "github.com/xaroth/pbt-engine/pob"
	"github.com/xaroth/pbt-engine/simulator"
)

func TestBlockers(t *testing.T) {
	t.Parallel()
	sim := []build.Warning{
		{Code: simulator.UnmodelledStat, Subject: "spell_damage_+% from node 57264", Message: "no entry, left out"},
		{Code: simulator.UnmodelledStat, Subject: "spell_damage_+% from node 11420", Message: "no entry, left out"},
		{Code: simulator.UnmodelledStat, Subject: "armour_+%_per_endurance_charge from node 65485",
			Message: "not applied: charges: endurance charges are not counted, left out"},
		{Code: simulator.UncheckedCondition, Subject: "maximum_life_+% from node 4139", Message: "applied without checking: x"},
	}
	full := []build.Warning{
		{Code: pobimport.SkillIgnored, Subject: "Penance Brand", Message: "skills are not imported (Penance Brand, Cruelty)"},
		{Code: pobimport.SkillIgnored, Subject: "Flame Dash", Message: "skills are not imported (Flame Dash, Cruelty)"},
		{Code: pobimport.ItemIgnored, Subject: "Doom Crown", Message: "items are not imported"},
		{Code: pobimport.ClusterNodeDropped, Subject: "node 65601", Message: "dropped"},
	}
	want := []Blocker{
		{BlockerGem, "Cruelty"}, {BlockerGem, "Flame Dash"}, {BlockerGem, "Penance Brand"},
		{BlockerMechanic, "charges"}, {BlockerMechanic, "items"}, {BlockerMechanic, "skills"},
		{BlockerStat, "armour_+%_per_endurance_charge"}, {BlockerStat, "spell_damage_+%"},
	}
	if diff := cmp.Diff(want, blockers(sim, full)); diff != "" {
		t.Errorf("blockers (-want +got):\n%s", diff)
	}
}
