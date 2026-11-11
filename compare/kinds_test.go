package compare_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pob"
)

// TestBuildKinds checks that every build of the current league has a metadata file and that its
// kind follows from its recording.
func TestBuildKinds(t *testing.T) {
	t.Parallel()
	dirs, err := pob.BuildDirs("..", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no 3.29 builds")
	}
	for _, dir := range dirs {
		m, err := compare.ReadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
		if err != nil {
			t.Fatal(err)
		}
		var v pob.Values
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if want := compare.DetectKind(v.Full); m.Kind != want {
			t.Errorf("%s: kind %s, want %s", filepath.Base(dir), m.Kind, want)
		}
	}
}

// TestBuildCounts checks that the current league holds the total of counted builds, each of a
// kind with a target, at least each kind's target and at least each content's quota.
func TestBuildCounts(t *testing.T) {
	t.Parallel()
	dirs, err := pob.BuildDirs("..", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	targets := compare.Targets()
	kinds := map[compare.Kind]int{}
	contents := map[compare.Content]int{}
	total := 0
	for _, dir := range dirs {
		m, err := compare.ReadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind == compare.Smoke {
			continue
		}
		if _, ok := targets[m.Kind]; !ok {
			t.Errorf("%s: kind %s has no target", filepath.Base(dir), m.Kind)
		}
		total++
		kinds[m.Kind]++
		for _, c := range m.Contains {
			contents[c]++
		}
	}
	if total != compare.Total() {
		t.Errorf("counted builds: got %d, want %d", total, compare.Total())
	}
	for k, want := range targets {
		if kinds[k] < want {
			t.Errorf("kind %s: got %d, want at least %d", k, kinds[k], want)
		}
	}
	for c, want := range compare.Quotas() {
		if contents[c] < want {
			t.Errorf("content %s: got %d, want at least %d", c, contents[c], want)
		}
	}
}
