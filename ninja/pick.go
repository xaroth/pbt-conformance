package ninja

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/xaroth/pbt-conformance/compare"
	"github.com/xaroth/pbt-conformance/pob"
	"github.com/xaroth/pbt-conformance/runtime"
)

// ErrTree marks a poe.ninja league on another passive tree than the build league's.
var ErrTree = errors.New("league is on another tree")

// Source is where candidates come from.
type Source struct {
	// Overview is poe.ninja's name for the league whose characters are read, such as
	// "allflame".
	Overview string
	// Variant, if set, names a league on a Ruthless or alternate tree of the same version,
	// such as "allflamer".
	Variant string
	// Skills is how many of the most popular main skills are searched and Mercenaries how many
	// of the most popular mercenary classes; PerSkill is how many characters of different
	// ascendancies each search gives. VariantRows is how many of the variant league's
	// characters of different ascendancies are read.
	Skills, Mercenaries, PerSkill, VariantRows int
}

// Lead is a character a search listed.
type Lead struct {
	Snapshot Snapshot
	Row      Row
}

// Leads lists the characters to read, most popular first: for each of the most popular main
// skills of the league, the first characters of different ascendancies poe.ninja lists for it;
// the same for each of the most popular mercenary classes; then the variant league's first
// characters of different ascendancies. league is the build league, such as "3.29", whose
// passive tree the poe.ninja leagues must be on.
func Leads(ctx context.Context, a *API, src Source, league string) ([]Lead, error) {
	snap, err := snapshotOn(ctx, a, src.Overview, league, false)
	if err != nil {
		return nil, err
	}
	all, err := a.Search(ctx, snap, nil)
	if err != nil {
		return nil, err
	}
	ls := leadSet{seen: map[[2]string]bool{}}
	for _, sk := range all.Skills[:min(src.Skills, len(all.Skills))] {
		if err := ls.search(ctx, a, snap, url.Values{"skills": {sk.Name}}, src.PerSkill); err != nil {
			return nil, err
		}
	}
	for _, m := range all.Mercenaries[:min(src.Mercenaries, len(all.Mercenaries))] {
		if err := ls.search(ctx, a, snap, url.Values{"mercenaryclass": {m.Name}}, src.PerSkill); err != nil {
			return nil, err
		}
	}
	if src.Variant == "" {
		return ls.leads, nil
	}
	vsnap, err := snapshotOn(ctx, a, src.Variant, league, true)
	if err != nil {
		return nil, err
	}
	if err := ls.search(ctx, a, vsnap, nil, src.VariantRows); err != nil {
		return nil, err
	}
	return ls.leads, nil
}

// leadSet gathers leads, each character once.
type leadSet struct {
	leads []Lead
	seen  map[[2]string]bool
}

// search adds the first n characters of different ascendancies that a search lists.
func (ls *leadSet) search(ctx context.Context, a *API, s Snapshot, q url.Values, n int) error {
	r, err := a.Search(ctx, s, q)
	if err != nil {
		return err
	}
	classes := map[string]bool{}
	for _, row := range r.Rows {
		key := [2]string{row.Account, row.Name}
		if len(classes) == n {
			break
		}
		if classes[row.Class] || ls.seen[key] {
			continue
		}
		classes[row.Class] = true
		ls.seen[key] = true
		ls.leads = append(ls.leads, Lead{Snapshot: s, Row: row})
	}
	return nil
}

// snapshotOn returns the overview's snapshot and checks it is on the league's tree, or with
// variant on a variant of it.
func snapshotOn(ctx context.Context, a *API, overview, league string, variant bool) (Snapshot, error) {
	s, err := a.Snapshot(ctx, overview)
	if err != nil {
		return Snapshot{}, err
	}
	tree := "PassiveTree-" + league
	if ok := s.PassiveTree == tree; variant {
		ok = strings.HasPrefix(s.PassiveTree, tree+"-")
		if !ok {
			return Snapshot{}, fmt.Errorf("ninja: %s on %s: %w", overview, s.PassiveTree, ErrTree)
		}
	} else if !ok {
		return Snapshot{}, fmt.Errorf("ninja: %s on %s: %w", overview, s.PassiveTree, ErrTree)
	}
	return s, nil
}

