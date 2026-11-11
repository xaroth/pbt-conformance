package compare_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

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

// TestBuildCounts checks that the current league has its target number of counted builds of
// each kind and at least the quota of builds holding each content.
func TestBuildCounts(t *testing.T) {
	t.Parallel()
	dirs, err := pob.BuildDirs("..", "3.29")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[compare.Kind]int{}
	contents := map[compare.Content]int{}
	for _, dir := range dirs {
		m, err := compare.ReadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind == compare.Smoke {
			continue
		}
		kinds[m.Kind]++
		for _, c := range m.Contains {
			contents[c]++
		}
	}
	if diff := cmp.Diff(compare.Targets(), kinds); diff != "" {
		t.Errorf("counted builds per kind (-want +got):\n%s", diff)
	}
	for c, n := range compare.Quotas() {
		if contents[c] < n {
			t.Errorf("%d builds hold %s, want at least %d", contents[c], c, n)
		}
	}
}
