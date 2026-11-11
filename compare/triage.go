package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
)

// TriageFile is the triage file's name at the repository root.
const TriageFile = "triage.json"

// ErrTriage marks a malformed triage file.
var ErrTriage = errors.New("invalid triage")

// Kinds of triage entries.
const (
	// KindDeviation: PoB's behaviour we do not follow, with our evidence.
	KindDeviation = "deviation"
	// KindPending: a mechanic pbt-engine does not model yet.
	KindPending = "pending"
)

// Entry accepts the mismatches it matches, for one reason. It matches either by pattern: its
// league (empty: any), builds by path.Match pattern on the build directory name, output
// (empty: any), and, when set, our value and PoB's tree-only value; or by its cases, each one
// exact mismatch. With values set, a triaged mismatch whose numbers change no longer matches
// and shows as untriaged; broad pending entries leave them out.
type Entry struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	League   string   `json:"league,omitempty"`
	Build    string   `json:"build,omitempty"`
	Output   string   `json:"output,omitempty"`
	Ours     *float64 `json:"ours,omitempty"`
	PoB      *float64 `json:"pob,omitempty"`
	Cases    []Case   `json:"cases,omitempty"`
	Reason   string   `json:"reason"`
	Evidence string   `json:"evidence,omitempty"`
}

// Case is one mismatch an entry accepts: a build by its exact name, an output, and for an
// output with values our value and PoB's tree-only value, PoB's left out when PoB gives none.
// Import and warning outputs have no values. A case whose values change no longer matches, and
// a case that matches nothing is stale.
type Case struct {
	Build  string   `json:"build"`
	Output string   `json:"output"`
	Ours   *float64 `json:"ours,omitempty"`
	PoB    *float64 `json:"pob,omitempty"`
}

func (c *Case) hasValues() bool {
	return !strings.HasPrefix(c.Output, importPrefix) && !strings.HasPrefix(c.Output, warningPrefix)
}

func (c *Case) matches(m *Mismatch) bool {
	if c.Build != m.Build || c.Output != m.Output {
		return false
	}
	if !c.hasValues() {
		return true
	}
	if c.PoB == nil {
		return !m.HasPoB && sameValue(c.Ours, m.Ours, true)
	}
	return sameValue(c.Ours, m.Ours, true) && sameValue(c.PoB, m.PoB, m.HasPoB)
}

// ReadTriage reads and checks the triage file in fsys.
func ReadTriage(fsys fs.FS) ([]Entry, error) {
	b, err := fs.ReadFile(fsys, TriageFile)
	if err != nil {
		return nil, fmt.Errorf("read triage: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var es []Entry
	if err := dec.Decode(&es); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", TriageFile, ErrTriage, err)
	}
	seen := map[string]bool{}
	for _, e := range es {
		switch {
		case e.ID == "" || seen[e.ID]:
			return nil, fmt.Errorf("%s: %w: id %q missing or repeated", TriageFile, ErrTriage, e.ID)
		case e.Kind != KindDeviation && e.Kind != KindPending:
			return nil, fmt.Errorf("%s: %w: %s: kind %q", TriageFile, ErrTriage, e.ID, e.Kind)
		case e.Reason == "":
			return nil, fmt.Errorf("%s: %w: %s: no reason", TriageFile, ErrTriage, e.ID)
		}
		if err := checkMatch(&e); err != nil {
			return nil, fmt.Errorf("%s: %w: %s: %w", TriageFile, ErrTriage, e.ID, err)
		}
		seen[e.ID] = true
	}
	return es, nil
}

var (
	errBothMatches = errors.New("a build pattern, output or values beside cases")
	errBadCase     = errors.New("case without build or output, repeated, or without values it needs")
)

// checkMatch checks that e matches either by pattern or by cases, and that each case names one
// mismatch: with our value for an output with values, and without values otherwise.
func checkMatch(e *Entry) error {
	if len(e.Cases) == 0 {
		if _, err := path.Match(e.Build, ""); err != nil {
			return fmt.Errorf("build pattern: %w", err)
		}
		return nil
	}
	if e.Build != "" || e.Output != "" || e.Ours != nil || e.PoB != nil {
		return errBothMatches
	}
	seen := map[Case]bool{}
	for _, c := range e.Cases {
		key := Case{Build: c.Build, Output: c.Output}
		if !c.valid() || seen[key] {
			return fmt.Errorf("%w: %s %s", errBadCase, c.Build, c.Output)
		}
		seen[key] = true
	}
	return nil
}

// valid reports whether c names a build and output, with our value exactly when the output
// has values.
func (c *Case) valid() bool {
	if c.Build == "" || c.Output == "" {
		return false
	}
	if c.hasValues() {
		return c.Ours != nil
	}
	return c.Ours == nil && c.PoB == nil
}

func (e *Entry) matches(m *Mismatch) bool {
	if len(e.Cases) > 0 {
		return slices.ContainsFunc(e.Cases, func(c Case) bool { return (e.League == "" || e.League == m.League) && c.matches(m) })
	}
	ok, _ := path.Match(e.Build, m.Build)
	return ok && (e.League == "" || e.League == m.League) && (e.Output == "" || e.Output == m.Output) &&
		sameValue(e.Ours, m.Ours, true) && sameValue(e.PoB, m.PoB, m.HasPoB)
}

// sameValue reports whether an entry's value want, when set, is got, which has holds.
func sameValue(want *float64, got float64, has bool) bool {
	return want == nil || (has && math.Abs(*want-got) <= tolerance)
}

// Apply sets each mismatch's Triage to the first entry matching it and returns what matches
// nothing, which is stale: the ids of entries, and "<id>: <build> <output>" for each case.
func Apply(ms []Mismatch, es []Entry) []string {
	used := map[string]bool{}
	for i := range ms {
		for j := range es {
			if es[j].matches(&ms[i]) {
				ms[i].Triage = es[j].ID
				used[es[j].ID] = true
				for _, c := range es[j].Cases {
					if c.matches(&ms[i]) {
						used[caseKey(&es[j], &c)] = true
					}
				}
				break
			}
		}
	}
	return staleOf(es, used)
}

// staleOf returns the entries without cases and the cases that used does not hold.
func staleOf(es []Entry, used map[string]bool) []string {
	var stale []string
	for _, e := range es {
		if len(e.Cases) == 0 {
			if !used[e.ID] {
				stale = append(stale, e.ID)
			}
			continue
		}
		for _, c := range e.Cases {
			if k := caseKey(&e, &c); !used[k] {
				stale = append(stale, k)
			}
		}
	}
	return stale
}

func caseKey(e *Entry, c *Case) string { return e.ID + ": " + c.Build + " " + c.Output }