// Fetch reads each lead's character and its PoB build.
func Fetch(ctx context.Context, a *API, leads []Lead) ([]Candidate, error) {
	cands := make([]Candidate, 0, len(leads))
	for _, l := range leads {
		ch, err := a.Character(ctx, l.Snapshot, l.Row.Account, l.Row.Name)
		if err != nil {
			return nil, err
		}
		c := Candidate{Char: ch, Snapshot: l.Snapshot}
		c.XML, c.Err = ch.XML()
		cands = append(cands, c)
	}
	return cands, nil
}

// classified is PoB's result for a build XML, or the error it raised, as cached.
type classified struct {
	Result pob.Result `json:"result"`
	Error  string     `json:"error,omitempty"`
}

// Classify runs each candidate's build through PoB at p and sets its kind, main skill,
// ascendancy and contents, or its error when PoB fails on it. Results are cached under dir by
// the XML's hash, so a rerun runs PoB on new builds only.
func Classify(ctx context.Context, p runtime.Paths, dir string, cands []Candidate) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	res := make([]*classified, len(cands))
	var todo []int
	for i, c := range cands {
		if c.Err != nil {
			continue
		}
		r, ok, err := readClassified(classFile(dir, c.XML))
		if err != nil {
			return err
		}
		if !ok {
			todo = append(todo, i)
			continue
		}
		res[i] = &r
	}
	if err := runClassify(ctx, p, dir, cands, todo, res); err != nil {
		return err
	}
	for i := range cands {
		if res[i] != nil {
			setBuild(&cands[i], *res[i])
		}
	}
	return nil
}

// classFile is the cache file of a build XML's result, without extension.
func classFile(dir string, x []byte) string {
	sum := sha256.Sum256(x)
	return filepath.Join(dir, hex.EncodeToString(sum[:]))
}

