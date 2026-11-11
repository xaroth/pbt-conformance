// Package pob runs build XMLs through Path of Building headless and records the values the
// conformance comparison reads.
package pob

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/xaroth/pbt-conformance/runtime"

	_ "embed"
)

//go:embed runner.lua
var runner []byte

// ErrBuild marks a build PoB could not load or calculate.
var ErrBuild = errors.New("pob failed on build")

const (
	linePrefix = "PBTC "
	// maxLine bounds one runner line: a build's outputs as JSON.
	maxLine = 1 << 20
)

// Result is what PoB computed for one build XML.
type Result struct {
	// Allocated is the number of nodes PoB allocated, class and ascendancy starts included;
	// fewer than the build lists means PoB dropped some.
	Allocated int `json:"allocated"`
	// Outputs holds PoB's value per output key; a key PoB gives no number for is absent.
	Outputs map[string]float64 `json:"outputs"`
	// MainSkill is the skill PoB calculates the build's damage for; nil in a layer whose skill
	// outputs are not recorded.
	MainSkill *MainSkill `json:"mainSkill,omitempty"`
}

// MainSkill is the skill PoB took as a build's main skill.
type MainSkill struct {
	// Group is the skill's socket group, counted from 1 in the build file; 0 when the skill
	// comes from no group of the file, such as PoB's default attack.
	Group int `json:"group,omitempty"`
	// ID and Name are the skill's granted effect.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Totem, Trap and Mine say how the skill is used.
	Totem bool `json:"totem,omitempty"`
	Trap  bool `json:"trap,omitempty"`
	Mine  bool `json:"mine,omitempty"`
	// Minion is the minion the skill summons, if any.
	Minion *Minion `json:"minion,omitempty"`
}

// Minion is the main skill's minion and the minion's own main skill.
type Minion struct {
	Name  string `json:"name"`
	Skill string `json:"skill"`
}

// Groups returns the groups of outputs recorded in r, in the order of Groups: the sheet and
// defences, and with a main skill the groups that apply to it.
func (r Result) Groups() []Group {
	all := r.MainSkill.groups()
	return slices.DeleteFunc(Groups(), func(g Group) bool { return !slices.Contains(all, g) })
}

// groups returns the groups of outputs that apply to a build with main skill ms.
func (ms *MainSkill) groups() []Group {
	g := []Group{GroupSheet, GroupDefence}
	if ms == nil {
		return g
	}
	g = append(g, GroupSkill)
	if ms.Minion != nil {
		g = append(g, GroupMinion)
	}
	if ms.Totem || ms.Trap || ms.Mine {
		g = append(g, GroupDelivery)
	}
	return g
}

// restrict returns r as recorded for a layer: with withSkill its main skill and the outputs of
// the groups that apply to it, else the sheet and defences only.
func (r Result) restrict(withSkill bool) Result {
	if !withSkill {
		r.MainSkill = nil
	}
	keep := keysOf(Outputs(), r.MainSkill.groups())
	out := make(map[string]float64, len(keep))
	for _, k := range keep {
		if v, ok := r.Outputs[k]; ok {
			out[k] = v
		}
	}
	r.Outputs = out
	return r
}

type line struct {
	Result

	File  string `json:"file"`
	Error string `json:"error"`
}

// Job is a build XML to run, and the XML of its ally, whose party buffs apply, if any.
type Job struct {
	File, Ally string
}

// Run runs each file through PoB at p in one process and returns the results in file order.
func Run(ctx context.Context, p runtime.Paths, files []string) ([]Result, error) {
	jobs := make([]Job, len(files))
	for i, f := range files {
		jobs[i] = Job{File: f}
	}
	return RunJobs(ctx, p, jobs)
}

// RunJobs runs each job through PoB at p in one process and returns the results in job order.
// A job with an ally runs as PoB's Party tab would with the ally's exported support imported.
func RunJobs(ctx context.Context, p runtime.Paths, jobs []Job) ([]Result, error) {
	script, err := os.CreateTemp("", "pbtc-runner-*.lua")
	if err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	defer os.Remove(script.Name())
	if _, err := script.Write(runner); err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	if err := script.Close(); err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	cmd := exec.CommandContext(ctx, p.LuaJIT, append([]string{script.Name()}, Keys()...)...)
	cmd.Dir = p.PoBSrc
	cmd.Env = append(os.Environ(), "LUA_PATH="+runtime.LuaPath, "LUA_CPATH="+p.CPath)
	var in strings.Builder
	files := make([]string, len(jobs))
	for i, j := range jobs {
		files[i] = j.File
		in.WriteString(j.File)
		if j.Ally != "" {
			in.WriteString("\t" + j.Ally)
		}
		in.WriteString("\n")
	}
	cmd.Stdin = strings.NewReader(in.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pob: %w\n%s", err, stderr.Bytes())
	}
	return parse(out, files)
}

func parse(out []byte, files []string) ([]Result, error) {
	var results []Result
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, maxLine)
	for sc.Scan() {
		text, ok := strings.CutPrefix(sc.Text(), linePrefix)
		if !ok {
			continue
		}
		var l line
		if err := json.Unmarshal([]byte(text), &l); err != nil {
			return nil, fmt.Errorf("pob: runner line: %w", err)
		}
		if l.Error != "" {
			return nil, fmt.Errorf("%w %s: %s", ErrBuild, l.File, l.Error)
		}
		if len(results) >= len(files) || l.File != files[len(results)] {
			return nil, fmt.Errorf("%w: unexpected result for %s", ErrBuild, l.File)
		}
		results = append(results, l.Result)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pob: %w", err)
	}
	if len(results) != len(files) {
		return nil, fmt.Errorf("%w: %d results for %d files", ErrBuild, len(results), len(files))
	}
	return results, nil
}
