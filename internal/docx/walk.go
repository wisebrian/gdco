package docx

import (
	"io"
	"strings"

	"github.com/wisebrian/gdco/internal/ooxml"
)

// frameKind says where a finished paragraph goes.
type frameKind uint8

const (
	frameStory   frameKind = iota // the body, a header, a note: lines of their own
	frameCell                     // a table cell: joined into its row's line
	frameTextBox                  // a text box: lines shown after the paragraph that anchors it
)

type frame struct {
	kind    frameKind
	lines   []Line          // story and text box frames
	parts   []string        // cell frames: the texts that make up the cell
	paraN   int             // paragraphs ended in this frame
	box     int             // the text box number, for text box frames
	pending strings.Builder // text of paragraphs whose mark is gone, waiting for the next paragraph
	waiting []Line          // their text boxes
}

// para is a paragraph being read.
type para struct {
	segs   []strings.Builder // one per line: w:br and w:cr start a new one
	pages  []int             // the page each segment starts on
	list   listProps
	markIn bool // the paragraph mark is a tracked insertion
	markDe bool // or a tracked deletion
	boxes  []Line
}

func (p *para) seg() *strings.Builder {
	if len(p.segs) == 0 {
		p.segs = append(p.segs, strings.Builder{})
		p.pages = append(p.pages, 0)
	}
	return &p.segs[len(p.segs)-1]
}

func (p *para) newline() {
	p.seg()
	p.segs = append(p.segs, strings.Builder{})
	p.pages = append(p.pages, 0)
}

// handler handles one child element of a container and reads past its end.
// Containers that only wrap content, such as revisions and content
// controls, pass their children to the handler of the level they are in.
type handler func(s ooxml.StartElement) error

// walker reads one story: the body, one header or footer, one note or one
// comment. It descends the XML recursively; the decoder's depth limit
// bounds the recursion.
type walker struct {
	r      *reader
	d      *ooxml.Decoder
	frames []*frame
	cur    *para
	fields []bool // open fields, innermost last: true once past the instruction
	tables int
	boxes  int
	counts counters
	body   bool // the body is the only story with pages
}

func (r *reader) newWalker(d *ooxml.Decoder, body bool) *walker {
	return &walker{r: r, d: d, frames: []*frame{{kind: frameStory}}, counts: counters{}, body: body}
}

func (w *walker) frame() *frame { return w.frames[len(w.frames)-1] }

// children passes each child element of the element just started to f,
// and reads past its end.
func (w *walker) children(f handler) error {
	for {
		t, err := w.d.Token()
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		if err != nil {
			return err
		}
		switch t := t.(type) {
		case ooxml.StartElement:
			if err := f(t); err != nil {
				return err
			}
		case ooxml.EndElement:
			return nil
		}
	}
}

func isW(s ooxml.StartElement) bool { return ooxml.Space(s.Name.Space) == ooxml.NSW }

// removed reports whether a revision element's content is absent from the
// version being compared: deletions once changes are accepted, insertions
// in the original.
func (w *walker) removed(local string) bool {
	w.r.revisions = true
	if w.r.o.Original {
		return local == "ins" || local == "moveTo"
	}
	return local == "del" || local == "moveFrom"
}

// wrapper handles the elements that may wrap content at any level:
// revisions, content controls, custom XML and alternate content. It
// reports false for any other element.
func (w *walker) wrapper(s ooxml.StartElement, f handler) (bool, error) {
	switch ooxml.Space(s.Name.Space) {
	case ooxml.NSW:
		switch s.Name.Local {
		case "ins", "del", "moveFrom", "moveTo":
			if w.removed(s.Name.Local) {
				return true, w.d.Skip()
			}
			return true, w.children(f)
		case "sdt":
			return true, w.children(func(k ooxml.StartElement) error {
				if isW(k) && k.Name.Local == "sdtContent" {
					return w.children(f)
				}
				return w.d.Skip() // sdtPr and sdtEndPr hold properties, not content
			})
		case "customXml", "smartTag", "dir", "bdo":
			return true, w.children(func(k ooxml.StartElement) error {
				if isW(k) && (k.Name.Local == "customXmlPr" || k.Name.Local == "smartTagPr") {
					return w.d.Skip()
				}
				return f(k)
			})
		}
	case ooxml.NSMC:
		if s.Name.Local == "AlternateContent" {
			return true, w.alternate(f)
		}
	}
	return false, nil
}

// alternate reads exactly one branch of mc:AlternateContent: the first
// Choice, or the Fallback when there is none. Reading both would show a
// text box twice, once from its DrawingML and once from its VML copy.
func (w *walker) alternate(f handler) error {
	chosen := false
	return w.children(func(k ooxml.StartElement) error {
		if ooxml.Space(k.Name.Space) == ooxml.NSMC && (k.Name.Local == "Choice" || k.Name.Local == "Fallback") && !chosen {
			chosen = true
			return w.children(f)
		}
		return w.d.Skip()
	})
}

