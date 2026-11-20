// Package xlsx reads the cells of an Excel workbook: sheet names and order
// from xl/workbook.xml, shared strings with their rich-text runs, cell types
// and number formats, so that a date shows as a date and every other number
// as the file writes it.
//
// Sheets are read with encoding/xml's streaming Decoder, never as whole
// trees, since a sheet can hold millions of cells.
package xlsx

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/ooxml"
)

// ErrLimit reports a workbook with more cells than allowed.
var ErrLimit = errors.New("too many cells")

// Options choose what a cell shows and bound the work.
type Options struct {
	Formulas bool // a formula cell shows its formula, "=SUM(A1:A3)", not its cached value
	Depth    int  // XML nesting limit
	MaxCells int  // cells in the whole workbook
}

// Workbook is the content of a workbook.
type Workbook struct {
	Sheets  []Sheet
	Skipped []string // sheets without cells, such as chart sheets
	Missing bool     // some formula has no cached value to show
}

// Sheet is one worksheet.
type Sheet struct {
	Name   string
	Hidden bool
	Rows   []Row
}

// Row is a row with at least one cell that shows something. Empty rows are
// left out, so that inserting a blank row is not a change.
type Row struct {
	N     int // the row number in the sheet, from 1
	Cells []Cell
}

// Cell is one cell that shows something.
type Cell struct {
	Col  int    // from 1
	Text string // what is compared: the value, or "=" and the formula
}

type reader struct {
	pkg      *ooxml.Package
	o        Options
	strings  []string
	kinds    []kind // by cell format index
	date1904 bool
	cells    int
	missing  bool
}

// Read reads the workbook whose main part is main.
func Read(pkg *ooxml.Package, main string, o Options) (*Workbook, error) {
	r := &reader{pkg: pkg, o: o}
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
	if name := first["sharedStrings"]; name != "" {
		if r.strings, err = r.sharedStrings(name); err != nil {
			return nil, err
		}
	}
	if name := first["styles"]; name != "" {
		if err := r.styles(name); err != nil {
			return nil, err
		}
	}
	b, err := pkg.Read(main)
	if err != nil {
		return nil, err
	}
	tree, err := ooxml.ParseTree(b, o.Depth)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", main, err)
	}
	wb := tree.Child(ooxml.NSS, "workbook")
	if p := wb.Child(ooxml.NSS, "workbookPr"); p != nil {
		v := p.Get(ooxml.NSOther, "date1904")
		r.date1904 = v == "1" || v == "true"
	}
	out := &Workbook{}
	for _, s := range wb.Child(ooxml.NSS, "sheets").Children(ooxml.NSS, "sheet") {
		name := s.Get(ooxml.NSOther, "name")
		rel, ok := byID[s.Get(ooxml.NSR, "id")]
		if !ok {
			continue
		}
		if rel.Type != "worksheet" {
			// Chart sheets and dialog sheets have no cells to compare.
			out.Skipped = append(out.Skipped, name)
			continue
		}
		state := s.Get(ooxml.NSOther, "state")
		sheet := Sheet{Name: name, Hidden: state == "hidden" || state == "veryHidden"}
		if sheet.Rows, err = r.sheet(rel.Target); err != nil {
			return nil, fmt.Errorf("sheet %q: %w", name, err)
		}
		out.Sheets = append(out.Sheets, sheet)
	}
	out.Missing = r.missing
	return out, nil
}

// richText collects the text of a string item: a plain t, or the t of each
// run of rich text. Phonetic runs (rPh), the reading aids of Japanese, are
// not part of the text.
func richText(d *ooxml.Decoder) (string, error) {
	var b strings.Builder
	var walk func() error
	walk = func() error {
		for {
			t, err := d.Token()
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			if err != nil {
				return err
			}
			switch t := t.(type) {
			case ooxml.StartElement:
				switch {
				case ooxml.Space(t.Name.Space) == ooxml.NSS && t.Name.Local == "t":
					s, err := d.Text()
					if err != nil {
						return err
					}
					b.WriteString(s)
				case ooxml.Space(t.Name.Space) == ooxml.NSS && (t.Name.Local == "rPh" || t.Name.Local == "phoneticPr" || t.Name.Local == "rPr"):
					if err := d.Skip(); err != nil {
						return err
					}
				default:
					if err := walk(); err != nil {
						return err
					}
				}
			case ooxml.EndElement:
				return nil
			}
		}
	}
	if err := walk(); err != nil {
		return "", err
	}
	return unescape(b.String()), nil
}

