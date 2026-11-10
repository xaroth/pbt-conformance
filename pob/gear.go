package pob

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// choiceAttr matches the <Build> choices that are config in PoB, reset to PoB's default as in
// the tree-only files.
var choiceAttr = regexp.MustCompile(`(\s(?:bandit|pantheonMajorGod|pantheonMinorGod))="[^"]*"`)

var errNoRoot = errors.New("no PathOfBuilding element")

// edit replaces full[start:end] with repl.
type edit struct {
	start, end int64
	repl       []byte
}

// Gear derives a build's gear layer from its full PoB XML: the build, its tree with the jewels
// in its sockets, and its items without flasks. Skills, config and every other top-level element
// are dropped, and the bandit and pantheon choices on <Build> are reset to None. The rest is
// kept byte for byte.
func Gear(full []byte) ([]byte, error) {
	edits, err := gearEdits(full)
	if err != nil {
		return nil, fmt.Errorf("pob: gear: %w", err)
	}
	slices.SortFunc(edits, func(a, b edit) int { return int(a.start - b.start) })
	var out bytes.Buffer
	pos := int64(0)
	for _, e := range edits {
		out.Write(full[pos:e.start])
		out.Write(e.repl)
		pos = e.end
	}
	out.Write(full[pos:])
	return out.Bytes(), nil
}

// gearWalk collects the edits that turn a full XML into its gear layer.
type gearWalk struct {
	d      *xml.Decoder
	full   []byte
	edits  []edit
	path   []string
	items  map[string]edit
	flasks []string
	root   bool
}

func gearEdits(full []byte) ([]edit, error) {
	w := &gearWalk{d: xml.NewDecoder(bytes.NewReader(full)), full: full, items: map[string]edit{}}
	for {
		start := w.d.InputOffset()
		tok, err := w.d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := w.open(t, start); err != nil {
				return nil, err
			}
		case xml.EndElement:
			w.path = w.path[:len(w.path)-1]
		}
	}
	if !w.root {
		return nil, errNoRoot
	}
	for _, id := range w.flasks {
		if e, ok := w.items[id]; ok {
			w.edits = append(w.edits, e)
			delete(w.items, id)
		}
	}
	return w.edits, nil
}

// open handles the element whose start tag t began at start: it either skips the element or
// descends into it.
func (w *gearWalk) open(t xml.StartElement, start int64) error {
	var (
		skipped bool
		err     error
	)
	switch {
	case len(w.path) == 0:
		w.root = t.Name.Local == "PathOfBuilding"
	case len(w.path) == 1:
		skipped, err = w.openTop(t, start)
	case slices.Contains(w.path, "Items"):
		skipped, err = w.openItems(t, start)
	}
	if err != nil || skipped {
		return err
	}
	w.path = append(w.path, t.Name.Local)
	return nil
}

// openTop resets the choices on <Build> and drops every top-level element a gear file does not
// keep, so PoB uses its defaults for them.
func (w *gearWalk) openTop(t xml.StartElement, start int64) (bool, error) {
	switch t.Name.Local {
	case "Build":
		tag := w.full[start:w.d.InputOffset()]
		w.edits = append(w.edits, edit{start, w.d.InputOffset(), choiceAttr.ReplaceAll(tag, []byte(`$1="None"`))})
		return false, nil
	case "Tree", "Items":
		return false, nil
	}
	e, err := w.skip(start)
	w.edits = append(w.edits, e)
	return true, err
}

// openItems drops flask slots and notes every item; the flasks' items are dropped once all
// slots are read.
func (w *gearWalk) openItems(t xml.StartElement, start int64) (bool, error) {
	switch {
	case t.Name.Local == "Slot" && strings.HasPrefix(attr(t, "name"), "Flask "):
		e, err := w.skip(start)
		w.edits = append(w.edits, e)
		w.flasks = append(w.flasks, attr(t, "itemId"))
		return true, err
	case t.Name.Local == "Item":
		e, err := w.skip(start)
		w.items[attr(t, "id")] = e
		return true, err
	}
	return false, nil
}

// skip reads past the element whose start tag began at start and returns its removal.
func (w *gearWalk) skip(start int64) (edit, error) {
	if err := w.d.Skip(); err != nil {
		return edit{}, fmt.Errorf("reading xml: %w", err)
	}
	return wholeLine(w.full, start, w.d.InputOffset()), nil
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// wholeLine widens the removal of full[start:end] to the whole line when the element stands
// alone on it, so no blank line is left behind.
func wholeLine(full []byte, start, end int64) edit {
	for start > 0 && (full[start-1] == ' ' || full[start-1] == '\t') {
		start--
	}
	for end < int64(len(full)) && (full[end] == ' ' || full[end] == '\t' || full[end] == '\r') {
		end++
	}
	if end < int64(len(full)) && full[end] == '\n' {
		end++
	}
	return edit{start: start, end: end}
}
