package ninja

import (
	"slices"

	"github.com/xaroth/pbt-conformance/compare"
)

// Build is a counted build: one already in the league, or a candidate from poe.ninja.
type Build struct {
	Kind compare.Kind
	// Skill is the main skill PoB took, Ascendancy the build's ascendancy or class.
	Skill, Ascendancy string
	Contains          []compare.Content
}

func (b Build) key() [2]string { return [2]string{b.Skill, b.Ascendancy} }

// Candidate is a poe.ninja character that could become a test build.
type Candidate struct {
	Build

	Char     Character
	Snapshot Snapshot
	// XML is the character's PoB build.
	XML []byte
	// Err is why the candidate cannot be used, such as PoB failing to load it.
	Err error
}

// Plan is the candidates picked and what the targets still lack.
type Plan struct {
	Picked []Candidate
	// Kinds and Contents count the counted builds, existing and picked, per kind and content.
	Kinds    map[compare.Kind]int
	Contents map[compare.Content]int
}

// Choose picks candidates, in their order of popularity, until each kind has its target count
// with the existing builds: one build per main skill and ascendancy, none failing to load. It
// then swaps picked builds for later candidates of the same kind until each content has its
// quota, where it can. The result is in kind order, then in candidate order.
func Choose(existing []Build, cands []Candidate, targets map[compare.Kind]int, quotas map[compare.Content]int) Plan {
	p := planner{existing: existing, cands: cands, picked: make([]bool, len(cands)), used: map[[2]string]bool{}}
	for _, b := range existing {
		p.used[b.key()] = true
	}
	for i, c := range cands {
		if p.usable(i) && p.kinds()[c.Kind] < targets[c.Kind] {
			p.pick(i)
		}
	}
	for _, q := range compare.Contents() {
		for p.contents()[q] < quotas[q] {
			if !p.swapIn(q, quotas) {
				break
			}
		}
	}
	return Plan{Picked: p.inKindOrder(), Kinds: p.kinds(), Contents: p.contents()}
}

type planner struct {
	existing []Build
	cands    []Candidate
	picked   []bool
	used     map[[2]string]bool
}

// inKindOrder returns the picked candidates in kind order, then in candidate order.
func (p *planner) inKindOrder() []Candidate {
	var out []Candidate
	for _, k := range compare.Kinds() {
		for i, c := range p.cands {
			if p.picked[i] && c.Kind == k {
				out = append(out, c)
			}
		}
	}
	return out
}

func (p *planner) usable(i int) bool {
	return p.candidate(i) && !p.used[p.cands[i].key()]
}

func (p *planner) pick(i int) {
	p.picked[i] = true
	p.used[p.cands[i].key()] = true
}

func (p *planner) drop(i int) {
	p.picked[i] = false
	delete(p.used, p.cands[i].key())
}

func (p *planner) counted() []Build {
	bs := slices.Clone(p.existing)
	for i, c := range p.cands {
		if p.picked[i] {
			bs = append(bs, c.Build)
		}
	}
	return bs
}

func (p *planner) kinds() map[compare.Kind]int {
	n := map[compare.Kind]int{}
	for _, b := range p.counted() {
		n[b.Kind]++
	}
	return n
}

func (p *planner) contents() map[compare.Content]int {
	n := map[compare.Content]int{}
	for _, b := range p.counted() {
		for _, c := range b.Contains {
			n[c]++
		}
	}
	return n
}

// swapIn picks the first unpicked candidate with content q in place of the last picked
// candidate of its kind that lacks q and holds no content at or under its quota; it reports
// whether it found such a pair.
func (p *planner) swapIn(q compare.Content, quotas map[compare.Content]int) bool {
	have := p.contents()
	spare := func(d Candidate) bool {
		return !slices.Contains(d.Contains, q) &&
			!slices.ContainsFunc(d.Contains, func(x compare.Content) bool { return quotas[x] > 0 && have[x] <= quotas[x] })
	}
	for i, c := range p.cands {
		if slices.Contains(c.Contains, q) && p.candidate(i) && p.replace(i, spare) {
			return true
		}
	}
	return false
}

// replace picks candidate i in place of the last picked candidate of its kind that spare
// allows and whose removal frees i's pair, if any, and reports whether it did.
func (p *planner) replace(i int, spare func(Candidate) bool) bool {
	for j, d := range slices.Backward(p.cands) {
		if !p.picked[j] || d.Kind != p.cands[i].Kind || !spare(d) {
			continue
		}
		p.drop(j)
		if p.usable(i) {
			p.pick(i)
			return true
		}
		p.pick(j)
	}
	return false
}

// candidate reports whether candidate i could be picked if its pair were free.
func (p *planner) candidate(i int) bool {
	c := p.cands[i]
	return c.Err == nil && !p.picked[i] && c.Kind != compare.KindParty && c.Kind != compare.Smoke
}
