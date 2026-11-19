package gdco

import (
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/textenc"
)

// Result is the comparison, shaped for a UI that renders it side by side or
// inline. Its JSON encoding, with camelCase keys, is what the wasm package
// returns.
type Result struct {
	A           Side       `json:"a"`
	B           Side       `json:"b"`
	Equal       bool       `json:"equal"`       // nothing differs beyond what Options ignore
	Approximate bool       `json:"approximate"` // the cost budget was hit: the alignment is valid but may not be minimal
	Parts       []PartDiff `json:"parts"`
	Stats       Stats      `json:"stats"` // sums over Parts
	Notes       []Note     `json:"notes,omitempty"`
}

// Side describes an input as it was read, to explain differences that are
// not in the text: a byte order mark, CRLF against LF.
type Side struct {
	Name       string     `json:"name"`
	Format     Format     `json:"format"`
	Encoding   Encoding   `json:"encoding,omitempty"`   // text read from bytes only
	LineEnding LineEnding `json:"lineEnding,omitempty"` // text only
	Lines      int        `json:"lines"`
}

// PartDiff compares one part: the body, the headers, one sheet. Parts
// exist so that a change in a header is reported where readers look for
// it, not interleaved with the body.
type PartDiff struct {
	Kind   PartKind     `json:"kind"`
	Name   string       `json:"name,omitempty"` // the sheet name; empty for other parts
	Status PartStatus   `json:"status"`
	Hunks  []Hunk       `json:"hunks"`
	Cells  []CellChange `json:"cells,omitempty"` // sheets only
	Stats  Stats        `json:"stats"`
}

// Hunk is a group of rows with the ranges of its "@@ -a,b +c,d @@" header.
type Hunk struct {
	A    Range `json:"a"`
	B    Range `json:"b"`
	Rows []Row `json:"rows"`
}

// Range is a 1-based first line and a count of lines. When Count is 0,
// Start is the number the next line would have.
type Range struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

// Row is one line of a side-by-side view. A modified line is one row with
// both sides rather than a deletion and an insertion, so that the two
// versions can sit next to each other with the changed words marked.
type Row struct {
	Op      Op    `json:"op"`
	A       *Line `json:"a,omitempty"` // nil on the side where the line does not exist
	B       *Line `json:"b,omitempty"`
	Ignored bool  `json:"ignored,omitempty"` // a change that Options made insignificant
}

// Op is what happened to a row or to a span.
type Op string

// The operations. Rows use all four; spans use OpEqual, OpDelete on side A
// and OpInsert on side B.
const (
	OpEqual  Op = "equal"
	OpInsert Op = "insert"
	OpDelete Op = "delete"
	OpModify Op = "modify"
)

// Line is one side of a row. Marker is kept apart from Text so that a list
// number is shown but not compared: inserting clause 3 must not mark
// clauses 4 to 40 as changed.
type Line struct {
	N         int      `json:"n"` // 1-based line number within the part
	Text      string   `json:"text"`
	Marker    string   `json:"marker,omitempty"` // such as "3.2", "•" or "a)"
	Loc       Location `json:"loc,omitzero"`
	Spans     []Span   `json:"spans,omitempty"`     // OpModify rows only; their texts join to Text
	NoNewline bool     `json:"noNewline,omitempty"` // the file ends here without a newline

	eol textenc.EOL // the line's own terminator, which WriteUnified keeps
}

// Span is a piece of a modified line. It holds text rather than offsets
// because JavaScript strings count UTF-16 units and Go counts bytes.
type Span struct {
	Op   Op     `json:"op"`
	Text string `json:"text"`
}

// Location says where a line sits in its source as numbers rather than
// prose, so that a UI can phrase "Table 2, row 3" in its own language.
// Zero means unknown or not applicable.
type Location struct {
	Page      int    `json:"page,omitempty"` // from the caller, or from Word's last layout (approximate)
	Item      int    `json:"item,omitempty"` // which header, footer, note or comment within the part
	TextBox   int    `json:"textBox,omitempty"`
	Paragraph int    `json:"paragraph,omitempty"`
	Table     int    `json:"table,omitempty"`
	Row       int    `json:"row,omitempty"`   // table row, or the sheet's own row number
	Line      int    `json:"line,omitempty"`  // line within a segment
	Label     string `json:"label,omitempty"` // the segment's label, or "first page" for a header
}

