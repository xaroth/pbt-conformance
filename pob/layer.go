package pob

import (
	"bytes"
	"cmp"
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

// layer is what a derived file keeps of the full build.
type layer int

const (
	layerTreeOnly layer = iota
	layerGear
)

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
	b, err := derive(full, layerGear)
	if err != nil {
		return nil, fmt.Errorf("pob: gear: %w", err)
	}
	return b, nil
}

// TreeOnly derives a build's tree-only layer from its full PoB XML: the build and its tree with
// no jewels in its sockets. Every other top-level element is dropped and the bandit and pantheon
// choices on <Build> are reset to None, as in Gear.
func TreeOnly(full []byte) ([]byte, error) {
	b, err := derive(full, layerTreeOnly)
	if err != nil {
		return nil, fmt.Errorf("pob: tree only: %w", err)
	}
	return b, nil
}

func derive(full []byte, l layer) ([]byte, error) {
	edits, err := layerEdits(full, l)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
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

// layerWalk collects the edits that turn a full XML into one of its layers.
type layerWalk struct {
	layer  layer
	d      *xml.Decoder
	full   []byte
	edits  []edit
	path   []string
	items  map[string]edit
	flasks []string
	root   bool
}

func layerEdits(full []byte, l layer) ([]edit, error) {
	w := &layerWalk{layer: l, d: xml.NewDecoder(bytes.NewReader(full)), full: full, items: map[string]edit{}}
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
func (w *layerWalk) open(t xml.StartElement, start int64) error {
	var (
		skipped bool
		err     error
	)
	switch {
	case len(w.path) == 0:
		w.root = t.Name.Local == "PathOfBuilding"
	case len(w.path) == 1:
		skipped, err = w.openTop(t, start)
	default:
		skipped, err = w.openIn(w.path[1], t, start)
	}
	if err != nil || skipped {
		return err
	}
	w.path = append(w.path, t.Name.Local)
	return nil
}

// openIn handles an element inside the kept top-level element top.
func (w *layerWalk) openIn(top string, t xml.StartElement, start int64) (bool, error) {
	switch top {
	case "Items":
		return w.openItems(t, start)
	case "Tree":
		if w.layer == layerTreeOnly && t.Name.Local == "Socket" {
			return true, w.drop(start)
		}
	}
	return false, nil
}

// openTop resets the choices on <Build> and drops every top-level element the layer does not
// keep, so PoB uses its defaults for them.
func (w *layerWalk) openTop(t xml.StartElement, start int64) (bool, error) {
	switch name := t.Name.Local; {
	case name == "Build":
		tag := w.full[start:w.d.InputOffset()]
		w.edits = append(w.edits, edit{start, w.d.InputOffset(), choiceAttr.ReplaceAll(tag, []byte(`$1="None"`))})
		return false, nil
	case name == "Tree",
		name == "Items" && w.layer >= layerGear:
		return false, nil
	}
	return true, w.drop(start)
}

// openItems drops flask slots and notes every item; the flasks' items are dropped once all
// slots are read.
func (w *layerWalk) openItems(t xml.StartElement, start int64) (bool, error) {
	switch {
	case t.Name.Local == "Slot" && strings.HasPrefix(attr(t, "name"), "Flask "):
		w.flasks = append(w.flasks, attr(t, "itemId"))
		return true, w.drop(start)
	case t.Name.Local == "Item":
		e, err := w.skip(start)
		w.items[attr(t, "id")] = e
		return true, err
	}
	return false, nil
}

// drop removes the element whose start tag began at start.
func (w *layerWalk) drop(start int64) error {
	e, err := w.skip(start)
	w.edits = append(w.edits, e)
	return err
}

// skip reads past the element whose start tag began at start and returns its removal.
func (w *layerWalk) skip(start int64) (edit, error) {
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
