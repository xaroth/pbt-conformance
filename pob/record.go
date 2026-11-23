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
	goruntime "runtime"
	"slices"
	"sync"

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

// maxProcs caps the PoB processes Record runs at once; each holds a loaded PoB.
const maxProcs = 8

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

// Sets of builds under the repository root, each a directory holding one per league.
const (
	// Builds are the builds whose counts the scoreboard reports.
	Builds = "builds"
	// Cases are builds made for a test, reported apart from Builds.
	Cases = "cases"
)

// BuildDirs returns the build directories of league under root/builds/<league>, sorted.
func BuildDirs(root, league string) ([]string, error) { return setDirs(root, Builds, league) }

// CaseDirs returns the case directories of league under root/cases/<league>, sorted.
func CaseDirs(root, league string) ([]string, error) { return setDirs(root, Cases, league) }

// setDirs returns the build directories of league under root/<set>/<league>, sorted.
func setDirs(root, set, league string) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(root, set, league, "*", FullXML))
	if err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	for i, d := range dirs {
		dirs[i] = filepath.Dir(d)
	}
	slices.Sort(dirs)
	return dirs, nil
}

// Record derives each build's XMLs, runs every build and case of league under root through its
// pinned PoB in cache and writes each build's values file. With check it writes nothing and
// fails with ErrStale when a stored derived XML or values file differs from a fresh run, naming
// the builds.
func Record(ctx context.Context, root, cache string, f *pins.File, game, league string, check bool) error {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return fmt.Errorf("pob: %s %s: %w", game, league, errNotPinned)
	}
	dirs, err := BuildDirs(root, league)
	if err != nil {
		return err
	}
	cases, err := CaseDirs(root, league)
	if err != nil {
		return err
	}
	dirs = append(dirs, cases...)
	if len(dirs) == 0 {
		return nil
	}
	if err := deriveAll(dirs, check); err != nil {
		return err
	}
	paths := runtime.Locate(cache, f.Runtime, l.PoB)
	res, err := runBuilds(ctx, paths, dirs)
	if err != nil {
		return err
	}
	return storeAll(dirs, l.PoB, res, check)
}

// runBuilds runs each build directory's XMLs through PoB, each build in a process of its own,
// and returns each build's results in the order of jobsOf. PoB keeps state between the builds
// it loads (shared tables, caches), so a build run after others can give other values than the
// same build alone.
func runBuilds(ctx context.Context, p runtime.Paths, dirs []string) ([][]Result, error) {
	res := make([][]Result, len(dirs))
	errs := make([]error, len(dirs))
	sem := make(chan struct{}, min(goruntime.NumCPU(), maxProcs))
	var wg sync.WaitGroup
	for i, d := range dirs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			var jobs []Job
			if jobs, errs[i] = jobsOf(d); errs[i] == nil {
				res[i], errs[i] = RunJobs(ctx, p, jobs)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return res, nil
}

// jobsOf returns the jobs of a build directory: its XMLs, in the order of xmlNames, then, with
// a party file, its full XML with the ally's.
func jobsOf(dir string) ([]Job, error) {
	jobs := make([]Job, 0, len(xmlNames())+1)
	for _, name := range xmlNames() {
		p, err := filepath.Abs(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("pob: %w", err)
		}
		jobs = append(jobs, Job{File: p})
	}
	ally, err := allyOf(dir)
	if err != nil || ally == "" {
		return jobs, err
	}
	return append(jobs, Job{File: jobs[0].File, Ally: ally}), nil
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

// storeAll stores each build's results, res[i] being those of dirs[i] as jobsOf orders them.
func storeAll(dirs []string, r pins.Release, res [][]Result, check bool) error {
	var stale []string
	for i, d := range dirs {
		v := Values{
			PoB: r, Full: res[i][0].restrict(true), Skill: res[i][1].restrict(true),
			Gear: res[i][2].restrict(false), TreeOnly: res[i][3].restrict(false),
		}
		if len(res[i]) > len(xmlNames()) {
			p := res[i][len(xmlNames())].restrict(true)
			v.Party = &p
		}
		same, err := store(d, v, check)
		if err != nil {
			return err
		}
		if !same {
			stale = append(stale, d)
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
