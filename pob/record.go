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
	// TreeOnlyXML is the same build with only the tree, to tell tree and gear causes apart.
	TreeOnlyXML = "pob-treeonly.xml"
	// ValuesFile holds what PoB computed for both XMLs.
	ValuesFile = "pob.json"
)

// ErrStale marks a stored values file that differs from a fresh PoB run.
var ErrStale = errors.New("stored pob values differ from a fresh run")

const filePerm = 0o644

// Values is a build's values file: PoB's results for both XMLs and the release that made them.
type Values struct {
	PoB      pins.Release `json:"pob"`
	Full     Result       `json:"full"`
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

// Record runs every build of league under root through its pinned PoB in cache and writes
// each build's values file. With check it writes nothing and fails with ErrStale when a stored
// file differs from the fresh run, naming the builds.
func Record(ctx context.Context, root, cache string, f *pins.File, game, league string, check bool) error {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return fmt.Errorf("pob: %s %s: %w", game, league, errNotPinned)
	}
	dirs, err := BuildDirs(root, league)
	if err != nil || len(dirs) == 0 {
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

// storeAll stores each build's pair of results, full then tree-only, as it lies in res.
func storeAll(dirs []string, r pins.Release, res []Result, check bool) error {
	var stale []string
	for i, d := range dirs {
		full, treeOnly := res[0], res[1]
		res = res[2:]
		same, err := store(d, Values{PoB: r, Full: full, TreeOnly: treeOnly}, check)
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

// xmlFiles returns the full and tree-only XML of each build directory, absolute, in order.
func xmlFiles(dirs []string) ([]string, error) {
	names := []string{FullXML, TreeOnlyXML}
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
	b = append(b, '\n')
	path := filepath.Join(dir, ValuesFile)
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
