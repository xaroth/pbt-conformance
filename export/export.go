// Package export checks that Path of Building reads pbt-engine's export of a build as it reads
// the build's own file: each recorded file of a build (pob.xml and the layers derived from it)
// is imported and exported with the engine, the export runs through the league's pinned PoB,
// and PoB's values are compared with the build's record of that layer.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	pobxml "github.com/xaroth/pbt-engine/pob"
)

// relTolerance is how far apart the export's value and the record's may be, relative to the
// record's.
const relTolerance = 0.001

// filePerm is the mode of an export, read only by the PoB run.
const filePerm = 0o600

// floatNoise absorbs float noise in values that are equal.
const floatNoise = 1e-9

// Outputs beside pob.Outputs that a check compares.
const (
	// AllocatedOutput is the number of nodes PoB allocated.
	AllocatedOutput = "allocated_nodes"
	// MainSkillOutput is the main skill PoB took, with its level, quality, minion and supports.
	MainSkillOutput = "main_skill"
)

// Layer is a recorded file of a build and its record in the values file.
type Layer struct {
	// Name is the layer's key in the values file.
	Name string
	// File is the layer's build file.
	File string
	// WithSkill is set for a layer recorded with its main skill.
	WithSkill bool
	record    func(*pob.Values) pob.Result
}

// Layers returns the layers a check exports, in report order.
func Layers() []Layer {
	return []Layer{
		{"full", pob.FullXML, true, func(v *pob.Values) pob.Result { return v.Full }},
		{"skill", pob.SkillXML, true, func(v *pob.Values) pob.Result { return v.Skill }},
		{"gear", pob.GearXML, false, func(v *pob.Values) pob.Result { return v.Gear }},
		{"treeOnly", pob.TreeOnlyXML, false, func(v *pob.Values) pob.Result { return v.TreeOnly }},
	}
}

// Build is the check of one build's layers.
type Build struct {
	League, Name string
	// Layers are the checks of the build's layers, in the order of Layers.
	Layers []Check
}

// Check is the check of one layer of a build.
type Check struct {
	Layer string
	// Import and Export are the engine's warnings on reading the layer's file and on writing
	// the export: what each leaves out.
	Import, Export []build.Warning
	// Error is what PoB raised on the export, nil when it read it.
	Error error
	// Diffs are the outputs PoB gives the export otherwise than the layer's file, in the order
	// of Compare.
	Diffs []Diff
}

// Matches reports whether PoB reads the export as the layer's file.
func (c Check) Matches() bool { return c.Error == nil && len(c.Diffs) == 0 }

// Diff is an output PoB gives the export otherwise than the build's file. A value PoB does not
// give is empty.
type Diff struct {
	Output         string
	Export, Record string
}

// Run exports every layer of every build and case of league under root with the pinned dataset
// under datasets, runs each export through the league's pinned PoB in cache, each in a process
// of its own, and compares PoB's values with the build's record of the layer.
func Run(ctx context.Context, root, cache, datasets string, f *pins.File, game, league string) ([]Build, error) {
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return nil, fmt.Errorf("export: %s %s: %w", game, league, compare.ErrDataset)
	}
	reg, _, err := compare.OpenPinned(datasets, game, l.Dataset)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	dirs, err := buildDirs(root, league)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "pbtc-export-")
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	defer os.RemoveAll(tmp)
	out, err := exportAll(dirs, league, tmp, reg)
	if err != nil {
		return nil, err
	}
	res, errs, err := pob.RunAlone(ctx, runtime.Locate(cache, f.Runtime, l.PoB), exportFiles(tmp, len(dirs)))
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	if err := compareAll(dirs, out, res, errs); err != nil {
		return nil, err
	}
	return out, nil
}

// exportAll exports every layer of the builds in dirs into tmp, at the paths exportFiles
// gives, and returns the builds with each layer's warnings.
func exportAll(dirs []string, league, tmp string, reg *dataset.Registry) ([]Build, error) {
	dst, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	defer dst.Close()
	layers := Layers()
	out := make([]Build, len(dirs))
	for i, d := range dirs {
		out[i] = Build{League: league, Name: filepath.Base(d), Layers: make([]Check, len(layers))}
		for j, layer := range layers {
			c := &out[i].Layers[j]
			c.Layer = layer.Name
			if c.Import, c.Export, err = exportFile(filepath.Join(d, layer.File), dst, exportName(i, j), reg); err != nil {
				return nil, fmt.Errorf("export %s %s: %w", out[i].Name, layer.File, err)
			}
		}
	}
	return out, nil
}