// block handles the content of a story, a cell or a text box.
func (w *walker) block(s ooxml.StartElement) error {
	if ok, err := w.wrapper(s, w.block); ok {
		return err
	}
	if !isW(s) {
		return w.d.Skip()
	}
	switch s.Name.Local {
	case "p":
		return w.paragraph()
	case "tbl":
		return w.table()
	case "sectPr":
		return w.sectPr()
	case "altChunk":
		w.r.skip("embedded documents (altChunk)")
		return w.d.Skip()
	}
	return w.d.Skip()
}

func (w *walker) paragraph() error {
	p := &para{}
	outer := w.cur
	w.cur = p
	err := w.children(func(s ooxml.StartElement) error {
		if isW(s) && s.Name.Local == "pPr" {
			return w.pPr(p)
		}
		return w.inline(s)
	})
	w.cur = outer
	if err != nil {
		return err
	}
	w.finish(w.frame(), p)
	return nil
}

// pPr reads what matters of the paragraph properties: the style and list
// for numbering, the section properties for headers, and whether the
// paragraph mark itself is a tracked change.
func (w *walker) pPr(p *para) error {
	return w.children(func(s ooxml.StartElement) error {
		if !isW(s) {
			return w.d.Skip()
		}
		switch s.Name.Local {
		case "pStyle":
			p.list.style = ooxml.Attr(s, ooxml.NSW, "val")
		case "numPr":
			return w.children(func(k ooxml.StartElement) error {
				if isW(k) {
					switch k.Name.Local {
					case "numId":
						p.list.numID, p.list.hasNum = ooxml.Attr(k, ooxml.NSW, "val"), true
					case "ilvl":
						p.list.ilvl, p.list.hasLevel = atoi(ooxml.Attr(k, ooxml.NSW, "val"), 0), true
					}
				}
				return w.d.Skip()
			})
		case "rPr":
			return w.children(func(k ooxml.StartElement) error {
				if isW(k) {
					switch k.Name.Local {
					case "ins", "moveTo":
						w.r.revisions = true
						p.markIn = true
					case "del", "moveFrom":
						w.r.revisions = true
						p.markDe = true
					}
				}
				return w.d.Skip()
			})
		case "sectPr":
			return w.sectPr()
		}
		return w.d.Skip()
	})
}

// inline handles the content of a paragraph and of the containers inside
// it, such as hyperlinks and simple fields, whose children are runs.
func (w *walker) inline(s ooxml.StartElement) error {
	if ok, err := w.wrapper(s, w.inline); ok {
		return err
	}
	switch ooxml.Space(s.Name.Space) {
	case ooxml.NSW:
		switch s.Name.Local {
		case "r":
			return w.children(w.run)
		case "hyperlink", "fldSimple", "rubyBase":
			// A simple field holds its result, which is what is shown.
			return w.children(w.inline)
		case "ruby":
			return w.children(func(k ooxml.StartElement) error {
				if isW(k) && k.Name.Local == "rubyBase" {
					return w.children(w.inline)
				}
				return w.d.Skip() // the phonetic guide and its properties
			})
		case "commentReference":
			w.r.reference(&w.r.commentRefs, ooxml.Attr(s, ooxml.NSW, "id"))
		}
		return w.d.Skip()
	case ooxml.NSM:
		return w.math(s)
	}
	return w.d.Skip()
}

// run handles the content of a run.
func (w *walker) run(s ooxml.StartElement) error {
	if ok, err := w.wrapper(s, w.run); ok {
		return err
	}
	if !isW(s) {
		return w.graphic(s)
	}
	p := w.cur
	switch s.Name.Local {
	case "t":
		return w.text(s)
	case "delText":
		if w.r.o.Original {
			return w.text(s)
		}
	case "tab", "ptab":
		w.emit("\t")
	case "br":
		if w.live() {
			p.newline()
		}
	case "cr":
		if w.live() {
			p.newline()
		}
	case "noBreakHyphen":
		w.emit("-")
	case "sym":
		w.emit(symbol(ooxml.Attr(s, ooxml.NSW, "font"), ooxml.Attr(s, ooxml.NSW, "char")))
	case "fldChar":
		w.field(ooxml.Attr(s, ooxml.NSW, "fldCharType"))
	case "footnoteReference":
		w.r.reference(&w.r.footRefs, ooxml.Attr(s, ooxml.NSW, "id"))
		w.emit("[footnote]")
	case "endnoteReference":
		w.r.reference(&w.r.endRefs, ooxml.Attr(s, ooxml.NSW, "id"))
		w.emit("[endnote]")
	case "commentReference":
		w.r.reference(&w.r.commentRefs, ooxml.Attr(s, ooxml.NSW, "id"))
	case "lastRenderedPageBreak":
		if w.body {
			w.r.rendered++
		}
	case "drawing", "pict", "object":
		return w.children(w.graphic)
	}
	return w.d.Skip()
}

