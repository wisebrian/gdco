package gdco

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/align"
	"github.com/wisebrian/gdco/internal/myers"
	"github.com/wisebrian/gdco/internal/textenc"
	"github.com/wisebrian/gdco/internal/tokens"
)

// Compare aligns a and b part by part: body with body, headers with
// headers, sheets by name. A nil document is an empty text.
func Compare(a, b *Document, o *Options) (r *Result, err error) {
	defer guard(&err)
	opts, err := o.resolve()
	if err != nil {
		return nil, err
	}
	if a == nil {
		a = TextDocument("", "")
	}
	if b == nil {
		b = TextDocument("", "")
	}
	for _, d := range []*Document{a, b} {
		if n := d.lineCount(); n > opts.Limits.MaxLines {
			return nil, fmt.Errorf("%w: %s has %d lines, more than %d", ErrLimit, display(d.name), n, opts.Limits.MaxLines)
		}
	}
	cfg := opts.alignConfig()
	if (a.format == FormatXlsx) != (b.format == FormatXlsx) {
		return nil, fmt.Errorf("%w: a workbook can only be compared with another workbook", ErrMismatch)
	}
	pa, pb := a.parts, b.parts
	var extra []Note
	if (a.format == FormatDocx) != (b.format == FormatDocx) {
		// Plain text has nothing to match headers or notes against: they
		// would all show as removed, which says nothing about the text.
		pa, pb = bodies(pa), bodies(pb)
		if len(pa) < len(a.parts) || len(pb) < len(b.parts) {
			extra = append(extra, Note{Code: "body-only", Message: "Only the main text was compared: plain text has no headers, footers, notes or comments to compare with."})
		}
	}
	c := &comparer{o: &opts, norm: opts.norm(), cfg: cfg, al: align.New(cfg)}
	r = &Result{A: sideOf(a), B: sideOf(b), Parts: []PartDiff{}}
	for _, pp := range matchParts(pa, pb) {
		pd := c.part(pp[0], pp[1])
		r.Parts = append(r.Parts, pd)
		r.Stats.add(pd.Stats)
	}
	r.Approximate = c.approx
	r.Equal = true
	for _, p := range r.Parts {
		if p.Status != StatusSame {
			r.Equal = false
		}
	}
	r.Notes = append(notes(a, b, r), extra...)
	return r, nil
}

// CompareText is Compare for two strings, named "a" and "b".
func CompareText(a, b string, o *Options) (*Result, error) {
	return Compare(TextDocument("a", a), TextDocument("b", b), o)
}

// CompareFiles reads both files, then compares them.
func CompareFiles(a, b File, o *Options) (*Result, error) {
	da, err := Read(a, o)
	if err != nil {
		return nil, err
	}
	db, err := Read(b, o)
	if err != nil {
		return nil, err
	}
	return Compare(da, db, o)
}

func sideOf(d *Document) Side {
	return Side{Name: d.name, Format: d.format, Encoding: d.encoding, LineEnding: d.ending, Lines: d.lineCount()}
}

// matchParts pairs the parts of two documents by kind and name, in an order
// that follows both documents; a part found on one side only comes paired
// with nil.
func matchParts(pa, pb []*part) [][2]*part {
	ids := map[string]int32{}
	key := func(p *part) int32 {
		k := string(p.kind) + "\x00" + p.name
		id, ok := ids[k]
		if !ok {
			id = int32(len(ids))
			ids[k] = id
		}
		return id
	}
	ka, kb := make([]int32, len(pa)), make([]int32, len(pb))
	for i, p := range pa {
		ka[i] = key(p)
	}
	for i, p := range pb {
		kb[i] = key(p)
	}
	var out [][2]*part
	for _, ch := range myers.Diff(ka, kb).Chunks() {
		if ch.Match {
			for k := 0; k < ch.NA; k++ {
				out = append(out, [2]*part{pa[ch.A+k], pb[ch.B+k]})
			}
			continue
		}
		for k := 0; k < ch.NA; k++ {
			out = append(out, [2]*part{pa[ch.A+k], nil})
		}
		for k := 0; k < ch.NB; k++ {
			out = append(out, [2]*part{nil, pb[ch.B+k]})
		}
	}
	return out
}