// unescape decodes the _xHHHH_ escapes SpreadsheetML uses for characters
// XML cannot carry, such as _x000D_ for a carriage return; _x005F_ escapes
// the underscore itself.
func unescape(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+7 <= len(s) && s[i] == '_' && s[i+1] == 'x' && s[i+6] == '_' {
			if v, err := strconv.ParseUint(s[i+2:i+6], 16, 16); err == nil {
				b.WriteRune(rune(v))
				i += 7
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func (r *reader) sharedStrings(name string) ([]string, error) {
	b, err := r.pkg.Read(name)
	if err != nil {
		return nil, err
	}
	d := ooxml.NewDecoder(b, r.o.Depth)
	var out []string
	for {
		t, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if s, ok := t.(ooxml.StartElement); ok && ooxml.Space(s.Name.Space) == ooxml.NSS && s.Name.Local == "si" {
			text, err := richText(d)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out = append(out, text)
		}
	}
}

// styles reads which cell formats show dates and times.
func (r *reader) styles(name string) error {
	b, err := r.pkg.Read(name)
	if err != nil {
		return err
	}
	tree, err := ooxml.ParseTree(b, r.o.Depth)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	ss := tree.Child(ooxml.NSS, "styleSheet")
	custom := map[int]kind{}
	for _, f := range ss.Child(ooxml.NSS, "numFmts").Children(ooxml.NSS, "numFmt") {
		if id, err := strconv.Atoi(f.Get(ooxml.NSOther, "numFmtId")); err == nil {
			custom[id] = codeKind(f.Get(ooxml.NSOther, "formatCode"))
		}
	}
	for _, xf := range ss.Child(ooxml.NSS, "cellXfs").Children(ooxml.NSS, "xf") {
		id, err := strconv.Atoi(xf.Get(ooxml.NSOther, "numFmtId"))
		k := plain
		if err == nil {
			if c, ok := custom[id]; ok {
				k = c
			} else {
				k = builtinKind(id)
			}
		}
		r.kinds = append(r.kinds, k)
	}
	return nil
}

// sheet reads the rows of one worksheet.
func (r *reader) sheet(name string) ([]Row, error) {
	b, err := r.pkg.Read(name)
	if err != nil {
		return nil, err
	}
	d := ooxml.NewDecoder(b, r.o.Depth)
	var rows []Row
	shared := map[string]sharedFormula{}
	lastRow := 0
	for {
		t, err := d.Token()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		s, ok := t.(ooxml.StartElement)
		if !ok || ooxml.Space(s.Name.Space) != ooxml.NSS || s.Name.Local != "row" {
			continue
		}
		n := lastRow + 1
		if v, err := strconv.Atoi(ooxml.Attr(s, ooxml.NSOther, "r")); err == nil {
			n = v
		}
		if n < 1 || n > maxRow {
			return nil, fmt.Errorf("%w: row number %d", ooxml.ErrMalformed, n)
		}
		lastRow = n
		row, err := r.row(d, n, shared)
		if err != nil {
			return nil, err
		}
		if len(row.Cells) > 0 {
			rows = append(rows, row)
		}
	}
}

type sharedFormula struct {
	text     string
	col, row int
}

func (r *reader) row(d *ooxml.Decoder, n int, shared map[string]sharedFormula) (Row, error) {
	row := Row{N: n}
	lastCol := 0
	for {
		t, err := d.Token()
		if err != nil {
			return row, err
		}
		switch t := t.(type) {
		case ooxml.EndElement:
			slices.SortStableFunc(row.Cells, func(a, b Cell) int { return a.Col - b.Col })
			return row, nil
		case ooxml.StartElement:
			if ooxml.Space(t.Name.Space) != ooxml.NSS || t.Name.Local != "c" {
				if err := d.Skip(); err != nil {
					return row, err
				}
				continue
			}
			col := lastCol + 1
			if ref := ooxml.Attr(t, ooxml.NSOther, "r"); ref != "" {
				c, _, ok := parseRef(ref)
				if !ok {
					return row, fmt.Errorf("%w: cell reference %q", ooxml.ErrMalformed, ref)
				}
				col = c
			}
			if col > maxCol {
				return row, fmt.Errorf("%w: column %d", ooxml.ErrMalformed, col)
			}
			lastCol = col
			text, err := r.cell(d, t, col, n, shared)
			if err != nil {
				return row, err
			}
			if text == "" {
				continue
			}
			r.cells++
			if r.cells > r.o.MaxCells {
				return row, fmt.Errorf("%w: more than %d cells", ErrLimit, r.o.MaxCells)
			}
			row.Cells = append(row.Cells, Cell{Col: col, Text: text})
		}
	}
}

// cell reads one c element and returns what it shows.
func (r *reader) cell(d *ooxml.Decoder, c ooxml.StartElement, col, row int, shared map[string]sharedFormula) (string, error) {
	typ := ooxml.Attr(c, ooxml.NSOther, "t")
	style, _ := strconv.Atoi(ooxml.Attr(c, ooxml.NSOther, "s"))
	var value, formula, inline string
	var hasValue, hasInline bool
	for open := true; open; {
		t, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := t.(type) {
		case ooxml.EndElement:
			open = false
		case ooxml.StartElement:
			if ooxml.Space(t.Name.Space) != ooxml.NSS {
				if err := d.Skip(); err != nil {
					return "", err
				}
				continue
			}
			switch t.Name.Local {
			case "v":
				if value, err = d.Text(); err != nil {
					return "", err
				}
				hasValue = true
			case "f":
				text, err := d.Text()
				if err != nil {
					return "", err
				}
				formula = r.formula(t, text, col, row, shared)
			case "is":
				if inline, err = richText(d); err != nil {
					return "", err
				}
				hasInline = true
			default:
				if err := d.Skip(); err != nil {
					return "", err
				}
			}
		}
	}
	if r.o.Formulas && formula != "" {
		return "=" + formula, nil
	}
	if formula != "" && !hasValue && !hasInline {
		r.missing = true
	}
	switch typ {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || i < 0 || i >= len(r.strings) {
			if !hasValue {
				return "", nil
			}
			return "", fmt.Errorf("%w: shared string %q does not exist", ooxml.ErrMalformed, value)
		}
		return r.strings[i], nil
	case "inlineStr":
		return inline, nil
	case "str", "e", "d":
		return unescape(value), nil
	case "b":
		switch strings.TrimSpace(value) {
		case "1":
			return "TRUE", nil
		case "0":
			return "FALSE", nil
		}
		return value, nil
	}
	if !hasValue {
		return "", nil
	}
	if style >= 0 && style < len(r.kinds) && r.kinds[style] != plain {
		if s, ok := format(value, r.kinds[style], r.date1904); ok {
			return s, nil
		}
	}
	return strings.TrimSpace(value), nil
}

// formula returns the text of a cell's formula. A shared formula is
// written once, on the first cell of its range; the other cells refer to
// it by index and get it moved by their distance from that cell.
func (r *reader) formula(f ooxml.StartElement, text string, col, row int, shared map[string]sharedFormula) string {
	if ooxml.Attr(f, ooxml.NSOther, "t") != "shared" {
		return text
	}
	si := ooxml.Attr(f, ooxml.NSOther, "si")
	if text != "" {
		shared[si] = sharedFormula{text: text, col: col, row: row}
		return text
	}
	master, ok := shared[si]
	if !ok {
		return ""
	}
	return shift(master.text, row-master.row, col-master.col)
}
