// Command pbtc runs pbt-conformance from the repository root.
//
//	pbtc setup --cache <dir>
//	pbtc pob --cache <dir> [--league <league>] [--check]
//	pbtc compare --datasets <dir> [--league <league>]
//
// setup builds the PoB runtime every pinned release needs (LuaJIT, lua-utf8, PoB checkouts)
// into the cache directory, skipping what is already built. It needs git, make and cc.
//
// pob runs the builds of a league (default: every pinned league) through its pinned PoB and
// writes each build's pob.json. With --check it writes nothing and fails when a stored
// pob.json differs from the fresh run.
//
// compare computes pbt-engine's sheet for every build of a league (default: every pinned
// league) with the pinned dataset under --datasets (one directory per dataset version) and
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
	"slices"
	"text/tabwriter"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"
)

const game = "poe1"

var (
	errUsage = errors.New("usage: pbtc setup --cache <dir> | pbtc pob --cache <dir> [--league <league>] [--check] | " +
		"pbtc compare --datasets <dir> [--league <league>]")
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
	cache := fs.String("cache", "", "cache directory for the PoB runtime (setup, pob)")
	datasets := fs.String("datasets", "", "directory with one dataset per version (compare)")
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
	switch {
	case cmd == "setup" && cache != "":
		if err := runtime.Setup(ctx, cache, f); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
		return nil
	case cmd == "pob" && cache != "":
		return runPoB(ctx, f, cache, league, check)
	case cmd == "compare" && datasets != "":
		return runCompare(os.Stdout, f, datasets, league)
	}
	return errUsage
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
		ms, err := compare.Run(".", datasets, f, game, l)
		if err != nil {
			return fmt.Errorf("compare %s: %w", l, err)
		}
		all = append(all, ms...)
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
