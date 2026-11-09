package compare

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/config"
	"github.com/xaroth/pbt-engine/dataset"
	"github.com/xaroth/pbt-engine/rules/poe1"
)

// ErrPoBXML marks a build XML that cannot be read into a pbt-engine build.
var ErrPoBXML = errors.New("unusable pob build xml")

type pobFile struct {
	Build struct {
		Level      int    `xml:"level,attr"`
		Class      string `xml:"className,attr"`
		Ascendancy string `xml:"ascendClassName,attr"`
		Bandit     string `xml:"bandit,attr"`
	} `xml:"Build"`
	Tree struct {
		Active int `xml:"activeSpec,attr"`
		Specs  []struct {
			TreeVersion    string `xml:"treeVersion,attr"`
			Nodes          string `xml:"nodes,attr"`
			MasteryEffects string `xml:"masteryEffects,attr"`
		} `xml:"Spec"`
	} `xml:"Tree"`
	Config struct {
		Active string     `xml:"activeConfigSet,attr"`
		Inputs []pobInput `xml:"Input"`
		Sets   []struct {
			ID     string     `xml:"id,attr"`
			Inputs []pobInput `xml:"Input"`
		} `xml:"ConfigSet"`
	} `xml:"Config"`
}

type pobInput struct {
	Name   string `xml:"name,attr"`
	Number string `xml:"number,attr"`
}

// FromPoB reads a PoB build XML as a pbt-engine build for version of game, with what the
// build format has: class, ascendancy, level, the active tree's nodes and mastery choices, and
// as config the bandits and the campaign stage from PoB's resistance penalty. Class and
// ascendancy names map to ids through the dataset's English text. It fails when the active
// tree is not for tree, the league's PoB tree version. Items and skills are left out: the
// build format has none yet.
func FromPoB(data []byte, game, version, tree string, s *dataset.Store) (*build.Build, error) {
	var f pobFile
	if err := xml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPoBXML, err)
	}
	i := max(f.Tree.Active, 1) - 1
	if i >= len(f.Tree.Specs) {
		return nil, fmt.Errorf("%w: no active tree", ErrPoBXML)
	}
	spec := f.Tree.Specs[i]
	if spec.TreeVersion != tree {
		return nil, fmt.Errorf("%w: tree %q, want %q", ErrPoBXML, spec.TreeVersion, tree)
	}
	b := &build.Build{Format: 1, Game: game, Version: version, Variant: "standard", Level: f.Build.Level}
	var err error
	if b.Class, b.Ascendancy, err = ids(s, f.Build.Class, f.Build.Ascendancy); err != nil {
		return nil, err
	}
	if b.Nodes, err = nodes(spec.Nodes); err != nil {
		return nil, err
	}
	if b.Masteries, err = masteries(spec.MasteryEffects); err != nil {
		return nil, err
	}
	if b.Config, err = f.config(); err != nil {
		return nil, err
	}
	return b, nil
}

// config returns the bandit choice and campaign stage as poe1 config values.
func (f *pobFile) config() (config.Values, error) {
	bandit, err := bandits(f.Build.Bandit)
	if err != nil {
		return nil, err
	}
	stage, err := campaign(f.inputs())
	if err != nil {
		return nil, err
	}
	var v config.Values
	if err := config.Set(&v, poe1.Bandits, bandit); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPoBXML, err)
	}
	if err := config.Set(&v, poe1.Campaign, stage); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPoBXML, err)
	}
	return v, nil
}

// inputs are the config inputs PoB applies: the active config set's, or the legacy flat list.
func (f *pobFile) inputs() []pobInput {
	for _, set := range f.Config.Sets {
		if set.ID == f.Config.Active {
			return set.Inputs
		}
	}
	return f.Config.Inputs
}

func ids(s *dataset.Store, class, ascendancy string) (string, string, error) {
	text, err := s.Text("en")
	if err != nil {
		return "", "", fmt.Errorf("compare: %w", err)
	}
	i := slices.IndexFunc(text.CharacterNames, func(n dataset.Name) bool { return n.Name == class })
	if i < 0 {
		return "", "", fmt.Errorf("%w: class %q", ErrPoBXML, class)
	}
	if ascendancy == "" || ascendancy == "None" {
		return text.CharacterNames[i].ID, "", nil
	}
	j := slices.IndexFunc(text.AscendancyNames, func(n dataset.AscendancyName) bool {
		return n.Variant == "standard" && n.Name == ascendancy
	})
	if j < 0 {
		return "", "", fmt.Errorf("%w: ascendancy %q", ErrPoBXML, ascendancy)
	}
	return text.CharacterNames[i].ID, text.AscendancyNames[j].ID, nil
}

func nodes(list string) ([]uint32, error) {
	var out []uint32
	for f := range strings.SplitSeq(list, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n, err := strconv.ParseUint(f, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%w: node %q", ErrPoBXML, f)
		}
		out = append(out, uint32(n))
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// masteries reads PoB's masteryEffects, "{node,effect},{node,effect}".
func masteries(list string) (map[uint32]uint32, error) {
	out := map[uint32]uint32{}
	for pair := range strings.SplitSeq(list, "},") {
		pair = strings.Trim(strings.TrimSpace(pair), "{}")
		if pair == "" {
			continue
		}
		n, e, ok := strings.Cut(pair, ",")
		node, err1 := strconv.ParseUint(strings.TrimSpace(n), 10, 32)
		effect, err2 := strconv.ParseUint(strings.TrimSpace(e), 10, 32)
		if !ok || err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%w: mastery effect %q", ErrPoBXML, pair)
		}
		out[uint32(node)] = uint32(effect)
	}
	return out, nil
}

// bandits maps PoB's bandit choice; "None" is PoB's kill-all.
func bandits(pob string) (poe1.BanditChoice, error) {
	switch pob {
	case "", "None":
		return poe1.BanditsKillAll, nil
	case "Alira":
		return poe1.BanditsHelpAlira, nil
	case "Kraityn":
		return poe1.BanditsHelpKraityn, nil
	case "Oak":
		return poe1.BanditsHelpOak, nil
	}
	return "", fmt.Errorf("%w: bandit %q", ErrPoBXML, pob)
}

// Resistance penalties of PoB's resistancePenalty config; -60, after both Kitava quests, is
// PoB's default.
const (
	penaltyComplete  = -60
	penaltyActs6To10 = -30
)

func campaign(inputs []pobInput) (poe1.CampaignStage, error) {
	for _, in := range inputs {
		if in.Name != "resistancePenalty" {
			continue
		}
		switch in.Number {
		case strconv.Itoa(penaltyComplete):
			return poe1.CampaignComplete, nil
		case strconv.Itoa(penaltyActs6To10):
			return poe1.CampaignActs6To10, nil
		case "0":
			return poe1.CampaignActs1To5, nil
		}
		return "", fmt.Errorf("%w: resistance penalty %q", ErrPoBXML, in.Number)
	}
	return poe1.CampaignComplete, nil
}
