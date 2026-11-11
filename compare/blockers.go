package compare

import (
	"cmp"
	"slices"
	"strings"

	"github.com/xaroth/pbt-engine/build"
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
// unmodelled-stat warnings) and what the full file's import left out.
func blockers(simWarnings, fullImport []build.Warning) []Blocker {
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
		m, ok := importMechanics()[w.Code]
		if !ok {
			continue
		}
		out = append(out, Blocker{BlockerMechanic, m})
		if w.Code == pobimport.SkillIgnored {
			for _, g := range gems(w.Message) {
				out = append(out, Blocker{BlockerGem, g})
			}
		}
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

// gems returns the gems of a skill-ignored warning's message, which lists them in
// parentheses: "skills are not imported (Fireball, Spell Echo)".
func gems(msg string) []string {
	_, list, ok := strings.Cut(msg, "(")
	if !ok {
		return nil
	}
	list = strings.TrimSuffix(list, ")")
	var out []string
	for g := range strings.SplitSeq(list, ", ") {
		if g != "" {
			out = append(out, g)
		}
	}
	return out
}
