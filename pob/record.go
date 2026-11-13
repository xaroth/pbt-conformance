package pob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/runtime"
)

// Build file names in a build directory, builds/<league>/<name>/.
const (
	// FullXML is the build as complete as PoB holds it: tree, items, skills.
	FullXML = "pob.xml"
	// SkillXML is the gear with the main skill alone against the enemy, derived from FullXML by
	// Skill.
	SkillXML = "pob-skill.xml"
	// GearXML is the tree and the gear, derived from FullXML by Gear.
	GearXML = "pob-gear.xml"
	// TreeOnlyXML is the same build with only the tree, to tell tree and gear causes apart,
	// derived from FullXML by TreeOnly.
	TreeOnlyXML = "pob-treeonly.xml"
	// ValuesFile holds what PoB computed for the four XMLs.
	ValuesFile = "pob.json"
	// PartyFile names the build's ally in its league: the build whose party buffs it is
	// recorded with too.
	PartyFile = "party.json"
)

// ErrStale marks a stored values file that differs from a fresh PoB run.
var ErrStale = errors.New("stored pob files differ from a fresh run")

const filePerm = 0o644

// Values is a build's values file: PoB's results for the four XMLs and the release that made
// them. The skill, minion and delivery outputs are recorded for the full and skill layers only:
// the others have no skills.
type Values struct {
	PoB      pins.Release `json:"pob"`
	Full     Result       `json:"full"`
	Skill    Result       `json:"skill"`
	Gear     Result       `json:"gear"`
	TreeOnly Result       `json:"treeOnly"`
	// Party is the full build with the party buffs of the ally its party file names, recorded
	// as the full layer is.
	Party *Result `json:"party,omitempty"`
}

// Party is a build's party file.
type Party struct {
	// Ally is the directory name of the ally's build, in the same league.
	Ally string `json:"ally"`
}

// ErrParty marks a malformed party file or one naming no build of its league.
var ErrParty = errors.New("invalid party file")

// BuildDirs returns the build directories of league under root/builds/<league>, sorted.
func BuildDirs(root, league string) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "builds", league, "*", FullXML))
	if err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	for i, d := range dirs {
		dirs[i] = filepath.Dir(d)
	}
	slices.Sort(dirs)
	return dirs, nil
}

// Record derives each build's XMLs, runs every build of league under root through its
// pinned PoB in cache and writes each build's values file. With check it writes nothing and
// fails with ErrStale when a stored derived XML or values file differs from a fresh run, naming
// the builds.
func Record(ctx context.Context, root, cache string, f *pins.File, game, league string, check bool) error {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return fmt.Errorf("pob: %s %s: %w", game, league, errNotPinned)
	}
	dirs, err := BuildDirs(root, league)
	if err != nil || len(dirs) == 0 {
		return err
	}
	if err := deriveAll(dirs, check); err != nil {
		return err
	}
	jobs, allies, err := jobsOf(dirs)
	if err != nil {
		return err
	}
	res, err := RunJobs(ctx, runtime.Locate(cache, f.Runtime, l.PoB), jobs)
	if err != nil {
		return err
	}
	return storeAll(dirs, allies, l.PoB, res, check)
}

// jobsOf returns the jobs of the build directories: their XMLs, in order, then each full XML
// with its ally's, in order of the builds with an ally. allies holds whether each build has one.
func jobsOf(dirs []string) (jobs []Job, allies []bool, err error) {
	files, err := xmlFiles(dirs)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		jobs = append(jobs, Job{File: f})
	}
	allies = make([]bool, len(dirs))
	for i, d := range dirs {
		ally, err := allyOf(d)
		if err != nil {
			return nil, nil, err
		}
		if ally == "" {
			continue
		}
		full, err := filepath.Abs(filepath.Join(d, FullXML))
		if err != nil {
			return nil, nil, fmt.Errorf("pob: %w", err)
		}
		jobs = append(jobs, Job{File: full, Ally: ally})
		allies[i] = true
	}
	return jobs, allies, nil
}

