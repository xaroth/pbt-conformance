package pob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// GearXML is the tree and the gear, derived from FullXML by Gear.
	GearXML = "pob-gear.xml"
	// TreeOnlyXML is the same build with only the tree, to tell tree and gear causes apart.
	TreeOnlyXML = "pob-treeonly.xml"
	// ValuesFile holds what PoB computed for the three XMLs.
	ValuesFile = "pob.json"
)

// ErrStale marks a stored values file that differs from a fresh PoB run.
var ErrStale = errors.New("stored pob files differ from a fresh run")

const filePerm = 0o644

// Values is a build's values file: PoB's results for the three XMLs and the release that made
// them. The skill, minion and delivery outputs are recorded for the full build only: the other
// layers have no skills of their own.
type Values struct {
	PoB      pins.Release `json:"pob"`
	Full     Result       `json:"full"`
	Gear     Result       `json:"gear"`
	TreeOnly Result       `json:"treeOnly"`
}

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

// Record derives each build's gear XML, runs every build of league under root through its
// pinned PoB in cache and writes each build's values file. With check it writes nothing and
// fails with ErrStale when a stored gear XML or values file differs from a fresh run, naming
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
	files, err := xmlFiles(dirs)
	if err != nil {
		return err
	}
	res, err := Run(ctx, runtime.Locate(cache, f.Runtime, l.PoB), files)
	if err != nil {
		return err
	}
	return storeAll(dirs, l.PoB, res, check)
}

// deriveAll writes each build's gear XML, or with check fails with ErrStale when one differs.
func deriveAll(dirs []string, check bool) error {
	var stale []string
	for _, d := range dirs {
		full, err := os.ReadFile(filepath.Join(d, FullXML))
		if err != nil {
			return fmt.Errorf("pob: %w", err)
		}
		gear, err := Gear(full)
		if err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		same, err := update(filepath.Join(d, GearXML), gear, check)
		if err != nil {
			return err
		}
		if !same {
			stale = append(stale, d)
		}
	}
	if check && len(stale) > 0 {
		return fmt.Errorf("%w: %s: %v", ErrStale, GearXML, stale)
	}
	return nil
}

// storeAll stores each build's results, in xmlNames order as they lie in res.
func storeAll(dirs []string, r pins.Release, res []Result, check bool) error {
	var stale []string
	for i, d := range dirs {
		v := Values{PoB: r, Full: res[0].restrict(true), Gear: res[1].restrict(false), TreeOnly: res[2].restrict(false)}
		res = res[len(xmlNames()):]
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
	return []string{FullXML, GearXML, TreeOnlyXML}
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