type comparer struct {
	o      *Options
	norm   tokens.Norm
	cfg    align.Config
	al     *align.Aligner
	approx bool
}

// row is a row of the full alignment of a part, before context is chosen.
// Two unrelated texts give hundreds of thousands of them, so it is kept to
// 32 bytes, with the spans of modified lines behind a pointer.
type row struct {
	a, b    int32 // line indexes; -1 on the side without a line
	wd, wi  int32 // words removed and added
	sp      *rowSpans
	op      rowOp
	ignored bool
}

type rowSpans struct{ a, b []align.Span }

// rowOp is Op in one byte.
type rowOp uint8

const (
	rEqual rowOp = iota
	rInsert
	rDelete
	rModify
)

func (o rowOp) public() Op { return [...]Op{OpEqual, OpInsert, OpDelete, OpModify}[o] }

func (c *comparer) part(pa, pb *part) PartDiff {
	status := StatusSame
	switch {
	case pa == nil:
		pa, status = &part{kind: pb.kind, name: pb.name}, StatusAdded
	case pb == nil:
		pb, status = &part{kind: pa.kind, name: pa.name}, StatusRemoved
	}
	ia, ib := c.keys(pa, pb)
	script := myers.Diff(ia, ib)
	if script.Approximate {
		c.approx = true
	}
	rows := c.rows(pa, pb, script)
	if c.o.IgnoreBlankLines {
		markBlank(rows, pa, pb)
	}
	pd := PartDiff{Kind: pa.kind, Name: pa.name, Status: status}
	for _, r := range rows {
		if r.ignored {
			continue
		}
		switch r.op {
		case rInsert:
			pd.Stats.LinesAdded++
		case rDelete:
			pd.Stats.LinesRemoved++
		case rModify:
			pd.Stats.LinesChanged++
		}
		pd.Stats.WordsAdded += int(r.wi)
		pd.Stats.WordsRemoved += int(r.wd)
	}
	if status == StatusSame {
		// Ignored rows, blank lines under IgnoreBlankLines, leave the part
		// the same.
		for _, r := range rows {
			if r.op != rEqual && !r.ignored {
				pd.Status = StatusChanged
				break
			}
		}
	}
	if pa.kind == PartSheet {
		pd.Cells = c.cellChanges(rows, pa, pb)
		pd.Stats.CellsChanged = len(pd.Cells)
	}
	pd.Hunks = c.hunks(rows, pa, pb)
	return pd
}

// keys interns the comparison key of every line, so the diff compares
// integers. Without options the key is the text itself and costs nothing.
func (c *comparer) keys(pa, pb *part) ([]int32, []int32) {
	shapes := hasShapes(pa) || hasShapes(pb)
	eofBit := !c.norm.IgnoresSpace()
	ids := make(map[string]int32, len(pa.lines)+len(pb.lines))
	var buf []byte
	key := func(l *line) int32 {
		var id int32
		if c.norm.Plain() && !shapes {
			var ok bool
			if id, ok = ids[l.text]; !ok {
				id = int32(len(ids))
				ids[l.text] = id
			}
		} else {
			buf = buf[:0]
			if shapes {
				// The marker's shape comes first, length-prefixed so that no
				// text can be mistaken for a shape.
				shape := ""
				if l.x != nil {
					shape = l.x.shape
				}
				buf = strconv.AppendInt(buf, int64(len(shape)), 10)
				buf = append(buf, ':')
				buf = append(buf, shape...)
			}
			buf = c.norm.LineKey(buf, l.text)
			var ok bool
			if id, ok = ids[string(buf)]; !ok {
				id = int32(len(ids))
				ids[string(buf)] = id
			}
		}
		// A last line without a newline differs from the same text with
		// one, as it does for diff and git; keeping that out of the key
		// text means no line can be confused with it.
		id *= 2
		if eofBit && l.eol == textenc.None {
			id++
		}
		return id
	}
	ka, kb := make([]int32, len(pa.lines)), make([]int32, len(pb.lines))
	for i := range pa.lines {
		ka[i] = key(&pa.lines[i])
	}
	for i := range pb.lines {
		kb[i] = key(&pb.lines[i])
	}
	return ka, kb
}