// allyOf returns the absolute path of the full XML of the ally that dir's party file names,
// empty without a party file.
func allyOf(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, PartyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("pob: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var p Party
	if err := dec.Decode(&p); err != nil {
		return "", fmt.Errorf("%s: %w: %w", dir, ErrParty, err)
	}
	if p.Ally == "" || p.Ally != filepath.Base(p.Ally) || p.Ally == filepath.Base(dir) {
		return "", fmt.Errorf("%s: %w: ally %q", dir, ErrParty, p.Ally)
	}
	ally, err := filepath.Abs(filepath.Join(filepath.Dir(dir), p.Ally, FullXML))
	if err != nil {
		return "", fmt.Errorf("pob: %w", err)
	}
	if _, err := os.Stat(ally); err != nil {
		return "", fmt.Errorf("%s: %w: %w", dir, ErrParty, err)
	}
	return ally, nil
}

// Derived returns the XMLs derived from a build's full XML, by file name.
func Derived() map[string]func([]byte) ([]byte, error) {
	return map[string]func([]byte) ([]byte, error){GearXML: Gear, SkillXML: Skill, TreeOnlyXML: TreeOnly}
}

// deriveAll writes each build's derived XMLs, or with check fails with ErrStale when one
// differs.
func deriveAll(dirs []string, check bool) error {
	var stale []string
	for _, d := range dirs {
		full, err := os.ReadFile(filepath.Join(d, FullXML))
		if err != nil {
			return fmt.Errorf("pob: %w", err)
		}
		for _, name := range slices.Sorted(maps.Keys(Derived())) {
			b, err := Derived()[name](full)
			if err != nil {
				return fmt.Errorf("%s: %w", d, err)
			}
			same, err := update(filepath.Join(d, name), b, check)
			if err != nil {
				return err
			}
			if !same {
				stale = append(stale, filepath.Join(d, name))
			}
		}
	}
	if check && len(stale) > 0 {
		return fmt.Errorf("%w: %v", ErrStale, stale)
	}
	return nil
}

// storeAll stores each build's results, as they lie in res in the order of jobsOf.
func storeAll(dirs []string, allies []bool, r pins.Release, res []Result, check bool) error {
	var stale []string
	party := res[len(xmlNames())*len(dirs):]
	for i, d := range dirs {
		v := Values{
			PoB: r, Full: res[0].restrict(true), Skill: res[1].restrict(true),
			Gear: res[2].restrict(false), TreeOnly: res[3].restrict(false),
		}
		res = res[len(xmlNames()):]
		if allies[i] {
			p := party[0].restrict(true)
			v.Party = &p
			party = party[1:]
		}
		same, err := store(d, v, check)
		if err != nil {
			return err
		}
		if !same {
			stale = append(stale, dirs[i])
		}
	}
	if check && len(stale) > 0 {
		return fmt.Errorf("%w: %v", ErrStale, stale)
	}
	return nil
}

// xmlNames returns the XMLs run per build, in the order storeAll reads their results.
func xmlNames() []string {
	return []string{FullXML, SkillXML, GearXML, TreeOnlyXML}
}

// xmlFiles returns the XMLs of each build directory, absolute, in order.
func xmlFiles(dirs []string) ([]string, error) {
	names := xmlNames()
	files := make([]string, 0, len(names)*len(dirs))
	for _, d := range dirs {
		for _, name := range names {
			p, err := filepath.Abs(filepath.Join(d, name))
			if err != nil {
				return nil, fmt.Errorf("pob: %w", err)
			}
			files = append(files, p)
		}
	}
	return files, nil
}

// store writes v as dir's values file, or with check only compares; it reports whether the
// stored file already held v.
func store(dir string, v Values, check bool) (bool, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return false, fmt.Errorf("pob: %w", err)
	}
	return update(filepath.Join(dir, ValuesFile), append(b, '\n'), check)
}

// update writes b to path, or with check only compares; it reports whether path already held b.
func update(path string, b []byte, check bool) (bool, error) {
	old, err := os.ReadFile(path)
	same := err == nil && bytes.Equal(old, b)
	if check || same {
		return same, nil
	}
	if err := os.WriteFile(path, b, filePerm); err != nil {
		return false, fmt.Errorf("pob: %w", err)
	}
	return same, nil
}

var errNotPinned = errors.New("league not pinned")
