// Command pbtc runs pbt-conformance from the repository root.
//
//	pbtc setup --cache <dir>
//	pbtc pob --cache <dir> [--league <league>] [--check]
//	pbtc data --cache <dir>
//	pbtc compare (--cache <dir> | --datasets <dir>) [--league <league>]
//
// setup builds the PoB runtime every pinned release needs (LuaJIT, lua-utf8, PoB checkouts)
// into the cache directory, skipping what is already built. It needs git, make and cc.
//
// pob derives each build's pob-gear.xml from its pob.xml, runs the builds of a league (default:
// every pinned league) through its pinned PoB and writes each build's pob.json. With --check it
// writes nothing and fails when a stored pob-gear.xml or pob.json differs from a fresh run.
//
// data pulls the datasets of every pinned league from the pinned pbt-data commit into
// <cache>/data/<commit>/<game> and prints that directory; a later run fetches nothing.
//
// compare computes pbt-engine's sheet for every build of a league (default: every pinned
// league) with the pinned dataset, from --datasets (directories of datasets, one per league or
// per version) or else pulled into --cache as data does, and
// compares it with the stored PoB values for the tree-only build. It lists every mismatch,
// with its triage.json entry if one accepts it, and fails on untriaged mismatches and on
// triage entries that match nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"text/tabwriter"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"

	"github.com/xaroth/pbt-engine/dataset/remote"
)

const game = "poe1"

var (
	errUsage = errors.New("usage: pbtc setup --cache <dir> | pbtc pob --cache <dir> [--league <league>] [--check] | " +
		"pbtc data --cache <dir> | pbtc compare (--cache <dir> | --datasets <dir>) [--league <league>]")
	errMismatch = errors.New("untriaged mismatches or stale triage entries")
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pbtc:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	cache := fs.String("cache", "", "cache directory for the PoB runtime and pulled datasets (setup, pob, data, compare)")
	datasets := fs.String("datasets", "", "directory of datasets to compare with instead of pulling them (compare)")
	league := fs.String("league", "", "league to run (pob, compare; default all pinned)")
	check := fs.Bool("check", false, "fail on stored values that differ instead of writing (pob)")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return dispatch(ctx, args[0], *cache, *datasets, *league, *check)
}

func dispatch(ctx context.Context, cmd, cache, datasets, league string, check bool) error {
	f, err := pins.Read(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	// Each command runs when its required flags are set.
	commands := map[string]struct {
		ready bool
		run   func() error
	}{
		"setup":   {cache != "", func() error { return runSetup(ctx, f, cache) }},
		"pob":     {cache != "", func() error { return runPoB(ctx, f, cache, league, check) }},
		"data":    {cache != "", func() error { return runData(ctx, os.Stdout, f, cache) }},
		"compare": {datasets != "" || cache != "", func() error { return runCompareFrom(ctx, f, cache, datasets, league) }},
	}
	c, ok := commands[cmd]
	if !ok || !c.ready {
		return errUsage
	}
	return c.run()
}

func runSetup(ctx context.Context, f *pins.File, cache string) error {
	if err := runtime.Setup(ctx, cache, f); err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	return nil
}

// runData pulls the pinned datasets and prints their directory.
func runData(ctx context.Context, w io.Writer, f *pins.File, cache string) error {
	dir, err := pullData(ctx, os.Stderr, f, cache)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, dir); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

// runCompareFrom compares with the datasets under datasets, or pulled into cache without it.
func runCompareFrom(ctx context.Context, f *pins.File, cache, datasets, league string) error {
	if datasets == "" {
		var err error
		if datasets, err = pullData(ctx, os.Stderr, f, cache); err != nil {
			return err
		}
	}
	return runCompare(os.Stdout, f, datasets, league)
}

// pullData brings the pinned leagues' datasets of the pinned pbt-data commit up to date under
// cache, noting what it did on notes, and returns their directory.
func pullData(ctx context.Context, notes io.Writer, f *pins.File, cache string) (string, error) {
	dir := filepath.Join(cache, "data", f.Data.Commit, game)
	rs, err := remote.Pull(ctx, remote.HTTP{Base: f.Data.RawURL()}, dir, game, leagues(f, "")...)
	for _, r := range rs {
		state := "current"
		if r.Updated {
			state = fmt.Sprintf("pulled, %d files", r.Fetched)
		}
		fmt.Fprintf(notes, "pbtc: %s %s %s (%s)\n", game, r.League, r.Version, state)
	}
	if err != nil {
		return "", fmt.Errorf("data: %w", err)
	}
	return dir, nil
}

func leagues(f *pins.File, league string) []string {
	if league != "" {
		return []string{league}
	}
	return slices.Sorted(maps.Keys(f.Games[game].Leagues))
}

func runCompare(w io.Writer, f *pins.File, datasets, league string) error {
	triage, err := compare.ReadTriage(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("compare: %w", err)
	}
	var all []compare.Mismatch
	for _, l := range leagues(f, league) {
		bs, err := compare.Run(".", datasets, f, game, l)
		if err != nil {
			return fmt.Errorf("compare %s: %w", l, err)
		}
		all = append(all, compare.Mismatches(bs)...)
	}
	stale := compare.Apply(all, triage)
	return report(w, all, stale)
}

// report lists the mismatches and stale triage entries and fails when any is not accepted.
func report(w io.Writer, ms []compare.Mismatch, stale []string) error {
	untriaged := 0
	const padding = 2
	tw := tabwriter.NewWriter(w, 0, 0, padding, ' ', 0)
	fmt.Fprintln(tw, "league\tbuild\toutput\tours\tpob tree\tpob full\ttriage")
	for _, m := range ms {
		p := "-"
		if m.HasPoB {
			p = fmt.Sprint(m.PoB)
		}
		t := m.Triage
		if t == "" {
			t = "UNTRIAGED"
			untriaged++
		}
		if m.Detail != "" {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\t\t%s\n", m.League, m.Build, m.Output, m.Detail, t)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\t%v\t%s\n", m.League, m.Build, m.Output, m.Ours, p, m.PoBFull, t)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("compare: %w", err)
	}
	for _, id := range stale {
		fmt.Fprintf(w, "stale triage entry: %s\n", id)
	}
	fmt.Fprintf(w, "%d mismatches, %d untriaged, %d stale triage entries\n", len(ms), untriaged, len(stale))
	if untriaged > 0 || len(stale) > 0 {
		return errMismatch
	}
	return nil
}

func runPoB(ctx context.Context, f *pins.File, cache, league string, check bool) error {
	for _, l := range leagues(f, league) {
		if err := pob.Record(ctx, ".", cache, f, game, l, check); err != nil {
			return fmt.Errorf("pob %s: %w", l, err)
		}
	}
	return nil
}
