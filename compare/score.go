package compare

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/xaroth/pbt-conformance/pob"
)

// GroupChecks is the scoreboard row of a layer's build checks: the import, warning and
// allocated-node mismatches, which belong to no group of outputs.
const GroupChecks pob.Group = "checks"

// ErrHints marks a malformed hints file.
var ErrHints = errors.New("invalid hints")

var errNoHint = errors.New(`entry has no "hint" text`)

// Scoreboard says how many builds the engine gets right and what blocks the most.
type Scoreboard struct {
	Mismatches []Mismatch `json:"mismatches"`
	// Stale are the ids of triage entries that match no mismatch.
	Stale []string `json:"staleTriage"`
	// Groups count builds by status per league, layer and group.
	Groups []GroupRow `json:"groups"`
	// Kinds count builds passing everything per league and kind of build, then per content.
	Kinds []KindRow `json:"kinds"`
	// Missing count builds per output PoB gives and the engine does not, in a group the engine
	// gives other outputs of.
	Missing []MissingRow `json:"missing"`
	// Blockers count builds per blocker, most blocking first.
	Blockers []BlockerRow `json:"blockers"`
}

// GroupRow counts the builds PoB gives a group's outputs for in one league and layer. Fail is
// Mismatch plus Incomplete.
type GroupRow struct {
	League      string    `json:"league"`
	Layer       string    `json:"layer"`
	Group       pob.Group `json:"group"`
	Builds      int       `json:"builds"`
	Pass        int       `json:"pass"`
	Fail        int       `json:"fail"`
	Mismatch    int       `json:"mismatch"`
	Incomplete  int       `json:"incomplete"`
	NotCompared int       `json:"notCompared"`
}

// KindRow counts the builds of a kind, or holding a content, and how many pass every group of
// every layer and their checks.
type KindRow struct {
	League string `json:"league"`
	// Kind is the kind, or "contains:<content>".
	Kind    string `json:"kind"`
	Builds  int    `json:"builds"`
	PassAll int    `json:"passAll"`
}

// MissingRow counts the builds lacking one output of ours.
type MissingRow struct {
	Group  pob.Group `json:"group"`
	Output string    `json:"output"`
	Builds int       `json:"builds"`
}

// BlockerRow counts the builds a blocker appears in, in all and per league.
type BlockerRow struct {
	Blocker

	Builds  int            `json:"builds"`
	Leagues map[string]int `json:"leagues"`
	// Hint is the hints file's text for a stat, if any.
	Hint string `json:"hint,omitempty"`
}

// ReadHints reads a hints file: a JSON object from game stat id to either a short text or an
// object whose "hint" field is that text. Other fields of the object are ignored.
func ReadHints(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hints: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", path, ErrHints, err)
	}
	h := make(map[string]string, len(raw))
	for stat, v := range raw {
		text, err := hintText(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w: %w", path, stat, ErrHints, err)
		}
		h[stat] = text
	}
	return h, nil
}

// hintText reads one hints file entry, a text or an object with a text "hint" field.
func hintText(v json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(v, &text); err == nil {
		return text, nil
	}
	var obj struct {
		Hint *string `json:"hint"`
	}
	if err := json.Unmarshal(v, &obj); err != nil {
		return "", fmt.Errorf("hint: %w", err)
	}
	if obj.Hint == nil {
		return "", errNoHint
	}
	return *obj.Hint, nil
}

// Score builds the scoreboard of builds, with ms their mismatches as triaged, stale the stale
// triage entries and hints the stat hints (nil for none).
func Score(builds []Build, ms []Mismatch, stale []string, hints map[string]string) *Scoreboard {
	s := &Scoreboard{Mismatches: ms, Stale: stale}
	if s.Mismatches == nil {
		s.Mismatches = []Mismatch{}
	}
	if s.Stale == nil {
		s.Stale = []string{}
	}
	s.Groups = groupRows(builds)
	s.Kinds = kindRows(builds)
	s.Missing = missingRows(builds)
	s.Blockers = blockerRows(builds, hints)
	return s
}

