// Package compare computes pbt-engine's character sheet for each conformance build and
// compares it with the values PoB computed for the same build, as stored in pob.json.
package compare

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	pobimport "github.com/xaroth/pbt-engine/pob"
	"github.com/xaroth/pbt-engine/simulator"
)

// Errors of a comparison run; match them with errors.Is.
var (
	// ErrDataset marks a dataset that is missing or not the pinned one.
	ErrDataset = errors.New("dataset not as pinned")
	// ErrLeague marks a build whose tree is not of the league it is filed under.
	ErrLeague = errors.New("build tree not of its league")
	errNoKey  = errors.New("no pob key for output")
)

// tolerance absorbs float noise in the values of triage entries.
const tolerance = 1e-9

// pobOutputs indexes the outputs of pob.Outputs by name.
func pobOutputs() map[string]pob.Output {
	m := map[string]pob.Output{}
	for _, o := range pob.Outputs() {
		m[o.Name] = o
	}
	return m
}

// Mismatch is one output of one build where pbt-engine and PoB differ, or a simulation warning
// (Output "warning:<code>", Detail its subject and message) that must be triaged like one.
type Mismatch struct {
	League string `json:"league"`
	Build  string `json:"build"`
	Output string `json:"output"`
	// Ours is pbt-engine's value; PoB is PoB's for the layer the output is compared on, the
	// tree-only build for the sheet and defences and the skill build for skill outputs, and
	// PoBFull PoB's for the full build. HasPoB is false when PoB has no value.
	Ours    float64 `json:"ours"`
	PoB     float64 `json:"pob"`
	PoBFull float64 `json:"pobFull"`
	HasPoB  bool    `json:"hasPoB"`
	// Triage is the id of the triage entry that accepts the mismatch, empty if none.
	Triage string `json:"triage,omitempty"`
	// Subject is what an import or warning mismatch is about, such as a stat and its node;
	// Detail is the subject with the message.
	Subject string `json:"subject,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Output prefixes of warning and import mismatches.
const (
	warningPrefix = "warning:"
	importPrefix  = "import:"
	// fullTree is the import mismatch of a full build whose tree differs from its tree-only one.
	fullTree = importPrefix + "full-tree"
)

// fullWarnings are the import warnings a full build may give: what it holds beside its tree.
func fullWarnings() []build.Code {
	return []build.Code{
		pobimport.ItemIgnored, pobimport.SkillIgnored, pobimport.JewelIgnored,
		pobimport.ConfigIgnored, pobimport.PantheonIgnored,
	}
}

// Build is the comparison of one build.
type Build struct {
	League, Name string
	Meta         Meta
	// Mismatches are the build's mismatches, in the order compare lists them.
	Mismatches []Mismatch
	// Layers judge each layer's groups of outputs, in the order of Layers().
	Layers []Layer
	// Blockers are what the engine left out as not modelled for the build, sorted.
	Blockers []Blocker
}

// Run compares every build of league under root, with the pinned dataset under datasets.
// A build that cannot be compared at all (unreadable XML, failing build check) is an error.
func Run(root, datasets string, f *pins.File, game, league string) ([]Build, error) {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return nil, fmt.Errorf("compare: %s %s: %w", game, league, ErrDataset)
	}
	reg, s, err := openPinned(datasets, game, l.Dataset)
	if err != nil {
		return nil, err
	}
	sim, err := simulator.New(s)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	dirs, err := pob.BuildDirs(root, league)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	out := make([]Build, 0, len(dirs))
	for _, dir := range dirs {
		b, err := compareBuild(dir, league, reg, sim)
		if err != nil {
			return nil, fmt.Errorf("compare %s: %w", filepath.Base(dir), err)
		}
		out = append(out, *b)
	}
	return out, nil
}

// Mismatches returns the mismatches of builds, in order.
func Mismatches(builds []Build) []Mismatch {
	var out []Mismatch
	for _, b := range builds {
		out = append(out, b.Mismatches...)
	}
	return out
}

// openPinned opens the datasets and the pinned one among them, checking its id.
func openPinned(datasets, game string, d pins.Dataset) (*dataset.Registry, *dataset.Store, error) {
	reg, s, err := openVersion(datasets, game, d.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("compare: %w: %w", ErrDataset, err)
	}
	if id := s.Manifest().ID; id != d.ID {
		return nil, nil, fmt.Errorf("compare: %w: %s has id %s, pinned %s", ErrDataset, d.Version, id, d.ID)
	}
	return reg, s, nil
}

func compareBuild(dir, league string, reg *dataset.Registry, sim *simulator.Sim) (*Build, error) {
	name := filepath.Base(dir)
	meta, err := ReadMeta(dir)
	if err != nil {
		return nil, err
	}
	b, full, fullWarns, out, err := importBoth(dir, league, name, reg)
	if err != nil {
		return nil, err
	}
	s := sim.Store()
	c, _, err := build.Check(b, s)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	st, err := sim.Simulate(b)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	v, err := readValues(dir)
	if err != nil {
		return nil, err
	}
	out = append(out, allocatedMismatches(c, v, league, name)...)
	out = append(out, warnings(st, league, name)...)
	ours, err := oursOf(dir, reg, sim, st, full)
	if err != nil {
		return nil, err
	}
	ms, err := compareOutputs(sim.Outputs(), ours, v, league, name)
	if err != nil {
		return nil, err
	}
	gems, err := gemNames(full.Skills, s)
	if err != nil {
		return nil, err
	}
	return &Build{
		League: league, Name: name, Meta: meta, Mismatches: append(out, ms...),
		Layers: judgeLayers(v, ours), Blockers: blockers(st.Warnings(), fullWarns, gems),
	}, nil
}

// importBoth imports a build's tree-only and full files and checks the imports.
func importBoth(dir, league, name string, reg *dataset.Registry) (
	tree, full *build.Build, fullWarns []build.Warning, mismatches []Mismatch, err error,
) {
	tree, treeWarnings, err := importFile(dir, pob.TreeOnlyXML, reg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if tree.Version != league {
		return nil, nil, nil, nil, fmt.Errorf("compare: %w: tree %s, league %s", ErrLeague, tree.Version, league)
	}
	full, fullWarns, err = importFile(dir, pob.FullXML, reg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return tree, full, fullWarns, importMismatches(league, name, tree, treeWarnings, full, fullWarns), nil
}

// allocatedMismatches reports a tree whose node count differs from PoB's.
func allocatedMismatches(c *build.Checked, v *pob.Values, league, name string) []Mismatch {
	n := Allocated(c)
	if v.TreeOnly.Allocated == n {
		return nil
	}
	return []Mismatch{{
		League: league, Build: name, Output: "allocated_nodes",
		Ours: float64(n), PoB: float64(v.TreeOnly.Allocated), PoBFull: float64(v.Full.Allocated), HasPoB: true,
	}}
}

// ours holds pbt-engine's values per layer: every output for the tree-only build, and the
// outputs that read skills (pob.Group.ReadsSkills) for the skill and full builds.
type ours struct {
	tree, skill, full map[string]float64
}

// oursOf simulates the skill and full builds of the build in dir, the tree-only one st already
// simulated, while the engine gives skill outputs.
func oursOf(dir string, reg *dataset.Registry, sim *simulator.Sim, st *simulator.Stats, full *build.Build) (ours, error) {
	o := ours{tree: engineValues(sim.Outputs(), st)}
	var skillOutputs []simulator.Output
	table := pobOutputs()
	for _, out := range sim.Outputs() {
		if get, ok := table[string(out.Key)]; ok && get.Group.ReadsSkills() {
			skillOutputs = append(skillOutputs, out)
		}
	}
	if len(skillOutputs) == 0 {
		return o, nil
	}
	skill, _, err := importFile(dir, pob.SkillXML, reg)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// A build recorded without a skill layer has none to compare.
	case err != nil:
		return ours{}, err
	default:
		if o.skill, err = simulateValues(sim, skillOutputs, skill); err != nil {
			return ours{}, err
		}
	}
	if o.full, err = simulateValues(sim, skillOutputs, full); err != nil {
		return ours{}, err
	}
	return o, nil
}

func simulateValues(sim *simulator.Sim, outputs []simulator.Output, b *build.Build) (map[string]float64, error) {
	st, err := sim.Simulate(b)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	return engineValues(outputs, st), nil
}

// engineValues returns the value of each of outputs st gives. The engine gives some outputs only
// for some builds, such as those of a main skill; one it does not give is left out, so it is not
// compared and judged as missing.
func engineValues(outputs []simulator.Output, st *simulator.Stats) map[string]float64 {
	m := make(map[string]float64, len(outputs))
	for _, o := range outputs {
		if v, ok := st.Get(o.Key); ok {
			m[string(o.Key)] = v
		}
	}
	return m
}

func importFile(dir, file string, reg *dataset.Registry) (*build.Build, []build.Warning, error) {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}
	b, ws, err := pobimport.FromXML(data, reg)
	if err != nil {
		return nil, nil, fmt.Errorf("import %s: %w", file, err)
	}
	return b, ws, nil
}

// importMismatches checks the imports of a build: the tree-only file imports without warnings,
// and the full file to the same tree with warnings only for what it holds beside the tree.
// Config is not compared: tree-only files keep PoB's default bandit choice.
func importMismatches(league, name string, tree *build.Build, treeWarnings []build.Warning,
	full *build.Build, fullWarns []build.Warning,
) []Mismatch {
	var out []Mismatch
	add := func(w build.Warning, file string) {
		out = append(out, Mismatch{
			League: league, Build: name, Output: importPrefix + string(w.Code),
			Subject: w.Subject, Detail: file + ": " + w.Subject + ": " + w.Message,
		})
	}
	for _, w := range treeWarnings {
		add(w, pob.TreeOnlyXML)
	}
	for _, w := range fullWarns {
		if !slices.Contains(fullWarnings(), w.Code) {
			add(w, pob.FullXML)
		}
	}
	if !sameTree(tree, full) {
		out = append(out, Mismatch{
			League: league, Build: name, Output: fullTree, Subject: pob.FullXML,
			Detail: pob.FullXML + " imports to another tree than " + pob.TreeOnlyXML,
		})
	}
	return out
}

// sameTree compares what a build holds beside config.
func sameTree(a, b *build.Build) bool {
	return a.Game == b.Game && a.Version == b.Version && a.Variant == b.Variant && a.Class == b.Class &&
		a.Ascendancy == b.Ascendancy && a.Level == b.Level && slices.Equal(a.Nodes, b.Nodes) && maps.Equal(a.Masteries, b.Masteries)
}

// ignoredByPoB are the check's warnings about choices PoB ignores too: an override on a node
// that is not allocated, and a mastery effect on a mastery an override replaces.
func ignoredByPoB() []build.Code {
	return []build.Code{build.OverrideNotAllocated, build.MasteryOverridden}
}

// warnings turns the simulation's warnings into mismatches, except stats left out as not
// modelled, which the output mismatches show, and choices PoB ignores too: a dropped node or
// mastery choice would otherwise change values silently and land on an entry for another cause.
func warnings(st *simulator.Stats, league, name string) []Mismatch {
	var out []Mismatch
	for _, w := range st.Warnings() {
		if w.Code == simulator.UnmodelledStat || slices.Contains(ignoredByPoB(), w.Code) {
			continue
		}
		out = append(out, Mismatch{
			League: league, Build: name, Output: warningPrefix + string(w.Code),
			Subject: w.Subject, Detail: w.Subject + ": " + w.Message,
		})
	}
	return out
}

// skillPrefix marks the engine's outputs of the main skill, which pob.Outputs maps only in part.
const skillPrefix = "skill."

// compareOutputs compares every catalogue output with PoB's values v: the sheet and defences of
// the tree-only build, and skill outputs of the skill build once the engine simulates it. A
// skill output with no PoB key in pob.Outputs is not compared; any other is an error, so a new
// sheet or defence output cannot go unchecked.
func compareOutputs(outputs []simulator.Output, o ours, v *pob.Values, league, name string) ([]Mismatch, error) {
	var out []Mismatch
	table := pobOutputs()
	for _, so := range outputs {
		get, ok := table[string(so.Key)]
		if !ok {
			if strings.HasPrefix(string(so.Key), skillPrefix) {
				continue
			}
			return nil, fmt.Errorf("compare: %w %s", errNoKey, so.Key)
		}
		values, layer := o.tree, v.TreeOnly
		if get.Group.ReadsSkills() {
			values, layer = o.skill, v.Skill
		}
		ours, given := values[string(so.Key)]
		if !given {
			continue
		}
		p, has := get.Value(layer.Outputs)
		full, _ := get.Value(v.Full.Outputs)
		if has && get.Matches(ours, p) {
			continue
		}
		out = append(out, Mismatch{League: league, Build: name, Output: string(so.Key), Ours: ours, PoB: p, PoBFull: full, HasPoB: has})
	}
	return out, nil
}

// Allocated counts the build's nodes as PoB does: PoB always allocates the class start and,
// with an ascendancy, the ascendancy start, whether the build lists them or not.
func Allocated(c *build.Checked) int {
	ids := map[uint32]bool{}
	for _, n := range c.Nodes() {
		ids[n.ID] = true
	}
	if cl := c.Class(); cl != nil {
		ids[cl.StartNode] = true
	}
	if asc := c.Build().Ascendancy; asc != "" {
		for i := range c.Tree().Nodes {
			if n := &c.Tree().Nodes[i]; n.Kind == dataset.AscendancyStart && n.Ascendancy == asc {
				ids[n.ID] = true
			}
		}
	}
	return len(ids)
}

func readValues(dir string) (*pob.Values, error) {
	b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
	if err != nil {
		return nil, fmt.Errorf("read pob values: %w", err)
	}
	var v pob.Values
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("pob values: %w", err)
	}
	return &v, nil
}

// OpenVersion opens game's dataset of exactly version among the datasets in the directories of
// dir: pulled from pbt-data (one per league) or one per version.
func OpenVersion(dir, game, version string) (*dataset.Store, error) {
	_, s, err := openVersion(dir, game, version)
	return s, err
}

func openVersion(dir, game, version string) (*dataset.Registry, *dataset.Store, error) {
	reg, err := dataset.OpenRoot(os.DirFS(dir))
	if reg == nil {
		return nil, nil, fmt.Errorf("open %s: %w", dir, err)
	}
	res, err := reg.Resolve(game, version)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", version, err)
	}
	if got := res.Store.Manifest().Version; got != version {
		return nil, nil, fmt.Errorf("%s has %s, not %s: %w", dir, got, version, ErrDataset)
	}
	return reg, res.Store, nil
}
