package compare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// MetaFile is the name of a build's metadata file in its directory. A build without one is
// untagged.
const MetaFile = "build.json"

// ErrMeta marks a malformed metadata file.
var ErrMeta = errors.New("invalid build metadata")

// Kind is what a build's main skill is.
type Kind string

// Kinds of build.
const (
	// Untagged is the kind of a build without a metadata file.
	Untagged Kind = "untagged"
	// Smoke is the kind of a smoke-test build, such as a level 1 start: it has no kind of its
	// own and counts towards none.
	Smoke        Kind = "smoke"
	KindSpell    Kind = "spell"
	KindAttack   Kind = "attack"
	KindDoT      Kind = "damage-over-time"
	KindMinion   Kind = "minion"
	KindTotem    Kind = "totem"
	KindTrapMine Kind = "trap-mine"
	KindParty    Kind = "party"
)

// Kinds returns the kinds a metadata file may give, in report order.
func Kinds() []Kind {
	return []Kind{KindSpell, KindAttack, KindDoT, KindMinion, KindTotem, KindTrapMine, KindParty}
}

// Content is something a build holds that the scoreboard shows apart.
type Content string

// Contents a metadata file may list.
const (
	ClusterJewel  Content = "cluster-jewel"
	TimelessJewel Content = "timeless-jewel"
	Mercenary     Content = "mercenary"
	// TreeVariant is a Ruthless or alternate tree.
	TreeVariant Content = "tree-variant"
	// ItemSkill is a skill granted by an item, a graft included.
	ItemSkill Content = "item-skill"
)

// Contents returns the contents a metadata file may list, in report order.
func Contents() []Content {
	return []Content{ClusterJewel, TimelessJewel, Mercenary, TreeVariant, ItemSkill}
}

// Meta is a build's metadata.
type Meta struct {
	Kind     Kind
	Contains []Content
	// Origin is where the build was taken from, if recorded.
	Origin *Origin
}

// Origin is where a build was taken from: the character's page and the snapshot of its
// league that was read.
type Origin struct {
	URL string `json:"url"`
	// League is the league's name, Snapshot the version of poe.ninja's data.
	League   string `json:"league"`
	Snapshot string `json:"snapshot"`
}

// MetaJSON is the metadata file: a kind with contents, or smoke alone, and where the build
// came from.
type MetaJSON struct {
	Kind     Kind      `json:"kind,omitempty"`
	Smoke    bool      `json:"smoke,omitempty"`
	Contains []Content `json:"contains,omitempty"`
	Origin   *Origin   `json:"origin,omitempty"`
}

// ReadMeta reads the metadata file in dir; without one the build is untagged, and with
// "smoke" set its kind is Smoke.
func ReadMeta(dir string) (Meta, error) {
	b, err := os.ReadFile(filepath.Join(dir, MetaFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Meta{Kind: Untagged}, nil
	}
	if err != nil {
		return Meta{}, fmt.Errorf("read meta: %w", err)
	}
	return parseMeta(b)
}

func parseMeta(b []byte) (Meta, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m MetaJSON
	if err := dec.Decode(&m); err != nil {
		return Meta{}, fmt.Errorf("%s: %w: %w", MetaFile, ErrMeta, err)
	}
	if m.Smoke {
		if m.Kind != "" || len(m.Contains) > 0 {
			return Meta{}, fmt.Errorf("%s: %w: smoke with a kind or contents", MetaFile, ErrMeta)
		}
		return Meta{Kind: Smoke}, nil
	}
	if !slices.Contains(Kinds(), m.Kind) {
		return Meta{}, fmt.Errorf("%s: %w: kind %q", MetaFile, ErrMeta, m.Kind)
	}
	for i, c := range m.Contains {
		if !slices.Contains(Contents(), c) || slices.Contains(m.Contains[:i], c) {
			return Meta{}, fmt.Errorf("%s: %w: contains %q unknown or repeated", MetaFile, ErrMeta, c)
		}
	}
	return Meta{Kind: m.Kind, Contains: m.Contains, Origin: m.Origin}, nil
}
