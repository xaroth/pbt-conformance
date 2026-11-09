// Package pins reads pins.json: the pbt-data commit datasets come from, per game and league the
// dataset and Path of Building release a conformance run uses, plus the sources the PoB runtime
// is built from, so every run can be repeated exactly.
package pins

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

// Name is the pins file's name at the repository root.
const Name = "pins.json"

// ErrInvalid is wrapped by every error Read returns for a malformed pins file.
var ErrInvalid = errors.New("invalid pins")

// Details of ErrInvalid; each is part of the error text.
var (
	errNoGames    = errors.New("no games")
	errNotPinned  = errors.New("not pinned")
	errNoTag      = errors.New("no pob tag")
	errOtherTree  = errors.New("is not the league's tree")
	errNotPatch   = errors.New("is not a patch of the league")
	errNotHTTPS   = errors.New("want an https URL")
	errNotHexHash = errors.New("want lowercase hex digits")
	errNotGitHub  = errors.New("want a https://github.com/ repository")
)

const (
	commitLen    = 40
	datasetIDLen = 64
)

// File is the content of pins.json.
type File struct {
	Runtime Runtime `json:"runtime"`
	// Data is the pbt-data commit the pinned datasets are pulled from.
	Data  Source          `json:"data"`
	Games map[string]Game `json:"games"`
}

// githubPrefix is where Data's repository must live: RawURL reads it through raw GitHub.
const githubPrefix = "https://github.com/"

// RawURL is the raw GitHub URL of the files of repository s at its commit, ending in a slash.
func (s Source) RawURL() string {
	return "https://raw.githubusercontent.com/" + strings.TrimPrefix(s.Repo, githubPrefix) + "/" + s.Commit + "/"
}

// Runtime are the sources the PoB runtime is built from.
type Runtime struct {
	LuaJIT  Source `json:"luajit"`
	LuaUTF8 Source `json:"luautf8"`
}

// Source is a git repository at a commit.
type Source struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
}

// Game holds the leagues of one game, keyed by league ("3.29"). Current is the league PoB
// targets now; its pin follows new PoB releases, the others are frozen.
type Game struct {
	Current string            `json:"current"`
	Leagues map[string]League `json:"leagues"`
}

// League pins what a run for one league uses.
type League struct {
	PoB Release `json:"pob"`
	// Tree is PoB's tree version for the league, such as "3_29".
	Tree    string  `json:"tree"`
	Dataset Dataset `json:"dataset"`
}

// Release is a PoB release by tag and the commit the tag pointed to when pinned.
type Release struct {
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

// Dataset is a pbt dataset by game version and content id; a rebuilt dataset keeps its
// version but gets a new id.
type Dataset struct {
	Version string `json:"version"`
	ID      string `json:"id"`
}

// Read reads and checks the pins file in fsys.
func Read(fsys fs.FS) (*File, error) {
	b, err := fs.ReadFile(fsys, Name)
	if err != nil {
		return nil, fmt.Errorf("read pins: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", Name, ErrInvalid, err)
	}
	if err := f.check(); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", Name, ErrInvalid, err)
	}
	return &f, nil
}

func (f *File) check() error {
	for name, s := range map[string]Source{"luajit": f.Runtime.LuaJIT, "luautf8": f.Runtime.LuaUTF8} {
		if err := s.check(); err != nil {
			return fmt.Errorf("runtime %s: %w", name, err)
		}
	}
	if err := f.Data.check(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if !strings.HasPrefix(f.Data.Repo, githubPrefix) {
		return fmt.Errorf("data repo %q: %w", f.Data.Repo, errNotGitHub)
	}
	if len(f.Games) == 0 {
		return errNoGames
	}
	for _, game := range slices.Sorted(maps.Keys(f.Games)) {
		if err := f.Games[game].check(); err != nil {
			return fmt.Errorf("%s: %w", game, err)
		}
	}
	return nil
}

func (s Source) check() error {
	if !strings.HasPrefix(s.Repo, "https://") {
		return fmt.Errorf("repo %q: %w", s.Repo, errNotHTTPS)
	}
	return checkHex("commit", s.Commit, commitLen)
}

func (g Game) check() error {
	if _, ok := g.Leagues[g.Current]; !ok {
		return fmt.Errorf("current league %q %w", g.Current, errNotPinned)
	}
	for _, name := range slices.Sorted(maps.Keys(g.Leagues)) {
		if err := g.Leagues[name].check(name); err != nil {
			return fmt.Errorf("league %s: %w", name, err)
		}
	}
	return nil
}

func (l League) check(name string) error {
	switch {
	case l.PoB.Tag == "":
		return errNoTag
	case l.Tree != strings.ReplaceAll(name, ".", "_"):
		return fmt.Errorf("tree %q %w", l.Tree, errOtherTree)
	case !strings.HasPrefix(l.Dataset.Version, name+"."):
		return fmt.Errorf("dataset version %q %w", l.Dataset.Version, errNotPatch)
	}
	if err := checkHex("pob commit", l.PoB.Commit, commitLen); err != nil {
		return err
	}
	return checkHex("dataset id", l.Dataset.ID, datasetIDLen)
}

func checkHex(what, s string, n int) error {
	if _, err := hex.DecodeString(s); err != nil || len(s) != n || strings.ToLower(s) != s {
		return fmt.Errorf("%s %q: %w, %d of them", what, s, errNotHexHash, n)
	}
	return nil
}
