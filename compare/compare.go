// Package compare computes pbt-engine's character sheet for each conformance build and
// compares it with the values PoB computed for the same build, as stored in pob.json.
package compare

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	"github.com/xaroth/pbt-engine/resolve"
	rules "github.com/xaroth/pbt-engine/rules/poe1"
	"github.com/xaroth/pbt-engine/semantics"
	sempoe1 "github.com/xaroth/pbt-engine/semantics/poe1"
)

// Errors of a comparison run; match them with errors.Is.
var (
	// ErrDataset marks a dataset that is missing or not the pinned one.
	ErrDataset = errors.New("dataset not as pinned")
	errNoKey   = errors.New("no pob key for output")
)

// tolerance absorbs float noise; values are compared at the precision both sides round to.
const tolerance = 1e-9

// pobValue returns the PoB value of a catalogue output from PoB's outputs, and false when
// PoB has none.
type pobValue func(map[string]float64) (float64, bool)

func key(k string) pobValue {
	return func(o map[string]float64) (float64, bool) {
		v, ok := o[k]
		return v, ok
	}
}

// maximum is <elem>Resist + Missing<elem>Resist: PoB has no plain maximum resistance key.
func maximum(elem string) pobValue {
	return func(o map[string]float64) (float64, bool) {
		v, ok1 := o[elem+"Resist"]
		m, ok2 := o["Missing"+elem+"Resist"]
		return v + m, ok1 && ok2
	}
}

// keyMap maps each pbt-engine catalogue output to its PoB value.
func keyMap() map[string]pobValue {
	m := map[string]pobValue{
		"player.strength": key("Str"), "player.dexterity": key("Dex"), "player.intelligence": key("Int"),
		"player.life": key("Life"), "player.mana": key("Mana"), "player.energy_shield": key("EnergyShield"),
		"player.armour": key("Armour"), "player.evasion_rating": key("Evasion"),
		"player.life_regeneration": key("LifeRegen"), "player.mana_regeneration": key("ManaRegen"),
		"player.energy_shield_regeneration": key("EnergyShieldRegen"),
	}
	for _, e := range []struct{ ours, pob string }{{"fire", "Fire"}, {"cold", "Cold"}, {"lightning", "Lightning"}, {"chaos", "Chaos"}} {
		m["player."+e.ours+"_resistance"] = key(e.pob + "Resist")
		m["player.maximum_"+e.ours+"_resistance"] = maximum(e.pob)
		m["player.uncapped_"+e.ours+"_resistance"] = key(e.pob + "ResistTotal")
	}
	return m
}

// Mismatch is one output of one build where pbt-engine and PoB differ.
type Mismatch struct {
	League, Build, Output string
	// Ours is pbt-engine's value; PoB is PoB's for the tree-only build, which is what the
	// engine models, and PoBFull PoB's for the full build. HasPoB is false when PoB has no value.
	Ours, PoB, PoBFull float64
	HasPoB             bool
	// Triage is the id of the triage entry that accepts the mismatch, empty if none.
	Triage string
}

// Run compares every build of league under root, with the pinned dataset under datasets.
// A build that cannot be compared at all (unreadable XML, failing build check) is an error.
func Run(root, datasets string, f *pins.File, game, league string) ([]Mismatch, error) {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return nil, fmt.Errorf("compare: %s %s: %w", game, league, ErrDataset)
	}
	s, err := openPinned(datasets, l.Dataset)
	if err != nil {
		return nil, err
	}
	t, err := sempoe1.Load()
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	dirs, err := pob.BuildDirs(root, league)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	var out []Mismatch
	for _, dir := range dirs {
		ms, err := compareBuild(dir, game, league, l, s, t)
		if err != nil {
			return nil, fmt.Errorf("compare %s: %w", filepath.Base(dir), err)
		}
		out = append(out, ms...)
	}
	return out, nil
}

func openPinned(datasets string, d pins.Dataset) (*dataset.Store, error) {
	s, err := dataset.Open(os.DirFS(filepath.Join(datasets, d.Version)))
	if err != nil {
		return nil, fmt.Errorf("compare: %w: %w", ErrDataset, err)
	}
	if id := s.Manifest().ID; id != d.ID {
		return nil, fmt.Errorf("compare: %w: %s has id %s, pinned %s", ErrDataset, d.Version, id, d.ID)
	}
	return s, nil
}

func compareBuild(dir, game, league string, l pins.League, s *dataset.Store, t *semantics.Table) ([]Mismatch, error) {
	xmlData, err := os.ReadFile(filepath.Join(dir, pob.TreeOnlyXML))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	b, err := FromPoB(xmlData, game, l.Dataset.Version, l.Tree, s)
	if err != nil {
		return nil, err
	}
	sh, err := sheet(b, s, t)
	if err != nil {
		return nil, err
	}
	v, err := readValues(dir)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(dir)
	var out []Mismatch
	if v.TreeOnly.Allocated != len(b.Nodes) {
		out = append(out, Mismatch{
			League: league, Build: name, Output: "allocated_nodes",
			Ours: float64(len(b.Nodes)), PoB: float64(v.TreeOnly.Allocated), PoBFull: float64(v.Full.Allocated), HasPoB: true,
		})
	}
	ms, err := compareOutputs(sh, v, league, name)
	return append(out, ms...), err
}

// compareOutputs compares every catalogue output of sh with PoB's values v.
func compareOutputs(sh *rules.Sheet, v *pob.Values, league, name string) ([]Mismatch, error) {
	var out []Mismatch
	for _, o := range rules.Outputs() {
		get, ok := keyMap()[o.Name]
		if !ok {
			return nil, fmt.Errorf("compare: %w %s", errNoKey, o.Name)
		}
		ours, err := sh.Output(o.Name)
		if err != nil {
			return nil, fmt.Errorf("compare: %w", err)
		}
		p, has := get(v.TreeOnly.Outputs)
		full, _ := get(v.Full.Outputs)
		if has && math.Abs(ours-p) <= tolerance {
			continue
		}
		out = append(out, Mismatch{League: league, Build: name, Output: o.Name, Ours: ours, PoB: p, PoBFull: full, HasPoB: has})
	}
	return out, nil
}

func sheet(b *build.Build, s *dataset.Store, t *semantics.Table) (*rules.Sheet, error) {
	c, err := build.Check(b, s)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	r, err := resolve.Resolve(c)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	sh, err := rules.Open(r, t)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	return sh, nil
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
