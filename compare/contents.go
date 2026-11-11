package compare

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// itemSkillLine matches an item line that grants or triggers a skill, as PoB reads them: "Grants
// Level 20 Summon Stone Golem", "Trigger Level 20 Bone Nova when ...", "Curse Enemies with Level
// 10 Despair on Hit", "Grants Summon Harbinger Skill".
var itemSkillLine = regexp.MustCompile(`(?i)^(?:\d+% chance to )?(?:grants?|triggers?|casts?|attack with|uses?|curse enemies with) level \d+ |` +
	` can trigger level \d+ |^\d+% chance to trigger .+ when |^grants [^0-9]+ skill$`)

// conqueredLine marks a timeless jewel, the Legion ones and the abyss ones on an Assembled Eye
// Jewel base alike.
var conqueredLine = regexp.MustCompile(`(?i)^passives (?:in radius|affected) are conquered by `)

// lineTags matches the tags PoB writes before an item line, such as {crafted} or {range:0.5}.
var lineTags = regexp.MustCompile(`^(?:\{[^}]*\})+`)

type pobFile struct {
	Tree struct {
		Active int       `xml:"activeSpec,attr"`
		Specs  []pobSpec `xml:"Spec"`
	} `xml:"Tree"`
	Items struct {
		ActiveSet int       `xml:"activeItemSet,attr"`
		SecondSet string    `xml:"useSecondWeaponSet,attr"`
		Items     []pobItem `xml:"Item"`
		Sets      []pobSet  `xml:"ItemSet"`
		// Slots holds an older file's slots, which have no item set.
		Slots []pobSlot `xml:"Slot"`
	} `xml:"Items"`
}

type pobSet struct {
	ID int `xml:"id,attr"`
	// SecondSet is "true" when the set uses its second weapons; PoB writes "nil" for unset.
	SecondSet string    `xml:"useSecondWeaponSet,attr"`
	Slots     []pobSlot `xml:"Slot"`
}

type pobSpec struct {
	TreeVersion string `xml:"treeVersion,attr"`
	Sockets     []struct {
		ItemID int `xml:"itemId,attr"`
	} `xml:"Sockets>Socket"`
}

type pobItem struct {
	ID   int    `xml:"id,attr"`
	Text string `xml:",chardata"`
}

type pobSlot struct {
	Name   string `xml:"name,attr"`
	ItemID int    `xml:"itemId,attr"`
}

// DetectContents returns what a full PoB file holds of the contents that can be read from it,
// in Contents order: cluster and timeless jewels socketed in the active tree, a Ruthless or
// alternate active tree, and an equipped item that grants a skill or a graft. A mercenary is
// not in PoB's file and never detected.
func DetectContents(full []byte) ([]Content, error) {
	var f pobFile
	if err := xml.Unmarshal(full, &f); err != nil {
		return nil, fmt.Errorf("detect contents: %w", err)
	}
	spec, ok := activeSpec(&f)
	if !ok {
		return nil, fmt.Errorf("detect contents: %w: no active spec", ErrMeta)
	}
	byID := map[int]pobItem{}
	for _, it := range f.Items.Items {
		byID[it.ID] = it
	}
	var jewels []pobItem
	for _, s := range spec.Sockets {
		if it, ok := byID[s.ItemID]; ok {
			jewels = append(jewels, it)
		}
	}
	found := map[Content]bool{
		ClusterJewel:  slices.ContainsFunc(jewels, func(it pobItem) bool { return hasBase(it, "Cluster Jewel") }),
		TimelessJewel: slices.ContainsFunc(jewels, isTimeless),
		TreeVariant:   strings.Contains(spec.TreeVersion, "ruthless") || strings.Contains(spec.TreeVersion, "alternate"),
		ItemSkill:     grantsSkill(activeSlots(&f), byID, jewels),
	}
	var out []Content
	for _, c := range Contents() {
		if found[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

func activeSpec(f *pobFile) (pobSpec, bool) {
	i := f.Tree.Active - 1
	if i < 0 || i >= len(f.Tree.Specs) {
		return pobSpec{}, false
	}
	return f.Tree.Specs[i], true
}

// activeSlots returns the slots of the active item set that PoB uses: the swap weapons only
// when the set uses its second weapon set, and the main weapons only when it does not.
func activeSlots(f *pobFile) []pobSlot {
	slots, second := f.Items.Slots, f.Items.SecondSet
	for _, s := range f.Items.Sets {
		if s.ID == f.Items.ActiveSet {
			slots, second = s.Slots, s.SecondSet
		}
	}
	return slices.DeleteFunc(slices.Clone(slots), func(s pobSlot) bool {
		return strings.HasPrefix(s.Name, "Weapon ") && strings.Contains(s.Name, " Swap") != (second == "true")
	})
}

func isTimeless(it pobItem) bool {
	return hasBase(it, "Timeless Jewel") || slices.ContainsFunc(itemLines(it), conqueredLine.MatchString)
}

// hasBase reports whether the item's name or base, the lines after its rarity, holds base; a
// magic item's name holds its base between its affixes.
func hasBase(it pobItem, base string) bool {
	// The rarity line, then the name, then for a rare or unique the base.
	const nameLines = 3
	lines := itemLines(it)
	if len(lines) == 0 {
		return false
	}
	for _, l := range lines[1:min(nameLines, len(lines))] {
		if strings.Contains(l, base) {
			return true
		}
	}
	return false
}

// grantsSkill reports whether a graft is equipped, or an equipped item or socketed jewel other
// than a flask has a line that grants or triggers a skill.
func grantsSkill(slots []pobSlot, byID map[int]pobItem, jewels []pobItem) bool {
	items := slices.Clone(jewels)
	for _, s := range slots {
		it, ok := byID[s.ItemID]
		if !ok || strings.HasPrefix(s.Name, "Flask ") {
			continue
		}
		if strings.HasPrefix(s.Name, "Graft ") {
			return true
		}
		items = append(items, it)
	}
	return slices.ContainsFunc(items, func(it pobItem) bool {
		return slices.ContainsFunc(itemLines(it), itemSkillLine.MatchString)
	})
}

func itemLines(it pobItem) []string {
	var out []string
	for l := range strings.Lines(it.Text) {
		if l = lineTags.ReplaceAllString(strings.TrimSpace(l), ""); l != "" {
			out = append(out, l)
		}
	}
	return out
}