// live reports whether text belongs to what is shown: not inside a field's
// instruction, only in its result.
func (w *walker) live() bool {
	if w.cur == nil {
		return false
	}
	for _, inResult := range w.fields {
		if !inResult {
			return false
		}
	}
	return true
}

func (w *walker) emit(s string) {
	if s == "" || !w.live() {
		return
	}
	p := w.cur
	b := p.seg()
	if b.Len() == 0 && w.body {
		p.pages[len(p.pages)-1] = w.r.rendered + 1
	}
	b.WriteString(s)
}

// text reads a w:t. Without xml:space="preserve", Word drops the white
// space at either end, so it is dropped here too.
func (w *walker) text(s ooxml.StartElement) error {
	t, err := w.d.Text()
	if err != nil {
		return err
	}
	if ooxml.Attr(s, ooxml.NSXML, "space") != "preserve" {
		t = strings.Trim(t, " \t\r\n")
	}
	if strings.ContainsAny(t, "\r\n") {
		t = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(t)
	}
	w.emit(t)
	return nil
}

// field follows complex fields, which may nest and span paragraphs: what
// lies between begin and separate is the instruction, hidden; what lies
// between separate and end is the result, shown.
func (w *walker) field(kind string) {
	switch kind {
	case "begin":
		if len(w.fields) < 64 {
			w.fields = append(w.fields, false)
		}
	case "separate":
		if n := len(w.fields); n > 0 {
			w.fields[n-1] = true
		}
	case "end":
		if n := len(w.fields); n > 0 {
			w.fields = w.fields[:n-1]
		}
	}
}

// math collects the text of an equation.
func (w *walker) math(s ooxml.StartElement) error {
	if ooxml.Space(s.Name.Space) == ooxml.NSM && s.Name.Local == "t" {
		t, err := w.d.Text()
		if err == nil {
			w.emit(t)
		}
		return err
	}
	return w.children(w.math)
}

// graphic looks through a drawing for the text boxes in it. Charts and
// diagrams keep their text in parts of their own, which are not read.
func (w *walker) graphic(s ooxml.StartElement) error {
	if ooxml.Space(s.Name.Space) == ooxml.NSMC && s.Name.Local == "AlternateContent" {
		return w.alternate(w.graphic)
	}
	if isW(s) && s.Name.Local == "txbxContent" {
		return w.textBox()
	}
	if s.Name.Local == "graphicData" {
		switch uri := ooxml.Attr(s, ooxml.NSOther, "uri"); {
		case strings.HasSuffix(uri, "/chart"), strings.HasSuffix(uri, "/chartex"):
			w.r.skip("charts")
		case strings.HasSuffix(uri, "/diagram"):
			w.r.skip("SmartArt diagrams")
		}
	}
	return w.children(w.graphic)
}

func (w *walker) textBox() error {
	w.boxes++
	f := &frame{kind: frameTextBox, box: w.boxes}
	w.frames = append(w.frames, f)
	outer := w.cur
	w.cur = nil
	err := w.children(w.block)
	w.cur = outer
	w.frames = w.frames[:len(w.frames)-1]
	if err != nil {
		return err
	}
	w.flush(f)
	if outer != nil {
		outer.boxes = append(outer.boxes, f.lines...)
	} else {
		w.place(f.lines)
	}
	return nil
}

// sectPr records the headers and footers a section refers to, in order.
func (w *walker) sectPr() error {
	if !w.body {
		return w.d.Skip()
	}
	w.r.sections++
	return w.children(func(s ooxml.StartElement) error {
		if isW(s) && (s.Name.Local == "headerReference" || s.Name.Local == "footerReference") {
			w.r.storyRefs = append(w.r.storyRefs, storyRef{
				header:  s.Name.Local == "headerReference",
				id:      ooxml.Attr(s, ooxml.NSR, "id"),
				kind:    ooxml.Attr(s, ooxml.NSW, "type"),
				section: w.r.sections,
			})
		}
		return w.d.Skip()
	})
}