// exportName is the file name of the export of build i's layer j.
func exportName(i, j int) string { return fmt.Sprintf("%d-%d.xml", i, j) }

// exportFiles returns the paths in tmp of the exports of n builds, build by build and layer by
// layer in the order of Layers.
func exportFiles(tmp string, n int) []string {
	files := make([]string, 0, n*len(Layers()))
	for i := range n {
		for j := range Layers() {
			files = append(files, filepath.Join(tmp, exportName(i, j)))
		}
	}
	return files
}

// compareAll compares PoB's results for the exports, as exportFiles orders them, with each
// build's record, and stores the differences and PoB's errors in out.
func compareAll(dirs []string, out []Build, res []pob.Result, errs []error) error {
	layers := Layers()
	for i, d := range dirs {
		v, err := readValues(d)
		if err != nil {
			return fmt.Errorf("export %s: %w", out[i].Name, err)
		}
		for j, layer := range layers {
			k := i*len(layers) + j
			c := &out[i].Layers[j]
			if c.Error = errs[k]; c.Error == nil {
				c.Diffs = Compare(res[k].Restrict(layer.WithSkill), layer.record(v))
			}
		}
	}
	return nil
}

// buildDirs returns the build directories of league under root, then its cases.
func buildDirs(root, league string) ([]string, error) {
	dirs, err := pob.BuildDirs(root, league)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	cases, err := pob.CaseDirs(root, league)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}
	return append(dirs, cases...), nil
}

// exportFile imports the build file src, exports it to name in dst and returns the warnings of
// both.
func exportFile(src string, dst *os.Root, name string, reg *dataset.Registry) (imp, exp []build.Warning, err error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, nil, fmt.Errorf("read: %w", err)
	}
	b, imp, err := pobxml.FromXML(data, reg)
	if err != nil {
		return nil, nil, fmt.Errorf("import: %w", err)
	}
	x, exp, err := pobxml.ToXML(b, reg)
	if err != nil {
		return nil, nil, fmt.Errorf("export: %w", err)
	}
	if err := dst.WriteFile(name, x, filePerm); err != nil {
		return nil, nil, fmt.Errorf("export: %w", err)
	}
	return imp, exp, nil
}

func readValues(dir string) (*pob.Values, error) {
	b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
	if err != nil {
		return nil, fmt.Errorf("read pob values: %w", err)
	}
	var v pob.Values
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("pob values: %w", err)
	}
	return &v, nil
}

// Compare returns the outputs of PoB's result for an export that differ from rec, the record
// of the layer it was exported from, both restricted to the layer: the allocated nodes, the
// main skill, and the outputs of the groups of either main skill, each within relTolerance of
// rec's.
func Compare(export, rec pob.Result) []Diff {
	var out []Diff
	if export.Allocated != rec.Allocated {
		out = append(out, Diff{Output: AllocatedOutput, Export: strconv.Itoa(export.Allocated), Record: strconv.Itoa(rec.Allocated)})
	}
	if !reflect.DeepEqual(export.MainSkill, rec.MainSkill) {
		out = append(out, Diff{Output: MainSkillOutput, Export: skillText(export.MainSkill), Record: skillText(rec.MainSkill)})
	}
	groups := slices.Concat(export.Groups(), rec.Groups())
	for _, o := range pob.Outputs() {
		if !slices.Contains(groups, o.Group) {
			continue
		}
		e, okE := o.Value(export.Outputs)
		r, okR := o.Value(rec.Outputs)
		if okE == okR && (!okE || math.Abs(e-r) <= max(relTolerance*math.Abs(r), floatNoise)) {
			continue
		}
		out = append(out, Diff{Output: o.Name, Export: valueText(e, okE), Record: valueText(r, okR)})
	}
	return out
}

func valueText(v float64, ok bool) string {
	if !ok {
		return ""
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func skillText(ms *pob.MainSkill) string {
	if ms == nil {
		return ""
	}
	b, err := json.Marshal(ms)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
