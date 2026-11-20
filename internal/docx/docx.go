// Package docx reads the text of a Word document in reading order, line by
// line, with where each line comes from: paragraph, table and row, text
// box, header or footer, footnote, endnote or comment.
//
// Paragraphs become lines and table rows become lines too, their cells
// joined with " | ", so that an inserted row is one inserted line. Text
// boxes follow the paragraph that anchors them. Tracked changes are read as
// accepted, or as rejected for the original version. List numbers are
// rendered from numbering.xml but kept apart from the text, so that a
// comparison can show them without comparing them.
//
// Runs are not merged by formatting here; comparing formatting later only
// needs the walker to keep run properties next to the text it emits.
package docx

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/ooxml"
)

// Options choose the version of the text and the parts to read.
type Options struct {
	Original                                        bool // tracked changes rejected rather than accepted
	Headers, Footers, Footnotes, Endnotes, Comments bool
	Depth                                           int // XML nesting limit
}

// Doc is the text of a document.
type Doc struct {
	Parts     []Part
	Revisions bool     // the document has tracked changes
	Skipped   []string // kinds of content that are in the document but not in the text
	Plain     bool     // some list numbers use formats shown as plain numbers
}

// Part is the body or one kind of optional part; all the headers of a
// document are one part, each header an item of it.
type Part struct {
	Kind  string // "body", "headers", "footers", "footnotes", "endnotes" or "comments"
	Lines []Line
}

// Line is a line of text with its list marker apart: Marker is what Word
// shows ("3.2."), Shape what is compared ("#1", a numbered paragraph at
// level 1).
type Line struct {
	Text, Marker, Shape string
	Loc                 Loc
}

// Loc locates a line, as numbers; zero is unknown or not applicable.
type Loc struct {
	Page, Item, TextBox, Paragraph, Table, Row int
	Label                                      string
}

// storyRef is a header or footer reference of a section.
type storyRef struct {
	header  bool
	id      string
	kind    string // "default", "first" or "even"
	section int
}

type reader struct {
	pkg       *ooxml.Package
	o         Options
	num       *numbering
	revisions bool
	skipped   []string
	rendered  int // w:lastRenderedPageBreak seen in the body
	sections  int
	storyRefs []storyRef
	footRefs  refs
	endRefs   refs
	// commentRefs is the order comments are anchored in, which is the order
	// Word lists them in.
	commentRefs refs
}

// refs lists ids in the order they first appear. The set beside the list
// keeps adding linear: a hostile document can hold a hundred thousand
// references.
type refs struct {
	order []string
	seen  map[string]bool
}

