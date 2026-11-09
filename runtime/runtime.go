// Package runtime builds the Path of Building runtime a conformance run needs, from the
// sources pins.json names, in a cache directory: LuaJIT, the lua-utf8 module and a PoB checkout
// per pinned release. Each part is built once per commit; a finished part has a marker file, so
// an interrupted setup is redone.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xaroth/pbt-conformance/pins"
)

const (
	doneMarker = ".done"
	dirPerm    = 0o755
	filePerm   = 0o600
)

// Paths locate a built runtime for one PoB release.
type Paths struct {
	// LuaJIT is the luajit binary.
	LuaJIT string
	// CPath is a LUA_CPATH entry that finds lua-utf8.
	CPath string
	// PoBSrc is the src directory of the PoB checkout; PoB runs with it as working directory.
	PoBSrc string
}

// LuaPath is the LUA_PATH PoB needs, relative to PoBSrc.
const LuaPath = "../runtime/lua/?.lua;../runtime/lua/?/init.lua;./?.lua"

// Locate returns the paths of the runtime for release r under cache, built or not. The paths
// are absolute, since luajit runs from PoB's src directory.
func Locate(cache string, rt pins.Runtime, r pins.Release) Paths {
	cache = absolute(cache)
	return Paths{
		LuaJIT: filepath.Join(luajitDir(cache, rt), "bin", "luajit"),
		CPath:  filepath.Join(utf8Dir(cache, rt), "?.so"),
		PoBSrc: filepath.Join(pobDir(cache, r), "src"),
	}
}

func luajitDir(cache string, rt pins.Runtime) string {
	return filepath.Join(cache, "luajit", rt.LuaJIT.Commit)
}

// utf8Dir is keyed by both commits: lua-utf8 is compiled against LuaJIT's headers.
func utf8Dir(cache string, rt pins.Runtime) string {
	return filepath.Join(cache, "luautf8", rt.LuaJIT.Commit+"-"+rt.LuaUTF8.Commit)
}

// absolute returns path made absolute. Abs fails only when the working directory is unknown;
// the path is then kept as given.
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func pobDir(cache string, r pins.Release) string { return filepath.Join(cache, "pob", r.Commit) }

// Setup builds what Locate points at for every pinned release, skipping finished parts. It
// needs git, make and a C compiler (cc).
func Setup(ctx context.Context, cache string, f *pins.File) error {
	// make, luajit and the build steps run in other directories than the caller's.
	cache = absolute(cache)
	if err := part(luajitDir(cache, f.Runtime), func(dir string) error { return buildLuaJIT(ctx, cache, dir, f.Runtime.LuaJIT) }); err != nil {
		return fmt.Errorf("luajit: %w", err)
	}
	if err := part(utf8Dir(cache, f.Runtime), func(dir string) error { return buildUTF8(ctx, cache, dir, f.Runtime) }); err != nil {
		return fmt.Errorf("lua-utf8: %w", err)
	}
	for _, g := range f.Games {
		for _, l := range g.Leagues {
			if err := part(pobDir(cache, l.PoB), func(dir string) error { return fetch(ctx, dir, pobRepo, l.PoB.Commit) }); err != nil {
				return fmt.Errorf("pob %s: %w", l.PoB.Tag, err)
			}
			if err := checkTree(ctx, Locate(cache, f.Runtime, l.PoB), l.Tree); err != nil {
				return fmt.Errorf("pob %s: %w", l.PoB.Tag, err)
			}
		}
	}
	return nil
}

// ErrWrongTree marks a PoB release whose newest tree is not its pinned league's.
var ErrWrongTree = errors.New("pob targets another tree")

// treeScript starts PoB headless and prints its newest tree version.
const treeScript = `arg = {}
dofile("HeadlessWrapper.lua")
io.write(latestTreeVersion)`

// checkTree starts PoB headless with p and checks it targets tree, which also checks that the
// runtime works.
func checkTree(ctx context.Context, p Paths, tree string) error {
	cmd := exec.CommandContext(ctx, p.LuaJIT, "-e", treeScript)
	cmd.Dir = p.PoBSrc
	cmd.Env = append(os.Environ(), "LUA_PATH="+LuaPath, "LUA_CPATH="+p.CPath)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("start headless: %w", err)
	}
	// HeadlessWrapper prints its own lines first; the tree version is the last output.
	got := string(out)
	if i := strings.LastIndexByte(got, '\n'); i >= 0 {
		got = got[i+1:]
	}
	if got != tree {
		return fmt.Errorf("%w: %q, pinned for %q", ErrWrongTree, got, tree)
	}
	return nil
}

const pobRepo = "https://github.com/PathOfBuildingCommunity/PathOfBuilding"

// part runs build into a fresh dir unless dir holds a finished part, then marks it finished.
// The paths are the operator's cache directory joined with pinned commits, which pins.Read
// checks are hex, so they cannot leave the cache.
func part(dir string, build func(dir string) error) error {
	if _, err := os.Stat(filepath.Join(dir, doneMarker)); err == nil {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := build(dir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, doneMarker), nil, filePerm); err != nil {
		return fmt.Errorf("mark %s: %w", dir, err)
	}
	return nil
}

// fetch checks out commit of repo into dir without history.
func fetch(ctx context.Context, dir, repo, commit string) error {
	for _, args := range [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth", "1", repo, commit},
		{"checkout", "-q", "FETCH_HEAD"},
	} {
		if err := run(ctx, dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func buildLuaJIT(ctx context.Context, cache, dir string, src pins.Source) error {
	work := filepath.Join(cache, "src", "luajit-"+src.Commit)
	if err := part(work, func(d string) error { return fetch(ctx, d, src.Repo, src.Commit) }); err != nil {
		return err
	}
	// LuaJIT bakes the prefix into the binary; build and install in one go.
	return run(ctx, work, "make", "-s", "install", "PREFIX="+dir)
}

func buildUTF8(ctx context.Context, cache, dir string, rt pins.Runtime) error {
	work := filepath.Join(cache, "src", "luautf8-"+rt.LuaUTF8.Commit)
	if err := part(work, func(d string) error { return fetch(ctx, d, rt.LuaUTF8.Repo, rt.LuaUTF8.Commit) }); err != nil {
		return err
	}
	include := filepath.Join(luajitDir(cache, rt), "include", "luajit-2.1")
	return run(ctx, work, "cc", "-O2", "-shared", "-fPIC", "-I"+include, "-o", filepath.Join(dir, "lua-utf8.so"), "lutf8lib.c")
}

// run runs one of the fixed tools setup needs (git, make, cc) with arguments built from
// pins.json, whose commits and repos pins.Read checks.
func run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v in %s: %w\n%s", name, args, dir, err, out)
	}
	return nil
}
