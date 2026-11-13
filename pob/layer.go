package pob

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// choiceAttr matches the <Build> choices that are config in PoB, reset to PoB's default as in
// the tree-only files.
var choiceAttr = regexp.MustCompile(`(\s(?:bandit|pantheonMajorGod|pantheonMinorGod))="[^"]*"`)

var (
	mainGroupAttr = regexp.MustCompile(`(\smainSocketGroup)="[^"]*"`)
	enabledAttr   = regexp.MustCompile(`(\senabled)="[^"]*"`)
)

var errNoRoot = errors.New("no PathOfBuilding element")

// layer is what a derived file keeps of the full build.
type layer int

const (
	layerTreeOnly layer = iota
	layerGear
	layerSkill
)

// Top-level elements the skill layer filters rather than keeps or drops whole.
const (
	elemSkills = "Skills"
	elemConfig = "Config"
)

// enemyInputs returns the config inputs of PoB's "Enemy Stats" section, which the skill layer
// keeps.
func enemyInputs() []string {
	return []string{
		"enemyLevel", "conditionEnemyRareOrUnique", "enemyIsBoss", "deliriousPercentage", "enemyPhysicalReduction",
		"enemyLightningResist", "enemyColdResist", "enemyFireResist", "enemyChaosResist", "enemyMaxResist",
		"enemyBlockChance", "enemyEvasion", "enemyArmour", "presetBossSkills", "enemyDamageRollRange",
		"enemyDamageType", "enemySpeed", "enemyMultiplierPvpDamage", "enemyCritChance", "enemyCritDamage",
		"enemyPhysicalDamage", "enemyPhysicalOverwhelm", "enemyLightningDamage", "enemyLightningPen",
		"enemyColdDamage", "enemyColdPen", "enemyFireDamage", "enemyFirePen", "enemyChaosDamage",
	}
}

// edit replaces full[start:end] with repl.
type edit struct {
	start, end int64
	repl       []byte
}

// Gear derives a build's gear layer from its full PoB XML: the build, its tree with the jewels
// in its sockets, and its items without flasks. Of the skills it keeps only the groups of the
// active skill set granted by items or the tree, disabled with their gems, since PoB would add
// them back enabled; a disabled group still counts when mainSocketGroup points at it, which it
// does once the other groups are gone. Config and every other top-level element are dropped,
// and the bandit and pantheon choices on <Build> are reset to None. The rest is kept byte for
// byte.
func Gear(full []byte) ([]byte, error) {
	b, err := derive(full, layerGear)
	if err != nil {
		return nil, fmt.Errorf("pob: gear: %w", err)
	}
	return b, nil
}

// TreeOnly derives a build's tree-only layer from its full PoB XML: the build and its tree with
// no jewels in its sockets. Every other top-level element is dropped and the bandit and pantheon
// choices on <Build> are reset to None, as in Gear.
func TreeOnly(full []byte) ([]byte, error) {
	b, err := derive(full, layerTreeOnly)
	if err != nil {
		return nil, fmt.Errorf("pob: tree only: %w", err)
	}
	return b, nil
}

// Skill derives a build's skill layer from its full PoB XML: the gear layer plus the main socket
// group of the active skill set and the enemy inputs of the active config set. Other skill sets
// and config sets are dropped. Of the other groups of the active set, groups granted by items or
// the tree are kept disabled, since PoB would add them back enabled; disabled groups are dropped;
// enabled ones are kept with only their support gems enabled, since PoB applies supports across
// groups (to a skill an item grants, and through linked slots). A gem is a support when its
// skillId starts with "Support"; a gem without a skillId counts as an active one.
// mainSocketGroup on <Build> is renumbered to the main group's place among the groups kept.
func Skill(full []byte) ([]byte, error) {
	b, err := derive(full, layerSkill)
	if err != nil {
		return nil, fmt.Errorf("pob: skill: %w", err)
	}
	return b, nil
}

func derive(full []byte, l layer) ([]byte, error) {
	edits, err := layerEdits(full, l)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	var out bytes.Buffer
	pos := int64(0)
	for _, e := range edits {
		out.Write(full[pos:e.start])
		out.Write(e.repl)
		pos = e.end
	}
	out.Write(full[pos:])
	return out.Bytes(), nil
}