// String is an English label for reports, such as "Page 3, line 12" or
// "Table 2, row 3". Item is left out because its meaning depends on the
// part: the HTML report names it ("Footnote 2").
func (l Location) String() string {
	var b []string
	switch {
	case l.Page > 0 && l.Label != "":
		b = append(b, "page "+l.Label)
	case l.Page > 0:
		b = append(b, "page "+strconv.Itoa(l.Page))
	case l.Label != "":
		b = append(b, l.Label)
	}
	add := func(name string, n int) {
		if n > 0 {
			b = append(b, name+" "+strconv.Itoa(n))
		}
	}
	add("text box", l.TextBox)
	add("table", l.Table)
	add("row", l.Row)
	add("paragraph", l.Paragraph)
	add("line", l.Line)
	s := strings.Join(b, ", ")
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		s = string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

// CellChange is one changed cell of two aligned rows. Both addresses are
// given because a row inserted above turns B7 into B8.
type CellChange struct {
	A   string `json:"a,omitempty"` // such as "B7"; empty when the cell is absent on that side
	B   string `json:"b,omitempty"`
	Old string `json:"old"`
	New string `json:"new"`
}

// Stats feed a summary. For a sheet, a line is a row.
type Stats struct {
	LinesAdded   int `json:"linesAdded"`
	LinesRemoved int `json:"linesRemoved"`
	LinesChanged int `json:"linesChanged"`
	WordsAdded   int `json:"wordsAdded"`
	WordsRemoved int `json:"wordsRemoved"`
	CellsChanged int `json:"cellsChanged"`
}

func (s *Stats) add(t Stats) {
	s.LinesAdded += t.LinesAdded
	s.LinesRemoved += t.LinesRemoved
	s.LinesChanged += t.LinesChanged
	s.WordsAdded += t.WordsAdded
	s.WordsRemoved += t.WordsRemoved
	s.CellsChanged += t.CellsChanged
}

// Note tells the reader something that is not a changed line. Code is
// stable, for a UI to translate; Message is English.
type Note struct {
	Code    string `json:"code"`           // "encoding", "line-endings", "approximate", ...
	Side    string `json:"side,omitempty"` // "a", "b", or empty for both
	Message string `json:"message"`
}

// Format is how a document was read. Like the other enumerations, it is a
// string so that the JSON reads without a lookup table.
type Format string

// The formats.
const (
	FormatText     Format = "text"
	FormatDocx     Format = "docx"
	FormatXlsx     Format = "xlsx"
	FormatSegments Format = "segments" // text with labels from elsewhere, such as PDF pages
)

// Encoding is the encoding a text file was decoded from.
type Encoding string

// The encodings Read recognises.
const (
	EncodingUTF8        Encoding = "utf-8"
	EncodingUTF8BOM     Encoding = "utf-8-bom"
	EncodingUTF16LE     Encoding = "utf-16le"
	EncodingUTF16BE     Encoding = "utf-16be"
	EncodingWindows1252 Encoding = "windows-1252"
)

// LineEnding is the line-ending convention of a text. The empty value means
// the text has no line terminator at all.
type LineEnding string

// The conventions.
const (
	LineEndingLF    LineEnding = "lf"
	LineEndingCRLF  LineEnding = "crlf"
	LineEndingCR    LineEnding = "cr"
	LineEndingMixed LineEnding = "mixed"
)

// PartKind names a part of a document.
type PartKind string

// The parts. Text has only a body; Word documents may have the others; each
// sheet of a workbook is a part of its own.
const (
	PartBody      PartKind = "body"
	PartHeaders   PartKind = "headers"
	PartFooters   PartKind = "footers"
	PartFootnotes PartKind = "footnotes"
	PartEndnotes  PartKind = "endnotes"
	PartComments  PartKind = "comments"
	PartSheet     PartKind = "sheet"
)

// PartStatus says whether a part changed, or exists on one side only.
type PartStatus string

// The statuses.
const (
	StatusSame    PartStatus = "same"
	StatusChanged PartStatus = "changed"
	StatusAdded   PartStatus = "added"
	StatusRemoved PartStatus = "removed"
)
