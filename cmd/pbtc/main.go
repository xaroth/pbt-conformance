// Command pbtc runs pbt-conformance from the repository root.
//
//	pbtc setup --cache <dir>
//	pbtc pob --cache <dir> [--league <league>] [--check]
//	pbtc data --cache <dir>
//	pbtc compare (--cache <dir> | --datasets <dir>) [--league <league>] [--scoreboard] [--json] [--hints <file>]
//	pbtc ninja --cache <dir> --overview <league> [--variant <league>] [--league <league>] [--skills <n>] [--mercenaries <n>]
//	           [--per-skill <n>] [--variant-rows <n>] [--dry-run]
//
// setup builds the PoB runtime every pinned release needs (LuaJIT, lua-utf8, PoB checkouts)
// into the cache directory, skipping what is already built. It needs git, make and cc.
//
// pob derives each build's pob-gear.xml and pob-treeonly.xml from its pob.xml, runs the builds of a league (default:
// every pinned league) through its pinned PoB and writes each build's pob.json. A build with a
// party.json is run once more with the party buffs of the ally it names. With --check it
// writes nothing and fails when a stored derived XML or pob.json differs from a fresh run.
//
// data pulls the datasets of every pinned league from the pinned pbt-data commit into
// <cache>/data/<commit>/<game> and prints that directory; a later run fetches nothing.
//
// compare computes pbt-engine's sheet for every build of a league (default: every pinned
// league) with the pinned dataset, from --datasets (directories of datasets, one per league or
// per version) or else pulled into --cache as data does, and
// compares it with the stored PoB values for the tree-only build. It lists every mismatch,
// with its triage.json entry if one accepts it, and fails on untriaged mismatches and on
// triage entries that match nothing. With --scoreboard it follows the list with the
// scoreboard: per league, layer and group of outputs how many builds pass, fail and are not
// compared, how many of each kind pass everything, the outputs the engine lacks, and the stats,
// gems and mechanics it leaves out by the number of builds they appear in, each stat with its
// text from the --hints file if given. --json writes the list and the scoreboard as JSON.
//
// ninja picks builds for a league (default: the current one) from poe.ninja's league named by
// --overview, such as allflame, and adds them to builds/<league> with their origin: for each
// kind, from the most popular main skills, one build per main skill and ascendancy until the
// kind has its target count, then builds of any kind until the league has its total, then
// builds with a content swapped in until each content has its quota, where it can. Candidates are the first characters of --per-skill ascendancies for each
// of the --skills most popular main skills and of the --mercenaries most popular mercenary
// classes, then --variant-rows characters of the --variant league (a Ruthless or alternate
// tree). Each candidate runs through the league's pinned PoB, whose
// result gives its kind; a candidate PoB fails on or gives no damage is not picked and is
// listed. Responses are
// cached under <cache>/ninja, so a rerun makes no request it made before. With --dry-run it
// prints the picks and writes nothing.
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
	"github.com/xaroth/pbt-conformance/ninja"
	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"

	"github.com/xaroth/pbt-engine/dataset/remote"
)

const game = "poe1"

var (
	errUsage = errors.New("usage: pbtc setup --cache <dir> | pbtc pob --cache <dir> [--league <league>] [--check] | " +
		"pbtc data --cache <dir> | pbtc compare (--cache <dir> | --datasets <dir>) [--league <league>] " +
		"[--scoreboard] [--json] [--hints <file>] | pbtc ninja --cache <dir> --overview <league> [--variant <league>] " +
		"[--league <league>] [--skills <n>] [--mercenaries <n>] [--per-skill <n>] [--variant-rows <n>] [--dry-run]")
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
	var out output
	fs.BoolVar(&out.scoreboard, "scoreboard", false, "follow the mismatches with the scoreboard (compare)")
	fs.BoolVar(&out.json, "json", false, "write the mismatches and the scoreboard as JSON (compare)")
	fs.StringVar(&out.hints, "hints", "", "JSON file of a hint per game stat id, a text or an object with a hint field, shown beside blocking stats (compare)")
	var pick picking
	fs.StringVar(&pick.src.Overview, "overview", "", "poe.ninja league to pick builds from, such as allflame (ninja)")
	fs.StringVar(&pick.src.Variant, "variant", "", "poe.ninja league on a Ruthless or alternate tree, such as allflamer (ninja)")
	fs.IntVar(&pick.src.Skills, "skills", defaultSkills, "most popular main skills to search (ninja)")
	fs.IntVar(&pick.src.Mercenaries, "mercenaries", defaultMercenaries, "most popular mercenary classes to search (ninja)")
	fs.IntVar(&pick.src.PerSkill, "per-skill", defaultPerSkill, "characters of different ascendancies per search (ninja)")
	fs.IntVar(&pick.src.VariantRows, "variant-rows", defaultVariantRows, "characters of the variant league (ninja)")
	fs.BoolVar(&pick.dryRun, "dry-run", false, "print the picks without writing builds (ninja)")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	if args[0] == "ninja" {
		return runNinja(ctx, *cache, *league, pick)
	}
	return dispatch(ctx, args[0], *cache, *datasets, *league, *check, out)
}

// Defaults of the ninja command.
const (
	defaultSkills      = 120
	defaultMercenaries = 5
	defaultPerSkill    = 2
	defaultVariantRows = 20
)

// picking is how the ninja command picks builds.
type picking struct {
	src    ninja.Source
	dryRun bool
}