// layerWalk collects the edits that turn a full XML into one of its layers.
type layerWalk struct {
	layer  layer
	d      *xml.Decoder
	full   []byte
	edits  []edit
	path   []string
	items  map[string]edit
	flasks []string
	root   bool

	// build is the <Build> start tag's edit, finished once the skills are read.
	build edit
	// mainGroup is the main socket group's number in the full file; groups counts the groups of
	// the active skill set and kept those kept; newMain is the main group's number among them.
	mainGroup, groups, kept, newMain int
	activeSkillSet, activeConfigSet  string
}

func layerEdits(full []byte, l layer) ([]edit, error) {
	w := &layerWalk{layer: l, d: xml.NewDecoder(bytes.NewReader(full)), full: full, items: map[string]edit{}}
	for {
		start := w.d.InputOffset()
		tok, err := w.d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := w.open(t, start); err != nil {
				return nil, err
			}
		case xml.EndElement:
			w.path = w.path[:len(w.path)-1]
		}
	}
	if !w.root {
		return nil, errNoRoot
	}
	return w.finish(), nil
}

// finish adds the edits that wait for the whole file: the flasks' items and the <Build> tag.
func (w *layerWalk) finish() []edit {
	for _, id := range w.flasks {
		if e, ok := w.items[id]; ok {
			w.edits = append(w.edits, e)
			delete(w.items, id)
		}
	}
	if w.newMain > 0 {
		w.build.repl = mainGroupAttr.ReplaceAll(w.build.repl, []byte(`$1="`+strconv.Itoa(w.newMain)+`"`))
	}
	if w.build.end > 0 {
		w.edits = append(w.edits, w.build)
	}
	return w.edits
}

// open handles the element whose start tag t began at start: it either skips the element or
// descends into it.
func (w *layerWalk) open(t xml.StartElement, start int64) error {
	var (
		skipped bool
		err     error
	)
	switch {
	case len(w.path) == 0:
		w.root = t.Name.Local == "PathOfBuilding"
	case len(w.path) == 1:
		skipped, err = w.openTop(t, start)
	default:
		skipped, err = w.openIn(w.path[1], t, start)
	}
	if err != nil || skipped {
		return err
	}
	w.path = append(w.path, t.Name.Local)
	return nil
}

// openIn handles an element inside the kept top-level element top.
func (w *layerWalk) openIn(top string, t xml.StartElement, start int64) (bool, error) {
	switch top {
	case "Items":
		return w.openItems(t, start)
	case elemSkills:
		return w.openSkills(t, start)
	case elemConfig:
		return w.openConfig(t, start)
	case "Tree":
		if w.layer == layerTreeOnly && t.Name.Local == "Socket" {
			return true, w.drop(start)
		}
	}
	return false, nil
}

// openTop resets the choices on <Build> and drops every top-level element the layer does not
// keep, so PoB uses its defaults for them.
func (w *layerWalk) openTop(t xml.StartElement, start int64) (bool, error) {
	switch name := t.Name.Local; {
	case name == "Build":
		w.mainGroup, _ = strconv.Atoi(attr(t, "mainSocketGroup"))
		tag := w.full[start:w.d.InputOffset()]
		w.build = edit{start, w.d.InputOffset(), choiceAttr.ReplaceAll(tag, []byte(`$1="None"`))}
		return false, nil
	case name == "Tree",
		name == "Items" && w.layer >= layerGear:
		return false, nil
	case name == elemSkills && w.layer >= layerGear:
		w.activeSkillSet = cmp.Or(attr(t, "activeSkillSet"), "1")
		return false, nil
	case name == elemConfig && w.layer == layerSkill:
		w.activeConfigSet = cmp.Or(attr(t, "activeConfigSet"), "1")
		return false, nil
	}
	return true, w.drop(start)
}

// openItems drops flask slots and notes every item; the flasks' items are dropped once all
// slots are read.
func (w *layerWalk) openItems(t xml.StartElement, start int64) (bool, error) {
	switch {
	case t.Name.Local == "Slot" && strings.HasPrefix(attr(t, "name"), "Flask "):
		w.flasks = append(w.flasks, attr(t, "itemId"))
		return true, w.drop(start)
	case t.Name.Local == "Item":
		e, err := w.skip(start)
		w.items[attr(t, "id")] = e
		return true, err
	}
	return false, nil
}