func hasShapes(p *part) bool {
	for i := range p.lines {
		if x := p.lines[i].x; x != nil && x.shape != "" {
			return true
		}
	}
	return false
}

func (c *comparer) rows(pa, pb *part, s myers.Script) []row {
	var rows []row
	for _, ch := range s.Chunks() {
		if ch.Match {
			for k := 0; k < ch.NA; k++ {
				rows = append(rows, row{op: rEqual, a: int32(ch.A + k), b: int32(ch.B + k)})
			}
			continue
		}
		rows = c.block(rows, pa, pb, ch)
	}
	return rows
}

// block turns one change block into rows, pairing lines that are versions
// of each other.
func (c *comparer) block(rows []row, pa, pb *part, ch myers.Chunk) []row {
	del := func(i int) row {
		return row{op: rDelete, a: int32(i), b: -1, wd: int32(align.Words(pa.lines[i].text, c.cfg))}
	}
	ins := func(j int) row {
		return row{op: rInsert, a: -1, b: int32(j), wi: int32(align.Words(pb.lines[j].text, c.cfg))}
	}
	if c.o.Granularity == ByLine || ch.NA == 0 || ch.NB == 0 {
		for i := ch.A; i < ch.A+ch.NA; i++ {
			rows = append(rows, del(i))
		}
		for j := ch.B; j < ch.B+ch.NB; j++ {
			rows = append(rows, ins(j))
		}
		return rows
	}
	ta, tb := make([]string, ch.NA), make([]string, ch.NB)
	for k := range ta {
		ta[k] = pa.lines[ch.A+k].text
	}
	for k := range tb {
		tb[k] = pb.lines[ch.B+k].text
	}
	for _, st := range align.Pair(ta, tb, c.cfg) {
		switch {
		case st.A >= 0 && st.B >= 0:
			sa, sb, wd, wi := c.al.Diff(ta[st.A], tb[st.B])
			rows = append(rows, row{op: rModify, a: int32(ch.A + st.A), b: int32(ch.B + st.B), sp: &rowSpans{sa, sb}, wd: int32(wd), wi: int32(wi)})
		case st.A >= 0:
			rows = append(rows, del(ch.A+st.A))
		default:
			rows = append(rows, ins(ch.B+st.B))
		}
	}
	return rows
}

// markBlank implements IgnoreBlankLines: a change region whose lines are
// all blank is marked ignored, as diff -B ignores it.
func markBlank(rows []row, pa, pb *part) {
	blank := func(s string) bool { return strings.TrimFunc(s, tokens.IsSpace) == "" }
	for i := 0; i < len(rows); {
		if rows[i].op == rEqual {
			i++
			continue
		}
		j, all := i, true
		for ; j < len(rows) && rows[j].op != rEqual; j++ {
			r := rows[j]
			if r.a >= 0 && !blank(pa.lines[r.a].text) || r.b >= 0 && !blank(pb.lines[r.b].text) {
				all = false
			}
		}
		if all {
			for k := i; k < j; k++ {
				rows[k].ignored = true
			}
		}
		i = j
	}
}

