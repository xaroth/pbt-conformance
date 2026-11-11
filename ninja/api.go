package ninja

import (
	"bytes"
	"cmp"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"strings"
)

// ErrNotFound marks a league or column poe.ninja does not list.
var ErrNotFound = errors.New("not found")

// API reads poe.ninja's builds API through a Client.
type API struct {
	Client *Client
	dicts  map[string][]string
}

// Snapshot is a league's builds as poe.ninja last indexed them; its version names the data a
// search reads. Overview names the league in poe.ninja's pages, APIName in its API.
type Snapshot struct {
	Overview    string `json:"url"`
	APIName     string `json:"snapshotName"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	PassiveTree string `json:"passiveTree"`
}

// Snapshot returns the experience-ranked snapshot of the league poe.ninja names overview, such
// as "allflame".
func (a *API) Snapshot(ctx context.Context, overview string) (Snapshot, error) {
	b, err := a.Client.Get(ctx, "/poe1/api/data/index-state", nil)
	if err != nil {
		return Snapshot{}, err
	}
	var st struct {
		Snapshots []Snapshot `json:"snapshotVersions"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return Snapshot{}, fmt.Errorf("ninja: index state: %w", err)
	}
	for _, s := range st.Snapshots {
		if s.Overview == overview && s.Type == "exp" {
			return s, nil
		}
	}
	return Snapshot{}, fmt.Errorf("ninja: league %q: %w", overview, ErrNotFound)
}

// Count is how many characters a value of a search dimension has.
type Count struct {
	Name  string
	Count int
}

// Row is one character a search lists.
type Row struct {
	Account, Name string
	// Class is the ascendancy, or the class without one.
	Class string
}

// Result is a search's answer: the characters it matches, the main skills and mercenary
// classes among them by popularity, and the first characters in poe.ninja's order.
type Result struct {
	Total       int
	Skills      []Count
	Mercenaries []Count
	Rows        []Row
}

// Search searches the snapshot's characters with the filters in q, such as "skills".
func (a *API) Search(ctx context.Context, s Snapshot, q url.Values) (Result, error) {
	q = cloneWith(q, "overview", s.APIName, "type", s.Type)
	b, err := a.Client.Get(ctx, "/poe1/api/builds/"+s.Version+"/search", q)
	if err != nil {
		return Result{}, err
	}
	raw, err := parseSearch(b)
	if err != nil {
		return Result{}, err
	}
	return a.resolve(ctx, raw)
}

// search is a search response as sent: dictionary indexes, not names.
type search struct {
	total int
	// dims maps a dimension to its dictionary and its counts by index.
	dims map[string]dimension
	// dicts maps a dictionary to its hash.
	dicts map[string]string
	// strs and idxs hold a column's values per row, as text or as dictionary indexes.
	strs    map[string][]string
	idxs    map[string][]uint64
	idxDict map[string]string
}

type dimension struct {
	dict   string
	counts map[uint64]int
}

// Field numbers of the search response.
const (
	resTotal  = 1
	resDim    = 2
	resDict   = 6
	resColumn = 12
	dimID     = 1
	dimDict   = 2
	dimCount  = 3
	cntIndex  = 1
	cntValue  = 2
	dictID    = 1
	dictHash  = 2
	colID     = 1
	colIdxs   = 6
	colStrs   = 7
	colDict   = 11
)

func parseSearch(b []byte) (search, error) {
	root, err := fields(b)
	if err != nil || len(root) != 1 || root[0].num != 1 {
		return search{}, fmt.Errorf("%w: search", ErrFormat)
	}
	fs, err := fields(root[0].bytes)
	if err != nil {
		return search{}, err
	}
	s := search{dims: map[string]dimension{}, dicts: map[string]string{}, strs: map[string][]string{},
		idxs: map[string][]uint64{}, idxDict: map[string]string{}}
	for _, f := range fs {
		if err := s.add(f); err != nil {
			return search{}, err
		}
	}
	return s, nil
}

func (s *search) add(f field) error {
	var err error
	switch f.num {
	case resTotal:
		s.total, err = toInt(f.value)
	case resDim:
		err = s.addDim(f.bytes)
	case resDict:
		err = s.addDict(f.bytes)
	case resColumn:
		err = s.addColumn(f.bytes)
	default:
	}
	return err
}

// toInt converts a count, which poe.ninja keeps far below the int32 range.
func toInt(v uint64) (int, error) {
	if v > math.MaxInt32 {
		return 0, fmt.Errorf("%w: count %d", ErrFormat, v)
	}
	return int(v), nil
}

func (s *search) addDim(b []byte) error {
	fs, err := fields(b)
	if err != nil {
		return err
	}
	var id string
	d := dimension{counts: map[uint64]int{}}
	for _, f := range fs {
		switch f.num {
		case dimID:
			id = string(f.bytes)
		case dimDict:
			d.dict = string(f.bytes)
		case dimCount:
			idx, n, err := count(f.bytes)
			if err != nil {
				return err
			}
			d.counts[idx] = n
		default:
		}
	}
	s.dims[id] = d
	return nil
}

// count reads a dimension's count: a dictionary index and its number of characters.
func count(b []byte) (uint64, int, error) {
	cs, err := fields(b)
	if err != nil {
		return 0, 0, err
	}
	var idx, n uint64
	for _, c := range cs {
		switch c.num {
		case cntIndex:
			idx = c.value
		case cntValue:
			n = c.value
		default:
		}
	}
	v, err := toInt(n)
	return idx, v, err
}