// openSkills keeps the active skill set's groups as Gear and Skill say.
func (w *layerWalk) openSkills(t xml.StartElement, start int64) (bool, error) {
	parent := w.path[len(w.path)-1]
	switch {
	case t.Name.Local == "SkillSet" && parent == elemSkills:
		if attr(t, "id") == w.activeSkillSet {
			return false, nil
		}
		return true, w.drop(start)
	case t.Name.Local == "Skill" && (parent == "SkillSet" || parent == elemSkills):
		return w.group(t, start)
	case t.Name.Local == "Gem" && parent == "Skill" && w.disablesGem(t):
		w.disable(start)
	}
	return false, nil
}

// disablesGem reports whether gem t of a group descended into is disabled. The gear layer
// descends only into granted groups and disables every gem; the skill layer only into groups
// kept with their supports alone.
func (w *layerWalk) disablesGem(t xml.StartElement) bool {
	return w.layer == layerGear || !strings.HasPrefix(attr(t, "skillId"), "Support")
}

// group keeps or drops the skill group t of the active skill set; it descends into a group whose
// gems it disables.
func (w *layerWalk) group(t xml.StartElement, start int64) (bool, error) {
	w.groups++
	granted := attr(t, "source") != ""
	switch {
	case w.layer == layerGear && granted:
		w.disable(start)
		return false, nil
	case w.layer == layerGear:
		return true, w.drop(start)
	case w.groups == w.mainGroup:
		w.newMain = w.kept + 1
	case granted:
		w.disable(start)
	case attr(t, "enabled") != "true" && attr(t, "active") != "true":
		return true, w.drop(start)
	default:
		w.kept++
		return false, nil
	}
	w.kept++
	if err := w.d.Skip(); err != nil {
		return true, fmt.Errorf("reading xml: %w", err)
	}
	return true, nil
}

// disable sets enabled="false" on the start tag that began at start and was just read.
func (w *layerWalk) disable(start int64) {
	end := w.d.InputOffset()
	tag := w.full[start:end]
	repl := enabledAttr.ReplaceAll(tag, []byte(`$1="false"`))
	if !enabledAttr.Match(tag) {
		i := bytes.IndexAny(tag, " \t\n/>")
		repl = slices.Concat(tag[:i], []byte(` enabled="false"`), tag[i:])
	}
	w.edits = append(w.edits, edit{start, end, repl})
}

// openConfig keeps the enemy inputs of the active config set.
func (w *layerWalk) openConfig(t xml.StartElement, start int64) (bool, error) {
	parent := w.path[len(w.path)-1]
	switch {
	case t.Name.Local == "ConfigSet" && parent == elemConfig:
		if attr(t, "id") == w.activeConfigSet {
			return false, nil
		}
	case t.Name.Local == "Input" && slices.Contains(enemyInputs(), attr(t, "name")):
		return false, nil
	}
	return true, w.drop(start)
}

// drop removes the element whose start tag began at start.
func (w *layerWalk) drop(start int64) error {
	e, err := w.skip(start)
	w.edits = append(w.edits, e)
	return err
}

// skip reads past the element whose start tag began at start and returns its removal.
func (w *layerWalk) skip(start int64) (edit, error) {
	if err := w.d.Skip(); err != nil {
		return edit{}, fmt.Errorf("reading xml: %w", err)
	}
	return wholeLine(w.full, start, w.d.InputOffset()), nil
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// wholeLine widens the removal of full[start:end] to the whole line when the element stands
// alone on it, so no blank line is left behind.
func wholeLine(full []byte, start, end int64) edit {
	for start > 0 && (full[start-1] == ' ' || full[start-1] == '\t') {
		start--
	}
	for end < int64(len(full)) && (full[end] == ' ' || full[end] == '\t' || full[end] == '\r') {
		end++
	}
	if end < int64(len(full)) && full[end] == '\n' {
		end++
	}
	return edit{start: start, end: end}
}