// runNinja picks builds for league from poe.ninja and writes them, or with dryRun prints them.
func runNinja(ctx context.Context, cache, league string, pick picking) error {
	if cache == "" || pick.src.Overview == "" {
		return errUsage
	}
	f, err := pins.Read(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	if league == "" {
		league = f.Games[game].Current
	}
	l, ok := f.Games[game].Leagues[league]
	if !ok {
		return fmt.Errorf("ninja: %w: %s", errUsage, league)
	}
	plan, cands, err := planNinja(ctx, cache, league, runtime.Locate(cache, f.Runtime, l.PoB), l.PoB.Commit, pick.src)
	if err != nil {
		return err
	}
	if err := writePlan(os.Stdout, plan, cands); err != nil || pick.dryRun {
		return err
	}
	dirs, err := ninja.Write(".", league, plan.Picked)
	if err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	fmt.Fprintf(os.Stderr, "pbtc: wrote %d builds; record them with pbtc pob\n", len(dirs))
	return nil
}

// planNinja reads the candidates from poe.ninja, classifies them with PoB at p and picks from
// them.
func planNinja(ctx context.Context, cache, league string, p runtime.Paths, commit string, src ninja.Source) (ninja.Plan, []ninja.Candidate, error) {
	api := &ninja.API{Client: &ninja.Client{Dir: filepath.Join(cache, "ninja", "http")}}
	leads, err := ninja.Leads(ctx, api, src, league)
	if err != nil {
		return ninja.Plan{}, nil, fmt.Errorf("pick: %w", err)
	}
	cands, err := ninja.Fetch(ctx, api, leads)
	if err != nil {
		return ninja.Plan{}, nil, fmt.Errorf("pick: %w", err)
	}
	fmt.Fprintf(os.Stderr, "pbtc: %d candidates, %d requests made\n", len(cands), api.Client.Requests)
	dir := filepath.Join(cache, "ninja", "pob", commit+"-"+pob.RunnerID()[:16])
	if err := ninja.Classify(ctx, p, dir, cands); err != nil {
		return ninja.Plan{}, nil, fmt.Errorf("pick: %w", err)
	}
	existing, err := ninja.Existing(".", league)
	if err != nil {
		return ninja.Plan{}, nil, fmt.Errorf("pick: %w", err)
	}
	return ninja.Choose(existing, cands, compare.Targets(), compare.Total(), compare.Quotas()), cands, nil
}

// writePlan prints the picks, the counts per kind and content against their targets, and the
// candidates that cannot be picked.
func writePlan(w io.Writer, plan ninja.Plan, cands []ninja.Candidate) error {
	const padding = 2
	tw := tabwriter.NewWriter(w, 0, 0, padding, ' ', 0)
	fmt.Fprintln(tw, "kind\tskill\tascendancy\tlevel\tcontains\tsource")
	if err := ninja.WriteTable(tw, plan.Picked); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	targets, quotas := compare.Targets(), compare.Quotas()
	total := 0
	for _, k := range compare.Kinds() {
		total += plan.Kinds[k]
		if plan.Kinds[k] > 0 || targets[k] > 0 {
			fmt.Fprintf(w, "kind %s: %d, target %d\n", k, plan.Kinds[k], targets[k])
		}
	}
	fmt.Fprintf(w, "total: %d of %d\n", total, compare.Total())
	for _, c := range compare.Contents() {
		fmt.Fprintf(w, "contains %s: %d, quota %d\n", c, plan.Contents[c], quotas[c])
	}
	for _, c := range cands {
		if c.Err != nil {
			fmt.Fprintf(w, "not picked: %s: %v\n", c.Char.URL(c.Snapshot), c.Err)
		}
	}
	return nil
}

// output is how compare writes its result.
type output struct {
	scoreboard, json bool
	hints            string
}

func dispatch(ctx context.Context, cmd, cache, datasets, league string, check bool, out output) error {
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
		"compare": {datasets != "" || cache != "", func() error { return runCompareFrom(ctx, f, cache, datasets, league, out) }},
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
func runCompareFrom(ctx context.Context, f *pins.File, cache, datasets, league string, out output) error {
	if datasets == "" {
		var err error
		if datasets, err = pullData(ctx, os.Stderr, f, cache); err != nil {
			return err
		}
	}
	return runCompare(os.Stdout, f, datasets, league, out)
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

func runCompare(w io.Writer, f *pins.File, datasets, league string, out output) error {
	triage, err := compare.ReadTriage(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("compare: %w", err)
	}
	hints, err := readHints(out.hints)
	if err != nil {
		return err
	}
	builds, err := compareLeagues(f, datasets, league)
	if err != nil {
		return err
	}
	all := compare.Mismatches(builds)
	stale := compare.Apply(all, triage)
	if out.json {
		if err := compare.Score(builds, all, stale, hints).WriteJSON(w); err != nil {
			return fmt.Errorf("compare: %w", err)
		}
		return verdict(all, stale)
	}
	if err := report(w, all, stale); err != nil && !errors.Is(err, errMismatch) {
		return err
	}
	if out.scoreboard {
		if err := compare.Score(builds, all, stale, hints).WriteText(w); err != nil {
			return fmt.Errorf("compare: %w", err)
		}
	}
	return verdict(all, stale)
}

// readHints reads the hints file at path, empty without one.
func readHints(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	h, err := compare.ReadHints(path)
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	return h, nil
}

func compareLeagues(f *pins.File, datasets, league string) ([]compare.Build, error) {
	var builds []compare.Build
	for _, l := range leagues(f, league) {
		bs, err := compare.Run(".", datasets, f, game, l)
		if err != nil {
			return nil, fmt.Errorf("compare %s: %w", l, err)
		}
		builds = append(builds, bs...)
	}
	return builds, nil
}

// verdict fails when a mismatch is untriaged or a triage entry stale.
func verdict(ms []compare.Mismatch, stale []string) error {
	if len(stale) > 0 || slices.ContainsFunc(ms, func(m compare.Mismatch) bool { return m.Triage == "" }) {
		return errMismatch
	}
	return nil
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
