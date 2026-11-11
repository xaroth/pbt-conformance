package compare

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	pobimport "github.com/xaroth/pbt-engine/pob"
	"github.com/xaroth/pbt-engine/simulator"
)

// BlockerKind is what a blocker is.
type BlockerKind string

// Kinds of blocker.
const (
	// BlockerStat is a game stat the engine gives no meaning yet; Name is its id.
	BlockerStat BlockerKind = "stat"
	// BlockerGem is a gem the engine does not model yet; Name is the gem's name.
	BlockerGem BlockerKind = "gem"
	// BlockerMechanic is a mechanic the engine does not model yet: what a stat it knows waits
	// for (charges, items, a condition), or a part of the build file it does not import.
	BlockerMechanic BlockerKind = "mechanic"
)

// Blocker is something the engine left out as not modelled for a build.
type Blocker struct {
	Kind BlockerKind `json:"kind"`
	Name string      `json:"name"`
}

func compareBlockers(a, b Blocker) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
}

// importMechanics names the parts of a full build file the importer leaves out.
func importMechanics() map[build.Code]string {
	return map[build.Code]string{
		pobimport.ItemIgnored: "items", pobimport.JewelIgnored: "jewels", pobimport.SkillIgnored: "skills",
		pobimport.ConfigIgnored: "config", pobimport.PantheonIgnored: "pantheon",
	}
}

// blockers returns, sorted and once each, what the tree-only simulation left out (its
// unmodelled-stat warnings), what the full file's import left out, and the gems of the full
// file, which the engine imports but does not simulate yet.
func blockers(simWarnings, fullImport []build.Warning, fullGems []string) []Blocker {
	var out []Blocker
	for _, w := range simWarnings {
		if w.Code != simulator.UnmodelledStat {
			continue
		}
		stat, _, _ := strings.Cut(w.Subject, " from ")
		out = append(out, Blocker{BlockerStat, stat})
		if m, ok := waitsFor(w.Message); ok {
			out = append(out, Blocker{BlockerMechanic, m})
		}
	}
	for _, w := range fullImport {
		if m, ok := importMechanics()[w.Code]; ok {
			out = append(out, Blocker{BlockerMechanic, m})
		}
	}
	if len(fullGems) > 0 {
		out = append(out, Blocker{BlockerMechanic, "skills"})
	}
	for _, g := range fullGems {
		out = append(out, Blocker{BlockerGem, g})
	}
	slices.SortFunc(out, compareBlockers)
	return slices.Compact(out)
}

// waitsFor returns the mechanic an unmodelled-stat warning's message names: the engine writes
// "not applied: <mechanic>: <text>, left out" for a stat it knows but cannot apply yet, and
// "no entry, left out" for one it has no meaning for.
func waitsFor(msg string) (string, bool) {
	rest, ok := strings.CutPrefix(msg, "not applied: ")
	if !ok {
		return "", false
	}
	m, _, _ := strings.Cut(strings.TrimSuffix(rest, ", left out"), ":")
	return m, m != ""
}

// gemNames returns the English name of every gem in skills: a transfigured gem's by the skill
// its variant grants, a support's without " Support". A gem without a name keeps its id.
func gemNames(skills build.Skills, store *dataset.Store) ([]string, error) {
	var gems []build.Gem
	for _, g := range skills.Groups {
		gems = append(gems, g.Gems...)
	}
	if len(gems) == 0 {
		return nil, nil
	}
	sk, err := store.Skills()
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	text, err := store.Text("en")
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	gemName, skillName := names(text.GemNames), names(text.SkillNames)
	out := make([]string, 0, len(gems))
	for _, g := range gems {
		name := cmp.Or(gemName[g.ID], g.ID)
		if active := variantSkill(sk, g.GemID); active != "" {
			name = cmp.Or(skillName[active], name)
		}
		out = append(out, strings.TrimSuffix(name, " Support"))
	}
	return out, nil
}

// variantSkill returns the active skill a gem's variant grants, empty for its first variant.
func variantSkill(sk *dataset.Skills, id build.GemID) string {
	gem := sk.Gem(id.ID)
	if id.Effect == "" || gem == nil {
		return ""
	}
	e := gem.Effect(id.Effect)
	if e == nil {
		return ""
	}
	i := slices.IndexFunc(sk.GrantedEffects, func(ge dataset.GrantedEffect) bool { return ge.ID == e.GrantedEffect })
	if i < 0 {
		return ""
	}
	return sk.GrantedEffects[i].ActiveSkill
}

func names(ns []dataset.Name) map[string]string {
	m := make(map[string]string, len(ns))
	for _, n := range ns {
		m[n.ID] = n.Name
	}
	return m
}