// readClassified reads a cached result and reports whether there is one.
func readClassified(base string) (classified, bool, error) {
	var r classified
	b, err := os.ReadFile(base + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("ninja: %w", err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, false, fmt.Errorf("ninja: %s: %w", base, err)
	}
	return r, true, nil
}

// runClassify runs the candidates todo through PoB in one process and caches and sets their
// results in res.
func runClassify(ctx context.Context, p runtime.Paths, dir string, cands []Candidate, todo []int, res []*classified) error {
	if len(todo) == 0 {
		return nil
	}
	files := make([]string, len(todo))
	for k, i := range todo {
		files[k] = classFile(dir, cands[i].XML) + ".xml"
		if err := os.WriteFile(files[k], cands[i].XML, filePerm); err != nil {
			return fmt.Errorf("ninja: %w", err)
		}
	}
	out, errs, err := pob.RunEach(ctx, p, files)
	if err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	for k, i := range todo {
		res[i] = &classified{Result: out[k]}
		if errs[k] != nil {
			res[i] = &classified{Error: errs[k].Error()}
		}
		b, err := json.Marshal(res[i])
		if err != nil {
			return fmt.Errorf("ninja: %w", err)
		}
		if err := os.WriteFile(strings.TrimSuffix(files[k], ".xml")+".json", b, filePerm); err != nil {
			return fmt.Errorf("ninja: %w", err)
		}
	}
	return nil
}

// Reasons a candidate is not picked.
var (
	// ErrLoad marks a build PoB fails to load.
	ErrLoad = errors.New("PoB cannot load the build")
	// ErrNoDamage marks a build whose main skill PoB gives no damage. Such builds are mostly
	// ones PoB cannot read from poe.ninja's export, such as a spectre build without its
	// spectre, which the export does not name.
	ErrNoDamage = errors.New("PoB gives the main skill no damage")
)

func setBuild(c *Candidate, r classified) {
	if r.Error != "" {
		c.Err = fmt.Errorf("%w: %s", ErrLoad, r.Error)
		return
	}
	c.Kind = compare.DetectKind(r.Result)
	if c.Kind == compare.KindOther {
		c.Err = ErrNoDamage
		return
	}
	if r.Result.MainSkill != nil {
		c.Skill = r.Result.MainSkill.Name
	}
	c.Ascendancy = c.Char.Class
	contains, err := compare.DetectContents(c.XML)
	if err != nil {
		c.Err = err
		return
	}
	if c.Char.HasMercenary() {
		contains = append(contains, compare.Mercenary)
	}
	c.Contains = sortContents(contains)
}

func sortContents(cs []compare.Content) []compare.Content {
	order := compare.Contents()
	slices.SortFunc(cs, func(x, y compare.Content) int { return slices.Index(order, x) - slices.Index(order, y) })
	return cs
}

// Existing returns the counted builds of league under root: all but smoke tests and untagged
// builds, with the main skill PoB recorded for each.
func Existing(root, league string) ([]Build, error) {
	dirs, err := pob.BuildDirs(root, league)
	if err != nil {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	var out []Build
	for _, d := range dirs {
		m, err := compare.ReadMeta(d)
		if err != nil {
			return nil, fmt.Errorf("ninja: %s: %w", d, err)
		}
		if m.Kind == compare.Smoke || m.Kind == compare.Untagged {
			continue
		}
		b := Build{Kind: m.Kind, Contains: m.Contains}
		if b.Skill, b.Ascendancy, err = recorded(d); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// recorded returns a build's main skill from its values file and ascendancy from its XML.
func recorded(dir string) (string, string, error) {
	var skill string
	b, err := os.ReadFile(filepath.Join(dir, pob.ValuesFile))
	if err != nil {
		return "", "", fmt.Errorf("ninja: %w", err)
	}
	var v pob.Values
	if err := json.Unmarshal(b, &v); err != nil {
		return "", "", fmt.Errorf("ninja: %s: %w", dir, err)
	}
	if v.Full.MainSkill != nil {
		skill = v.Full.MainSkill.Name
	}
	x, err := os.ReadFile(filepath.Join(dir, pob.FullXML))
	if err != nil {
		return "", "", fmt.Errorf("ninja: %w", err)
	}
	var f buildClass
	if err := xml.Unmarshal(x, &f); err != nil {
		return "", "", fmt.Errorf("ninja: %s: %w", dir, err)
	}
	asc := f.Build.Ascend
	if asc == "" || asc == "None" {
		asc = f.Build.Class
	}
	return skill, asc, nil
}

// buildClass is a PoB file's class and ascendancy.
type buildClass struct {
	Build struct {
		Class  string `xml:"className,attr"`
		Ascend string `xml:"ascendClassName,attr"`
	} `xml:"Build"`
}

// Write adds the picked builds to league under root after its existing builds, numbered on,
// each with its PoB XML and its metadata file, and returns their directories. The derived XMLs
// and values are made by recording the league with pob.Record.
func Write(root, league string, picked []Candidate) ([]string, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "builds", league, "*"))
	if err != nil {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	next := 1
	for _, d := range dirs {
		num, _, _ := strings.Cut(filepath.Base(d), "-")
		if n, err := strconv.Atoi(num); err == nil {
			next = max(next, n+1)
		}
	}
	var out []string
	for _, c := range picked {
		dir := filepath.Join(root, "builds", league, DirName(next, c))
		next++
		if err := writeBuild(dir, c); err != nil {
			return nil, err
		}
		out = append(out, dir)
	}
	return out, nil
}

func writeBuild(dir string, c Candidate) error {
	if err := os.Mkdir(dir, dirPerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	m := compare.MetaJSON{Kind: c.Kind, Contains: c.Contains, Origin: &compare.Origin{
		URL: c.Char.URL(c.Snapshot), League: c.Snapshot.Name, Snapshot: c.Snapshot.Version,
	}}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, compare.MetaFile), append(b, '\n'), filePerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, pob.FullXML), c.XML, filePerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	return nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// DirName is a picked build's directory name: <NN>-<class>-L<level>-<ascendancy>-<skill>, the
// ascendancy left out when the build has none.
func DirName(n int, c Candidate) string {
	parts := []string{fmt.Sprintf("%02d", n), slug(c.Char.BaseClass), "L" + strconv.Itoa(c.Char.Level)}
	if !strings.EqualFold(c.Ascendancy, c.Char.BaseClass) {
		parts = append(parts, slug(c.Ascendancy))
	}
	return strings.Join(append(parts, slug(c.Skill)), "-")
}

// WriteTable writes the picked builds as a table: kind, main skill, ascendancy, level,
// contents and the character's page.
func WriteTable(w io.Writer, picked []Candidate) error {
	for _, c := range picked {
		cs := make([]string, len(c.Contains))
		for i, x := range c.Contains {
			cs[i] = string(x)
		}
		contains := strings.Join(cs, ",")
		if contains == "" {
			contains = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", c.Kind, c.Skill, c.Ascendancy, c.Char.Level, contains,
			c.Char.URL(c.Snapshot)); err != nil {
			return fmt.Errorf("ninja: %w", err)
		}
	}
	return nil
}
