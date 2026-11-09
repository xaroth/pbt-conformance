package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/xaroth/pbt-conformance/compare"
)

func TestReport(t *testing.T) {
	t.Parallel()
	ms := []compare.Mismatch{
		{League: "3.29", Build: "05-x", Output: "player.life", Ours: 1150, PoB: 1166, PoBFull: 2400, HasPoB: true, Triage: "D1"},
		{League: "3.29", Build: "05-x", Output: "player.mana", Ours: 47},
	}
	var out strings.Builder
	err := report(&out, ms, []string{"P9"})
	if !errors.Is(err, errMismatch) {
		t.Errorf("got %v, want %v", err, errMismatch)
	}
	for _, want := range []string{"player.life", "1166", "D1", "UNTRIAGED", "stale triage entry: P9", "2 mismatches, 1 untriaged, 1 stale"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
	if err := report(&out, ms[:1], nil); err != nil {
		t.Errorf("all triaged: got %v, want nil", err)
	}
}
