package pins_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/xaroth/pbt-conformance/pins"
)

func TestReadRepoPins(t *testing.T) {
	t.Parallel()
	f, err := pins.Read(os.DirFS(".."))
	if err != nil {
		t.Fatal(err)
	}
	if f.Games["poe1"].Current == "" {
		t.Error("no current poe1 league")
	}
}

func TestReadRejects(t *testing.T) {
	t.Parallel()
	const (
		commit = "f9f4f3b4ab6a3a37d2eb693265b5c73317ff42a6"
		id     = "51c9afc83cb9e1db75822ba2efdd7c3786e243fb24a439f4052be98ab80ade1c"
	)
	runtime := `"runtime": {"luajit": {"repo": "https://x", "commit": "` + commit + `"}, "luautf8": {"repo": "https://y", "commit": "` + commit + `"}}`
	league := func(tree, version, c string) string {
		return `{"pob": {"tag": "v1", "commit": "` + c + `"}, "tree": "` + tree + `", "dataset": {"version": "` + version + `", "id": "` + id + `"}}`
	}
	file := func(current, l string) string {
		return `{` + runtime + `, "games": {"poe1": {"current": "` + current + `", "leagues": {"3.28": ` + l + `}}}}`
	}
	testCases := []struct {
		name, data, want string
	}{
		{"valid", file("3.28", league("3_28", "3.28.0.16", commit)), ""},
		{"unknown field", `{"extra": 1}`, "unknown field"},
		{"no games", `{` + runtime + `}`, "no games"},
		{"current not pinned", file("3.29", league("3_28", "3.28.0.16", commit)), `current league "3.29" not pinned`},
		{"tree of another league", file("3.28", league("3_29", "3.28.0.16", commit)), `tree "3_29" is not the league's tree`},
		{"dataset of another league", file("3.28", league("3_28", "3.29.3.3", commit)), "is not a patch of the league"},
		{"short commit", file("3.28", league("3_28", "3.28.0.16", "f9f4f3b")), "want lowercase hex digits, 40 of them"},
		{"http repo", strings.Replace(file("3.28", league("3_28", "3.28.0.16", commit)), "https://x", "http://x", 1), "want an https URL"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := pins.Read(fstest.MapFS{pins.Name: {Data: []byte(testCase.data)}})
			switch {
			case testCase.want == "" && err != nil:
				t.Errorf("got %v, want nil", err)
			case testCase.want != "" && (!errors.Is(err, pins.ErrInvalid) || !strings.Contains(err.Error(), testCase.want)):
				t.Errorf("got %v, want %v containing %q", err, pins.ErrInvalid, testCase.want)
			}
		})
	}
}