func (s *refs) add(id string) bool {
	if id == "" || s.seen[id] {
		return false
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[id] = true
	s.order = append(s.order, id)
	return true
}

func (r *reader) reference(list *refs, id string) { list.add(id) }

func (r *reader) skip(what string) {
	if !slices.Contains(r.skipped, what) {
		r.skipped = append(r.skipped, what)
	}
}

// Read reads the document whose main part is main.
func Read(pkg *ooxml.Package, main string, o Options) (*Doc, error) {
	r := &reader{pkg: pkg, o: o, num: newNumbering()}
	rels, err := pkg.Rels(main)
	if err != nil {
		return nil, err
	}
	byID := map[string]ooxml.Rel{}
	first := map[string]string{}
	for _, rel := range rels {
		if rel.External || !pkg.Has(rel.Target) {
			continue
		}
		byID[rel.ID] = rel
		if _, ok := first[rel.Type]; !ok {
			first[rel.Type] = rel.Target
		}
	}
	for typ, read := range map[string]func(*ooxml.Node){"styles": r.num.readStyles, "numbering": r.num.readNumbering} {
		if name := first[typ]; name != "" {
			b, err := pkg.Read(name)
			if err != nil {
				return nil, err
			}
			tree, err := ooxml.ParseTree(b, o.Depth)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			read(tree)
		}
	}

	doc := &Doc{}
	body, err := r.story(main, true, func(w *walker, root ooxml.StartElement) error {
		return w.children(func(s ooxml.StartElement) error {
			if isW(s) && s.Name.Local == "body" {
				return w.children(w.block)
			}
			return w.d.Skip()
		})
	})
	if err != nil {
		return nil, err
	}
	if r.rendered == 0 {
		// Without Word's record of its last layout there are no pages to
		// speak of: a break counted alone would number most pages wrong.
		for i := range body {
			body[i].Loc.Page = 0
		}
	}
	doc.Parts = append(doc.Parts, Part{Kind: "body", Lines: body})

	for _, header := range []bool{true, false} {
		if (header && !o.Headers) || (!header && !o.Footers) {
			continue
		}
		var lines []Line
		var seen refs
		item := 0
		for _, ref := range r.storyRefs {
			rel, ok := byID[ref.id]
			want := "footer"
			if header {
				want = "header"
			}
			if ref.header != header || !ok || rel.Type != want || !seen.add(ref.id) {
				continue
			}
			item++
			part, err := r.story(rel.Target, false, func(w *walker, root ooxml.StartElement) error {
				return w.children(w.block)
			})
			if err != nil {
				return nil, err
			}
			label := storyLabel(ref, r.sections)
			for i := range part {
				part[i].Loc.Item, part[i].Loc.Label = item, label
			}
			lines = append(lines, part...)
		}
		kind := "footers"
		if header {
			kind = "headers"
		}
		if len(lines) > 0 {
			doc.Parts = append(doc.Parts, Part{Kind: kind, Lines: lines})
		}
	}

	notes := []struct {
		on        bool
		rel, elem string
		kind      string
		refs      refs
	}{
		{o.Footnotes, "footnotes", "footnote", "footnotes", r.footRefs},
		{o.Endnotes, "endnotes", "endnote", "endnotes", r.endRefs},
		{o.Comments, "comments", "comment", "comments", r.commentRefs},
	}
	for _, n := range notes {
		name := first[n.rel]
		if !n.on || name == "" {
			continue
		}
		lines, err := r.notes(name, n.elem, n.refs)
		if err != nil {
			return nil, err
		}
		if len(lines) > 0 {
			doc.Parts = append(doc.Parts, Part{Kind: n.kind, Lines: lines})
		}
	}
	doc.Revisions, doc.Skipped, doc.Plain = r.revisions, r.skipped, r.num.plain
	return doc, nil
}

// storyLabel names a header or footer by the pages it is for.
func storyLabel(ref storyRef, sections int) string {
	var label string
	switch ref.kind {
	case "first":
		label = "first page"
	case "even":
		label = "even pages"
	}
	if sections > 1 {
		s := "section " + strconv.Itoa(ref.section)
		if label != "" {
			return s + ", " + label
		}
		return s
	}
	return label
}

// story reads one part with a walker of its own and returns its lines.
func (r *reader) story(name string, body bool, root func(w *walker, s ooxml.StartElement) error) ([]Line, error) {
	var w *walker
	err := r.parse(name, func(d *ooxml.Decoder, s ooxml.StartElement) error {
		w = r.newWalker(d, body)
		return root(w, s)
	})
	if err != nil || w == nil {
		return nil, err
	}
	f := w.frames[0]
	w.flush(f)
	return f.lines, nil
}

// parse reads a part up to its root element and hands the rest to f.
func (r *reader) parse(name string, f func(d *ooxml.Decoder, root ooxml.StartElement) error) error {
	b, err := r.pkg.Read(name)
	if err != nil {
		return err
	}
	d := ooxml.NewDecoder(b, r.o.Depth)
	for {
		t, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if s, ok := t.(ooxml.StartElement); ok {
			if err := f(d, s); err != nil {
				if err == io.ErrUnexpectedEOF {
					err = fmt.Errorf("%w: unexpected end of XML", ooxml.ErrMalformed)
				}
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		}
	}
}

// notes reads footnotes, endnotes or comments, each an item, in the order
// the body refers to them: that is the order a reader numbers them in, and
// a note the body no longer refers to is not part of the document.
// Comments not anchored anywhere still count, after the others.
func (r *reader) notes(name, elem string, order refs) ([]Line, error) {
	byID := map[string][]Line{}
	var inFile []string
	err := r.parse(name, func(d *ooxml.Decoder, root ooxml.StartElement) error {
		top := r.newWalker(d, false)
		return top.children(func(s ooxml.StartElement) error {
			if !isW(s) || s.Name.Local != elem {
				return d.Skip()
			}
			if t := ooxml.Attr(s, ooxml.NSW, "type"); t != "" && t != "normal" {
				return d.Skip() // separators and continuation notices
			}
			id := ooxml.Attr(s, ooxml.NSW, "id")
			w := r.newWalker(d, false)
			if err := w.children(w.block); err != nil {
				return err
			}
			f := w.frames[0]
			w.flush(f)
			if len(f.lines) > 0 {
				// The space after the note's own number mark, which is not read.
				f.lines[0].Text = strings.TrimLeft(f.lines[0].Text, " \t")
			}
			if author := ooxml.Attr(s, ooxml.NSW, "author"); elem == "comment" && author != "" {
				if len(f.lines) == 0 {
					f.lines = []Line{{Loc: Loc{Paragraph: 1}}}
				}
				f.lines[0].Text = author + ": " + f.lines[0].Text
			}
			if _, dup := byID[id]; !dup {
				inFile = append(inFile, id)
			}
			byID[id] = f.lines
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if elem == "comment" {
		for _, id := range inFile {
			order.add(id)
		}
	}
	ids := order.order
	var out []Line
	item := 0
	for _, id := range ids {
		lines, ok := byID[id]
		if !ok {
			continue
		}
		item++
		for _, l := range lines {
			l.Loc.Item = item
			out = append(out, l)
		}
	}
	return out, nil
}
