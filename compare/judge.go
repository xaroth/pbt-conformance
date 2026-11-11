package compare

import (
	"github.com/xaroth/pbt-conformance/pob"
)

// Layers of a build, in report order.
const (
	LayerTree = "tree"
	LayerGear = "gear"
	LayerFull = "full"
	// LayerParty is the full build with its ally's party buffs; only builds with an ally have it.
	LayerParty = "party"
)

// Layers returns the layers in report order.
func Layers() []string { return []string{LayerTree, LayerGear, LayerFull, LayerParty} }

// Status is how a build fares on one group of outputs in one layer.
type Status string

// Statuses of a group.
const (
	// StatusPass: the engine gives every output of the group PoB gives, each matching.
	StatusPass Status = "pass"
	// StatusMismatch: an output of the group differs from PoB's.
	StatusMismatch Status = "mismatch"
	// StatusIncomplete: the outputs the engine gives match, but it lacks some PoB gives.
	StatusIncomplete Status = "incomplete"
	// StatusNotCompared: the engine gives no output of the group for the layer yet.
	StatusNotCompared Status = "not-compared"
)

// Layer judges the groups of one layer of a build.
type Layer struct {
	Name string `json:"name"`
	// Groups are the groups recorded for the layer that PoB gives outputs of, in the order of
	// pob.Groups.
	Groups []GroupResult `json:"groups"`
}

// GroupResult is the judgement of one group.
type GroupResult struct {
	Group  pob.Group `json:"group"`
	Status Status    `json:"status"`
	// Missing are the outputs PoB gives that the engine does not, when it gives some.
	Missing []string `json:"missing,omitempty"`
}

// judgeLayers judges each layer of PoB's values v against our values for it, the party layer
// only when v has one. The engine simulates the tree-only build only, so ours are the tree
// layer's.
func judgeLayers(v *pob.Values, ours map[string]float64) []Layer {
	ls := []Layer{
		judgeLayer(LayerTree, v.TreeOnly, ours),
		judgeLayer(LayerGear, v.Gear, nil),
		judgeLayer(LayerFull, v.Full, nil),
	}
	if v.Party != nil {
		ls = append(ls, judgeLayer(LayerParty, *v.Party, nil))
	}
	return ls
}

func judgeLayer(name string, theirs pob.Result, ours map[string]float64) Layer {
	l := Layer{Name: name}
	for _, g := range theirs.Groups() {
		if r, ok := judgeGroup(g, theirs.Outputs, ours); ok {
			l.Groups = append(l.Groups, r)
		}
	}
	return l
}

// judgeGroup judges group g, and returns false when PoB gives no output of it.
func judgeGroup(g pob.Group, theirs, ours map[string]float64) (GroupResult, bool) {
	given, differs := 0, false
	var missing []string
	for _, o := range pob.Outputs() {
		if o.Group != g {
			continue
		}
		p, ok := o.Value(theirs)
		if !ok {
			continue
		}
		given++
		v, ok := ours[o.Name]
		if !ok {
			missing = append(missing, o.Name)
			continue
		}
		if !o.Matches(v, p) {
			differs = true
		}
	}
	if given == 0 {
		return GroupResult{}, false
	}
	return verdictOf(g, given, missing, differs), true
}

// verdictOf judges a group of which PoB gives given outputs, the engine lacks missing and
// differs on some when differs.
func verdictOf(g pob.Group, given int, missing []string, differs bool) GroupResult {
	switch {
	case len(missing) == given:
		return GroupResult{Group: g, Status: StatusNotCompared}
	case differs:
		return GroupResult{Group: g, Status: StatusMismatch, Missing: missing}
	case len(missing) > 0:
		return GroupResult{Group: g, Status: StatusIncomplete, Missing: missing}
	}
	return GroupResult{Group: g, Status: StatusPass}
}
