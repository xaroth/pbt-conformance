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

// Keys are the PoB output keys recorded per build: the sheet values pbt-engine computes.
// PoB has no plain maximum resistance key; a maximum is <Elem>Resist + Missing<Elem>Resist.
func Keys() []string {
	return []string{
		"Str", "Dex", "Int", "Life", "Mana", "EnergyShield", "Armour", "Evasion",
		"LifeRegen", "ManaRegen", "EnergyShieldRegen",
		"FireResist", "FireResistTotal", "MissingFireResist",
		"ColdResist", "ColdResistTotal", "MissingColdResist",
		"LightningResist", "LightningResistTotal", "MissingLightningResist",
		"ChaosResist", "ChaosResistTotal", "MissingChaosResist",
	}
}

// Result is what PoB computed for one build XML.
type Result struct {
	// Allocated is the number of nodes PoB allocated, class and ascendancy starts included;
	// fewer than the build lists means PoB dropped some.
	Allocated int                `json:"allocated"`
	Outputs   map[string]float64 `json:"outputs"`
}

type line struct {
	Result

	File  string `json:"file"`
	Error string `json:"error"`
}

// Run runs each file through PoB at p in one process and returns the results in file order.
func Run(ctx context.Context, p runtime.Paths, files []string) ([]Result, error) {
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
	cmd.Stdin = strings.NewReader(strings.Join(files, "\n") + "\n")
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