// failsChecks reports whether b has a mismatch that is not one of an output.
func failsChecks(b *Build) bool {
	table := pobOutputs()
	return slices.ContainsFunc(b.Mismatches, func(m Mismatch) bool {
		_, ok := table[m.Output]
		return !ok
	})
}

// passesAll reports whether b passes every group of every layer and its checks.
func passesAll(b *Build) bool {
	for _, l := range b.Layers {
		for _, g := range l.Groups {
			if g.Status != StatusPass {
				return false
			}
		}
	}
	return !failsChecks(b)
}

func leaguesOf(builds []Build) []string {
	set := map[string]bool{}
	for _, b := range builds {
		set[b.League] = true
	}
	return slices.Sorted(maps.Keys(set))
}

func groupRows(builds []Build) []GroupRow {
	var out []GroupRow
	for _, league := range leaguesOf(builds) {
		for _, layer := range Layers() {
			rows := map[pob.Group]*GroupRow{}
			for i := range builds {
				b := &builds[i]
				if b.League != league {
					continue
				}
				for _, g := range layerOf(b, layer).Groups {
					count(rows, league, layer, g.Group, g.Status)
				}
				if layer == LayerTree {
					st := StatusPass
					if failsChecks(b) {
						st = StatusMismatch
					}
					count(rows, league, layer, GroupChecks, st)
				}
			}
			for _, g := range append(pob.Groups(), GroupChecks) {
				if r, ok := rows[g]; ok {
					out = append(out, *r)
				}
			}
		}
	}
	return out
}

func layerOf(b *Build, name string) Layer {
	for _, l := range b.Layers {
		if l.Name == name {
			return l
		}
	}
	return Layer{Name: name}
}

func count(rows map[pob.Group]*GroupRow, league, layer string, g pob.Group, st Status) {
	r, ok := rows[g]
	if !ok {
		r = &GroupRow{League: league, Layer: layer, Group: g}
		rows[g] = r
	}
	r.Builds++
	switch st {
	case StatusPass:
		r.Pass++
	case StatusMismatch:
		r.Fail++
		r.Mismatch++
	case StatusIncomplete:
		r.Fail++
		r.Incomplete++
	case StatusNotCompared:
		r.NotCompared++
	}
}

// kindKeys returns the keys of kind rows in report order: untagged and smoke builds, the kinds,
// then the contents.
func kindKeys() []string {
	keys := make([]string, 0, 2+len(Kinds())+len(Contents()))
	keys = append(keys, string(Untagged), string(Smoke))
	for _, k := range Kinds() {
		keys = append(keys, string(k))
	}
	for _, c := range Contents() {
		keys = append(keys, contentKey(c))
	}
	return keys
}

func contentKey(c Content) string { return "contains:" + string(c) }

func kindRows(builds []Build) []KindRow {
	var out []KindRow
	for _, league := range leaguesOf(builds) {
		rows := map[string]*KindRow{}
		for i := range builds {
			if b := &builds[i]; b.League == league {
				countKinds(rows, b)
			}
		}
		for _, k := range kindKeys() {
			if r, ok := rows[k]; ok {
				out = append(out, *r)
			}
		}
	}
	return out
}

// countKinds counts b in the rows of its kind and of each content it holds.
func countKinds(rows map[string]*KindRow, b *Build) {
	pass := passesAll(b)
	keys := make([]string, 0, 1+len(b.Meta.Contains))
	keys = append(keys, string(b.Meta.Kind))
	for _, c := range b.Meta.Contains {
		keys = append(keys, contentKey(c))
	}
	for _, key := range keys {
		r, ok := rows[key]
		if !ok {
			r = &KindRow{League: b.League, Kind: key}
			rows[key] = r
		}
		r.Builds++
		if pass {
			r.PassAll++
		}
	}
}

