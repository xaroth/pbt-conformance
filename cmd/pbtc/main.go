// Command pbtc runs pbt-conformance from the repository root.
//
//	pbtc setup --cache <dir>
//	pbtc pob --cache <dir> [--league <league>] [--check]
//
// setup builds the PoB runtime every pinned release needs (LuaJIT, lua-utf8, PoB checkouts)
// into the cache directory, skipping what is already built. It needs git, make and cc.
//
// pob runs the builds of a league (default: every pinned league) through its pinned PoB and
// writes each build's pob.json. With --check it writes nothing and fails when a stored
// pob.json differs from the fresh run.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"
)

const game = "poe1"

var errUsage = errors.New("usage: pbtc setup --cache <dir> | pbtc pob --cache <dir> [--league <league>] [--check]")

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
	cache := fs.String("cache", "", "cache directory for the PoB runtime")
	league := fs.String("league", "", "league to run (pob; default all pinned)")
	check := fs.Bool("check", false, "fail on stored values that differ instead of writing (pob)")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	if *cache == "" {
		return errUsage
	}
	f, err := pins.Read(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	switch args[0] {
	case "setup":
		if err := runtime.Setup(ctx, *cache, f); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
		return nil
	case "pob":
		return runPoB(ctx, f, *cache, *league, *check)
	}
	return errUsage
}

func runPoB(ctx context.Context, f *pins.File, cache, league string, check bool) error {
	leagues := slices.Sorted(maps.Keys(f.Games[game].Leagues))
	if league != "" {
		leagues = []string{league}
	}
	for _, l := range leagues {
		if err := pob.Record(ctx, ".", cache, f, game, l, check); err != nil {
			return fmt.Errorf("pob %s: %w", l, err)
		}
	}
	return nil
}