func (s *search) addDict(b []byte) error {
	fs, err := fields(b)
	if err != nil {
		return err
	}
	var id, hash string
	for _, f := range fs {
		switch f.num {
		case dictID:
			id = string(f.bytes)
		case dictHash:
			hash = string(f.bytes)
		default:
		}
	}
	s.dicts[id] = hash
	return nil
}

func (s *search) addColumn(b []byte) error {
	fs, err := fields(b)
	if err != nil {
		return err
	}
	var id string
	for _, f := range fs {
		switch f.num {
		case colID:
			id = string(f.bytes)
		case colStrs:
			s.strs[id] = append(s.strs[id], string(f.bytes))
		case colIdxs:
			if s.idxs[id], err = varints(f.bytes); err != nil {
				return err
			}
		case colDict:
			s.idxDict[id] = string(f.bytes)
		default:
		}
	}
	return nil
}

// Character is what poe.ninja holds of one character.
type Character struct {
	Account   string `json:"account"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	Class     string `json:"class"`
	BaseClass string `json:"baseClass"`
	// Export is the character as a Path of Building code.
	Export string `json:"pathOfBuildingExport"`
	// Mercenary is set when the character has a mercenary.
	Mercenary json.RawMessage `json:"mercenary"`
}

// Character returns a character of the snapshot.
func (a *API) Character(ctx context.Context, s Snapshot, account, name string) (Character, error) {
	q := cloneWith(nil, "account", account, "name", name, "overview", s.APIName, "type", s.Type)
	b, err := a.Client.Get(ctx, "/poe1/api/builds/"+s.Version+"/character", q)
	if err != nil {
		return Character{}, err
	}
	var c Character
	if err := json.Unmarshal(b, &c); err != nil {
		return Character{}, fmt.Errorf("ninja: character %s/%s: %w: %w", account, name, ErrFormat, err)
	}
	return c, nil
}

// HasMercenary reports whether poe.ninja shows a mercenary for the character.
func (c Character) HasMercenary() bool {
	return len(c.Mercenary) > 0 && !bytes.Equal(c.Mercenary, []byte("null"))
}

// XML decodes the character's Path of Building code into PoB's build XML.
func (c Character) XML() ([]byte, error) {
	z, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(c.Export), "="))
	if err != nil {
		return nil, fmt.Errorf("ninja: %s/%s export: %w", c.Account, c.Name, err)
	}
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil, fmt.Errorf("ninja: %s/%s export: %w", c.Account, c.Name, err)
	}
	defer r.Close()
	x, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("ninja: %s/%s export: %w", c.Account, c.Name, err)
	}
	return x, nil
}

// URL is the character's page on poe.ninja.
func (c Character) URL(s Snapshot) string {
	return BaseURL + "/poe1/builds/" + s.Overview + "/character/" + url.PathEscape(c.Account) + "/" + url.PathEscape(c.Name)
}

// cloneWith returns q with the key-value pairs kv set.
func cloneWith(q url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

// resolve names a search's skills and rows from its dictionaries.
func (a *API) resolve(ctx context.Context, s search) (Result, error) {
	r := Result{Total: s.total}
	var err error
	if r.Skills, err = a.counts(ctx, s, "skills"); err != nil {
		return Result{}, err
	}
	if r.Mercenaries, err = a.counts(ctx, s, "mercenaryclass"); err != nil {
		return Result{}, err
	}
	classes, err := a.dictionary(ctx, s.dicts[s.idxDict["class"]])
	if err != nil {
		return Result{}, err
	}
	accounts, chars, idxs := s.strs["account"], s.strs["name"], s.idxs["class"]
	if len(accounts) != len(chars) || len(idxs) != len(chars) {
		return Result{}, fmt.Errorf("%w: search rows", ErrFormat)
	}
	for i := range chars {
		if idxs[i] >= uint64(len(classes)) {
			return Result{}, fmt.Errorf("%w: class index %d", ErrFormat, idxs[i])
		}
		r.Rows = append(r.Rows, Row{Account: accounts[i], Name: chars[i], Class: classes[idxs[i]]})
	}
	return r, nil
}

// counts returns the counts of a search's dimension by name, most first.
func (a *API) counts(ctx context.Context, s search, dim string) ([]Count, error) {
	d, ok := s.dims[dim]
	if !ok {
		return nil, fmt.Errorf("ninja: search: %s %w", dim, ErrNotFound)
	}
	names, err := a.dictionary(ctx, s.dicts[d.dict])
	if err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(d.counts))
	for idx, n := range d.counts {
		if idx >= uint64(len(names)) {
			return nil, fmt.Errorf("%w: %s index %d", ErrFormat, dim, idx)
		}
		out = append(out, Count{Name: names[idx], Count: n})
	}
	slices.SortFunc(out, func(x, y Count) int {
		return cmp.Or(cmp.Compare(y.Count, x.Count), strings.Compare(x.Name, y.Name))
	})
	return out, nil
}

// dictionary returns the names of the dictionary with hash.
func (a *API) dictionary(ctx context.Context, hash string) ([]string, error) {
	if d, ok := a.dicts[hash]; ok {
		return d, nil
	}
	if hash == "" {
		return nil, fmt.Errorf("ninja: dictionary %w", ErrNotFound)
	}
	b, err := a.Client.Get(ctx, "/poe1/api/builds/dictionary/"+hash, nil)
	if err != nil {
		return nil, err
	}
	d, err := dictionary(b)
	if err != nil {
		return nil, err
	}
	if a.dicts == nil {
		a.dicts = map[string][]string{}
	}
	a.dicts[hash] = d
	return d, nil
}