// finish turns a finished paragraph into lines of frame f, or into text
// of the cell f is.
func (w *walker) finish(f *frame, p *para) {
	segs := make([]string, len(p.segs))
	for i := range p.segs {
		segs[i] = p.segs[i].String()
	}
	if len(segs) == 0 {
		segs = []string{""}
		p.pages = []int{0}
	}
	// A paragraph whose mark is gone in this version runs on into the next
	// one, as it does in Word once the change is accepted (or, for an
	// inserted mark, before it was made).
	if (p.markDe && !w.r.o.Original) || (p.markIn && w.r.o.Original) {
		for i, s := range segs {
			if i > 0 {
				f.pending.WriteByte(' ')
			}
			f.pending.WriteString(s)
		}
		f.waiting = append(f.waiting, p.boxes...)
		return
	}
	if f.pending.Len() > 0 {
		segs[0] = f.pending.String() + segs[0]
		f.pending.Reset()
	}
	boxes := append(f.waiting, p.boxes...)
	f.waiting = nil
	marker, shape := w.r.num.marker(w.counts, p.list)

	if f.kind == frameCell {
		text := strings.Join(segs, " ")
		if marker != "" {
			text = marker + " " + text
		}
		if text != "" {
			f.parts = append(f.parts, text)
		}
		for _, b := range boxes {
			if b.Text != "" {
				f.parts = append(f.parts, b.Text)
			}
		}
		return
	}
	f.paraN++
	for i, s := range segs {
		l := Line{Text: s, Loc: Loc{Paragraph: f.paraN, TextBox: f.box}}
		if w.body {
			l.Loc.Page = p.pages[i]
		}
		if i == 0 {
			l.Marker, l.Shape = marker, shape
		}
		f.lines = append(f.lines, l)
	}
	f.lines = append(f.lines, boxes...)
}

// flush ends a frame: text still waiting for a next paragraph that never
// came is a paragraph of its own.
func (w *walker) flush(f *frame) {
	if f.pending.Len() == 0 && len(f.waiting) == 0 {
		return
	}
	p := &para{}
	p.seg().WriteString(f.pending.String())
	p.boxes = f.waiting
	f.pending.Reset()
	f.waiting = nil
	w.finish(f, p)
}

// place adds lines to the innermost frame, as text if it is a cell.
func (w *walker) place(lines []Line) {
	f := w.frame()
	if f.kind == frameCell {
		for _, l := range lines {
			if l.Text != "" {
				f.parts = append(f.parts, l.Text)
			}
		}
		return
	}
	f.lines = append(f.lines, lines...)
}

type table struct {
	nested  bool
	ordinal int
	rows    int
}

type row struct {
	cells []string
	skip  bool
}

// table reads a table as one line per row, its cells separated by " | ",
// so that an inserted row is one inserted line. A table inside a cell is
// folded into that cell's text.
func (w *walker) table() error {
	t := &table{nested: w.frame().kind == frameCell}
	if !t.nested {
		w.tables++
		t.ordinal = w.tables
	}
	var rows handler
	rows = func(s ooxml.StartElement) error {
		if ok, err := w.wrapper(s, rows); ok {
			return err
		}
		if isW(s) && s.Name.Local == "tr" {
			return w.row(t)
		}
		return w.d.Skip()
	}
	return w.children(rows)
}

func (w *walker) row(t *table) error {
	r := &row{}
	page := w.r.rendered + 1
	var cells handler
	cells = func(s ooxml.StartElement) error {
		if r.skip {
			return w.d.Skip()
		}
		if ok, err := w.wrapper(s, cells); ok {
			return err
		}
		if !isW(s) {
			return w.d.Skip()
		}
		switch s.Name.Local {
		case "trPr":
			return w.children(func(k ooxml.StartElement) error {
				if isW(k) && (k.Name.Local == "ins" || k.Name.Local == "del") && w.removed(k.Name.Local) {
					r.skip = true
				}
				return w.d.Skip()
			})
		case "tc":
			return w.cell(r)
		}
		return w.d.Skip()
	}
	if err := w.children(cells); err != nil {
		return err
	}
	if r.skip {
		return nil
	}
	text := strings.Join(r.cells, " | ")
	if t.nested {
		if text != "" {
			f := w.frame()
			f.parts = append(f.parts, text)
		}
		return nil
	}
	t.rows++
	f := w.frame()
	l := Line{Text: text, Loc: Loc{Table: t.ordinal, Row: t.rows, TextBox: f.box}}
	if w.body {
		l.Loc.Page = page
	}
	f.lines = append(f.lines, l)
	return nil
}

func (w *walker) cell(r *row) error {
	f := &frame{kind: frameCell}
	w.frames = append(w.frames, f)
	outer := w.cur
	w.cur = nil
	err := w.children(func(s ooxml.StartElement) error {
		if isW(s) && s.Name.Local == "tcPr" {
			return w.d.Skip()
		}
		return w.block(s)
	})
	w.cur = outer
	w.flush(f)
	w.frames = w.frames[:len(w.frames)-1]
	r.cells = append(r.cells, strings.Join(f.parts, " "))
	return err
}