func missingRows(builds []Build) []MissingRow {
	counts := map[MissingRow]int{}
	for _, b := range builds {
		for _, l := range b.Layers {
			for _, g := range l.Groups {
				for _, o := range g.Missing {
					counts[MissingRow{Group: g.Group, Output: o}]++
				}
			}
		}
	}
	out := make([]MissingRow, 0, len(counts))
	for r, n := range counts {
		r.Builds = n
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b MissingRow) int {
		return cmp.Or(cmp.Compare(b.Builds, a.Builds), cmp.Compare(a.Group, b.Group), cmp.Compare(a.Output, b.Output))
	})
	return out
}

func blockerRows(builds []Build, hints map[string]string) []BlockerRow {
	rows := map[Blocker]*BlockerRow{}
	for _, b := range builds {
		for _, bl := range b.Blockers {
			r, ok := rows[bl]
			if !ok {
				r = &BlockerRow{Blocker: bl, Leagues: map[string]int{}}
				if bl.Kind == BlockerStat {
					r.Hint = hints[bl.Name]
				}
				rows[bl] = r
			}
			r.Builds++
			r.Leagues[b.League]++
		}
	}
	out := make([]BlockerRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b BlockerRow) int {
		return cmp.Or(cmp.Compare(b.Builds, a.Builds), compareBlockers(a.Blocker, b.Blocker))
	})
	return out
}

// WriteJSON writes s as indented JSON.
func (s *Scoreboard) WriteJSON(w io.Writer) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("scoreboard: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("scoreboard: %w", err)
	}
	return nil
}

// WriteText writes the scoreboard's tables after the mismatch list, which the caller writes:
// builds by status, by kind, the outputs the engine lacks and the blockers.
func (s *Scoreboard) WriteText(w io.Writer) error {
	var buf bytes.Buffer
	const padding = 2
	tw := tabwriter.NewWriter(&buf, 0, 0, padding, ' ', 0)
	s.writeCounts(tw)
	s.writeBlockers(tw)
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("scoreboard: %w", err)
	}
	for line := range strings.Lines(buf.String()) {
		if _, err := io.WriteString(w, strings.TrimRight(line, " \n")+"\n"); err != nil {
			return fmt.Errorf("scoreboard: %w", err)
		}
	}
	return nil
}

// writeCounts writes the tables of builds by group, by kind and by missing output.
func (s *Scoreboard) writeCounts(tw io.Writer) {
	fmt.Fprintln(tw, "\nleague\tlayer\tgroup\tbuilds\tpass\tfail\tmismatch\tincomplete\tnot compared")
	for _, r := range s.Groups {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", r.League, r.Layer, r.Group, r.Builds, r.Pass, r.Fail, r.Mismatch, r.Incomplete, r.NotCompared)
	}
	fmt.Fprintln(tw, "\nleague\tkind\tbuilds\tpass all")
	for _, r := range s.Kinds {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", r.League, r.Kind, r.Builds, r.PassAll)
	}
	fmt.Fprintln(tw, "\nmissing output\tgroup\tbuilds")
	for _, r := range s.Missing {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", r.Output, r.Group, r.Builds)
	}
}

// writeBlockers writes the blockers with their count per league.
func (s *Scoreboard) writeBlockers(tw io.Writer) {
	leagues := map[string]bool{}
	for _, r := range s.Blockers {
		for l := range r.Leagues {
			leagues[l] = true
		}
	}
	ls := slices.Sorted(maps.Keys(leagues))
	fmt.Fprint(tw, "\nbuilds")
	for _, l := range ls {
		fmt.Fprint(tw, "\t", l)
	}
	fmt.Fprintln(tw, "\tblocker\tname\thint")
	for _, r := range s.Blockers {
		fmt.Fprint(tw, r.Builds)
		for _, l := range ls {
			fmt.Fprint(tw, "\t", r.Leagues[l])
		}
		fmt.Fprintf(tw, "\t%s\t%s\t%s\n", r.Kind, r.Name, r.Hint)
	}
}
