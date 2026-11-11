package ninja_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/ninja"
)

func cand(kind compare.Kind, skill, asc string, contains ...compare.Content) ninja.Candidate {
	c := ninja.Candidate{Kind: kind, Skill: skill, Ascendancy: asc, Contains: contains}
	c.Char.Name = skill + " " + asc
	return c
}

func names(cs []ninja.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Char.Name)
	}
	return out
}

func TestChooseKinds(t *testing.T) {
	t.Parallel()
	existing := []ninja.Build{{Kind: compare.KindSpell, Skill: "Arc", Ascendancy: "Elementalist"}}
	broken := cand(compare.KindSpell, "Spark", "Inquisitor")
	broken.Err = ninja.ErrLoad
	cands := []ninja.Candidate{
		cand(compare.KindAttack, "Cyclone", "Slayer"),
		cand(compare.KindSpell, "Arc", "Elementalist"), // pair taken by an existing build
		broken,
		cand(compare.KindSpell, "Arc", "Hierophant"),
		cand(compare.KindAttack, "Cyclone", "Slayer"), // pair picked already
		cand(compare.KindAttack, "Frenzy", "Deadeye"),
		cand(compare.KindAttack, "Lacerate", "Gladiator"), // attack is full
		cand(compare.KindParty, "Aura", "Guardian"),
		cand(compare.KindSpell, "Spark", "Inquisitor"),
	}
	targets := map[compare.Kind]int{compare.KindSpell: 3, compare.KindAttack: 2, compare.KindParty: 1}
	p := ninja.Choose(existing, cands, targets, nil)
	want := []string{"Arc Hierophant", "Spark Inquisitor", "Cyclone Slayer", "Frenzy Deadeye"}
	if diff := cmp.Diff(want, names(p.Picked)); diff != "" {
		t.Errorf("picked (-want +got):\n%s", diff)
	}
	if p.Kinds[compare.KindSpell] != 3 || p.Kinds[compare.KindAttack] != 2 || p.Kinds[compare.KindParty] != 0 {
		t.Errorf("kinds %v", p.Kinds)
	}
}

func TestChooseQuotas(t *testing.T) {
	t.Parallel()
	cands := []ninja.Candidate{
		cand(compare.KindAttack, "A", "x", compare.ClusterJewel),
		cand(compare.KindAttack, "B", "x"),
		cand(compare.KindAttack, "C", "x"),
		cand(compare.KindSpell, "D", "x"),
		cand(compare.KindAttack, "E", "x", compare.TimelessJewel),
		cand(compare.KindAttack, "F", "x", compare.TimelessJewel, compare.ClusterJewel),
		cand(compare.KindMinion, "G", "x", compare.Mercenary),
	}
	targets := map[compare.Kind]int{compare.KindAttack: 3, compare.KindSpell: 1}
	quotas := map[compare.Content]int{compare.ClusterJewel: 1, compare.TimelessJewel: 2, compare.Mercenary: 1}
	p := ninja.Choose(nil, cands, targets, quotas)
	// C, then B, make room for the timeless jewels; A keeps the only cluster jewel until F
	// brings another. Minions have no target, so the mercenary quota stays short.
	want := []string{"D x", "A x", "E x", "F x"}
	if diff := cmp.Diff(want, names(p.Picked)); diff != "" {
		t.Errorf("picked (-want +got):\n%s", diff)
	}
	if p.Contents[compare.TimelessJewel] != 2 || p.Contents[compare.Mercenary] != 0 {
		t.Errorf("contents %v", p.Contents)
	}
}
