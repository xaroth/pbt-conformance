package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
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

// Entry accepts the mismatches it matches: its league (empty: any), builds by path.Match
// pattern on the build directory name, output (empty: any), and, when set, our value and
// PoB's tree-only value. With values set, a triaged mismatch whose numbers change no longer
// matches and shows as untriaged; broad pending entries leave them out.
type Entry struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	League   string   `json:"league,omitempty"`
	Build    string   `json:"build"`
	Output   string   `json:"output,omitempty"`
	Ours     *float64 `json:"ours,omitempty"`
	PoB      *float64 `json:"pob,omitempty"`
	Reason   string   `json:"reason"`
	Evidence string   `json:"evidence,omitempty"`
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
		if _, err := path.Match(e.Build, ""); err != nil {
			return nil, fmt.Errorf("%s: %w: %s: build pattern: %w", TriageFile, ErrTriage, e.ID, err)
		}
		seen[e.ID] = true
	}
	return es, nil
}

func (e *Entry) matches(m *Mismatch) bool {
	ok, _ := path.Match(e.Build, m.Build)
	return ok && (e.League == "" || e.League == m.League) && (e.Output == "" || e.Output == m.Output) &&
		sameValue(e.Ours, m.Ours, true) && sameValue(e.PoB, m.PoB, m.HasPoB)
}

// sameValue reports whether an entry's value want, when set, is got, which has holds.
func sameValue(want *float64, got float64, has bool) bool {
	return want == nil || (has && math.Abs(*want-got) <= tolerance)
}

// Apply sets each mismatch's Triage to the first entry matching it and returns the ids of
// entries that match nothing, which are stale.
func Apply(ms []Mismatch, es []Entry) []string {
	used := map[string]bool{}
	for i := range ms {
		for j := range es {
			if es[j].matches(&ms[i]) {
				ms[i].Triage = es[j].ID
				used[es[j].ID] = true
				break
			}
		}
	}
	var stale []string
	for _, e := range es {
		if !used[e.ID] {
			stale = append(stale, e.ID)
		}
	}
	return stale
}
