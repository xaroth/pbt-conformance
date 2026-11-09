// Command pbtc runs pbt-conformance.
//
//	pbtc setup --cache <dir>
//
// setup builds the PoB runtime every pinned release needs (LuaJIT, lua-utf8, PoB checkouts)
// into the cache directory, skipping what is already built. It needs git, make and cc.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/xaroth/pbt-conformance/pins"
	"github.com/xaroth/pbt-conformance/runtime"
)

var errUsage = errors.New("usage: pbtc setup --cache <dir>")

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
	if len(args) == 0 || args[0] != "setup" {
		return errUsage
	}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	cache := fs.String("cache", "", "cache directory for the runtime")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	if *cache == "" {
		return errUsage
	}
	f, err := pins.Read(os.DirFS("."))
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	if err := runtime.Setup(ctx, *cache, f); err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	return nil
}
