// Package imports_test checks pbt-engine's PoB importer on what the conformance builds lack.
// Each directory holds a PoB build file (pob.xml), what it must import to (expect.json: the
// build and the warnings by code and subject, or the error) and a README saying how the file
// was made from a conformance build.
package imports_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/xaroth/pbt-engine/build"
	"github.com/xaroth/pbt-engine/dataset"
	"github.com/xaroth/pbt-engine/pob"
)

type warning struct {
	Code    build.Code `json:"code"`
	Subject string     `json:"subject"`
}

type expect struct {
	Build    json.RawMessage `json:"build"`
	Warnings []warning       `json:"warnings"`
	Error    string          `json:"error"`
}

// errorsByName are the importer errors expect.json can name.
func errorsByName() map[string]error {
	return map[string]error{"poe2": pob.ErrPoE2, "not-pob": pob.ErrNotPoB, "tree-version": pob.ErrTreeVersion}
}

func TestImports(t *testing.T) {
	t.Parallel()
	datasets := os.Getenv("PBT_DATASETS")
	if datasets == "" {
		t.Skip("PBT_DATASETS not set")
	}
	reg, err := dataset.OpenRoot(os.DirFS(datasets))
	if reg == nil {
		t.Fatal(err)
	}
	dirs, err := filepath.Glob("*/pob.xml")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, path := range dirs {
		dir := filepath.Dir(path)
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			checkCase(t, reg, dir)
		})
	}
}

func checkCase(t *testing.T, reg *dataset.Registry, dir string) {
	t.Helper()
	want := readExpect(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, "pob.xml"))
	if err != nil {
		t.Fatal(err)
	}
	b, ws, err := pob.FromXML(data, reg)
	if want.Error != "" {
		if wantErr, ok := errorsByName()[want.Error]; !ok || !errors.Is(err, wantErr) {
			t.Errorf("got %v, want error %s", err, want.Error)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	wantBuild, err := build.Parse(want.Build)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantBuild, b, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("build (-want +got):\n%s", diff)
	}
	got := make([]warning, 0, len(ws))
	for _, w := range ws {
		got = append(got, warning{w.Code, w.Subject})
	}
	if diff := cmp.Diff(want.Warnings, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

func readExpect(t *testing.T, dir string) expect {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "expect.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e expect
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