// hunks cuts the full alignment into hunks with the requested context,
// merging two changes whose contexts would touch, as diff -u does.
func (c *comparer) hunks(rows []row, pa, pb *part) []Hunk {
	changed := func(i int) bool { return rows[i].op != rEqual && !rows[i].ignored }
	var ranges [][2]int
	if ctx := c.o.Context; ctx < 0 {
		if len(rows) > 0 {
			ranges = append(ranges, [2]int{0, len(rows)})
		}
	} else {
		for i := 0; i < len(rows); {
			for i < len(rows) && !changed(i) {
				i++
			}
			if i == len(rows) {
				break
			}
			start, end := max(0, i-ctx), i+1
			for {
				k := end
				for k < len(rows) && !changed(k) {
					k++
				}
				if k == len(rows) || k-end > 2*ctx {
					break
				}
				end = k + 1
			}
			stop := min(len(rows), end+ctx)
			ranges = append(ranges, [2]int{start, stop})
			i = stop
		}
	}

	hunks := make([]Hunk, 0, len(ranges))
	next, beforeA, beforeB := 0, 0, 0 // lines of each side before row next
	for _, rg := range ranges {
		for ; next < rg[0]; next++ {
			if rows[next].a >= 0 {
				beforeA++
			}
			if rows[next].b >= 0 {
				beforeB++
			}
		}
		h := Hunk{A: Range{Start: beforeA + 1}, B: Range{Start: beforeB + 1}, Rows: make([]Row, 0, rg[1]-rg[0])}
		// One allocation for all the lines of the hunk; it never grows, so
		// pointers into it stay valid.
		slab := make([]Line, 0, 2*(rg[1]-rg[0]))
		for _, r := range rows[rg[0]:rg[1]] {
			out := Row{Op: r.op.public(), Ignored: r.ignored}
			var sa, sb []align.Span
			if r.sp != nil {
				sa, sb = r.sp.a, r.sp.b
			}
			if r.a >= 0 {
				slab = append(slab, lineOf(pa, r.a, sa))
				out.A = &slab[len(slab)-1]
				h.A.Count++
			}
			if r.b >= 0 {
				slab = append(slab, lineOf(pb, r.b, sb))
				out.B = &slab[len(slab)-1]
				h.B.Count++
			}
			h.Rows = append(h.Rows, out)
		}
		hunks = append(hunks, h)
		next, beforeA, beforeB = rg[1], beforeA+h.A.Count, beforeB+h.B.Count
	}
	return hunks
}

func lineOf(p *part, i int32, sp []align.Span) Line {
	l := &p.lines[i]
	out := Line{N: int(i) + 1, Text: l.text, NoNewline: l.eol == textenc.None, eol: l.eol}
	if l.x != nil {
		out.Marker = l.x.marker
		out.Loc = l.x.loc
	}
	if len(sp) > 0 {
		out.Spans = make([]Span, len(sp))
		for k, s := range sp {
			op := OpEqual
			switch s.Op {
			case align.Delete:
				op = OpDelete
			case align.Insert:
				op = OpInsert
			}
			out.Spans[k] = Span{Op: op, Text: s.Text}
		}
	}
	return out
}

// notes explains what the line changes do not show.
func notes(a, b *Document, r *Result) []Note {
	var out []Note
	if ea, eb := a.encoding, b.encoding; ea != "" && eb != "" && ea != eb {
		out = append(out, Note{Code: "encoding", Message: fmt.Sprintf(
			"A is encoded as %s and B as %s; the texts were compared after decoding.", encodingName(ea), encodingName(eb))})
	}
	if la, lb := a.ending, b.ending; la != "" && lb != "" && la != lb {
		out = append(out, Note{Code: "line-endings", Message: fmt.Sprintf(
			"A uses %s and B uses %s; line endings are not compared.", endingName(la), endingName(lb))})
	}
	if r.Approximate {
		out = append(out, Note{Code: "approximate", Message: "The inputs differ so much that the comparison stopped refining it: every change shown is real, but there may be a shorter way to describe them."})
	}
	for _, n := range a.notes {
		n.Side = "a"
		out = append(out, n)
	}
	for _, n := range b.notes {
		n.Side = "b"
		out = append(out, n)
	}
	return out
}

func encodingName(e Encoding) string {
	switch e {
	case EncodingUTF8BOM:
		return "UTF-8 with a byte order mark"
	case EncodingUTF16LE:
		return "UTF-16 (little-endian)"
	case EncodingUTF16BE:
		return "UTF-16 (big-endian)"
	case EncodingWindows1252:
		return "Windows-1252"
	}
	return "UTF-8"
}

func endingName(e LineEnding) string {
	switch e {
	case LineEndingCRLF:
		return "Windows line endings (CRLF)"
	case LineEndingCR:
		return "classic Mac line endings (CR)"
	case LineEndingMixed:
		return "mixed line endings"
	}
	return "Unix line endings (LF)"
}

func bodies(ps []*part) []*part {
	var out []*part
	for _, p := range ps {
		if p.kind == PartBody {
			out = append(out, p)
		}
	}
	return out
}
