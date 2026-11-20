package gdco

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/textenc"
	"github.com/wisebrian/gdco/internal/xlsx"
)

// sheetCell is a cell of a spreadsheet row: its column and what it shows,
// the value or, with Options.Formulas, the formula.
type sheetCell struct {
	col  int
	text string
}

// readExcel turns each worksheet into a part whose lines are its rows, so
// that the line comparison aligns rows: an inserted row is one inserted
// line and the rows below it stay equal. Cells are compared afterwards,
// column by column, within the rows the alignment paired. A row's number
// is its marker: shown, and kept out of the comparison, since inserting a
// row renumbers every row below it.
func readExcel(fileName string, pkg *ooxml.Package, main string, o *Options) (*Document, error) {
	name := display(fileName)
	wb, err := xlsx.Read(pkg, main, xlsx.Options{Formulas: o.Formulas, Depth: o.Limits.MaxXMLDepth, MaxCells: o.Limits.MaxCells})
	if err != nil {
		if errors.Is(err, xlsx.ErrLimit) {
			return nil, fmt.Errorf("%w: %s: %v", ErrLimit, name, err)
		}
		return nil, officeErr(name, err)
	}
	d := &Document{name: valid(fileName), format: FormatXlsx}
	var hidden []string
	for _, s := range wb.Sheets {
		pt := &part{kind: PartSheet, name: valid(s.Name), lines: make([]line, len(s.Rows))}
		extras := make([]lineExtra, len(s.Rows))
		for i, r := range s.Rows {
			cells := make([]sheetCell, len(r.Cells))
			for k, c := range r.Cells {
				cells[k] = sheetCell{col: c.Col, text: valid(c.Text)}
			}
			extras[i] = lineExtra{marker: strconv.Itoa(r.N), loc: Location{Row: r.N}, cells: cells}
			pt.lines[i] = line{text: rowText(cells), eol: textenc.LF, x: &extras[i]}
		}
		d.parts = append(d.parts, pt)
		if s.Hidden {
			hidden = append(hidden, s.Name)
		}
	}
	if n := d.lineCount(); n > o.Limits.MaxLines {
		return nil, fmt.Errorf("%w: %s has %d rows, more than %d", ErrLimit, name, n, o.Limits.MaxLines)
	}
	if len(hidden) > 0 {
		d.notes = append(d.notes, Note{Code: "hidden-sheets", Message: "Hidden sheets were compared too: " + quoted(hidden) + "."})
	}
	if len(wb.Skipped) > 0 {
		d.notes = append(d.notes, Note{Code: "skipped-content", Message: "Sheets without cells, such as charts, were not compared: " + quoted(wb.Skipped) + "."})
	}
	if wb.Missing && !o.Formulas {
		d.notes = append(d.notes, Note{Code: "missing-values", Message: "Some formulas have no saved result, so their cells look empty; compare formulas to see them."})
	}
	return d, nil
}

func quoted(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "“" + valid(n) + "”"
	}
	return strings.Join(out, ", ")
}

// widest is the last column a row line spells out cell by cell. Beyond it a
// row is written with addresses instead, so that one value in column XFD
// does not make a line of sixteen thousand empty cells.
const widest = 64

// rowText is the line a row is compared as: its cells in column order
// separated by " | ", with empty cells kept in place, so that a value that
// moves to another column is a change.
func rowText(cells []sheetCell) string {
	if len(cells) == 0 {
		return ""
	}
	clean := func(s string) string {
		if strings.ContainsAny(s, "\r\n") {
			return strings.NewReplacer("\r\n", "↵", "\r", "↵", "\n", "↵").Replace(s)
		}
		return s
	}
	last := cells[len(cells)-1].col
	if last > widest {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = xlsx.ColName(c.col) + ": " + clean(c.text)
		}
		return strings.Join(parts, " | ")
	}
	vals := make([]string, last)
	for _, c := range cells {
		vals[c.col-1] = clean(c.text)
	}
	return strings.Join(vals, " | ")
}

// cellChanges compares, column by column, the cells of every pair of rows
// the alignment made: rows it found equal and rows it paired as modified.
// Cells compare under the same options as lines, so ignoring case ignores
// it here too.
func (c *comparer) cellChanges(rows []row, pa, pb *part) []CellChange {
	var out []CellChange
	var ka, kb []byte
	same := func(x, y string) bool {
		if c.norm.Plain() {
			return x == y
		}
		ka = c.norm.LineKey(ka[:0], x)
		kb = c.norm.LineKey(kb[:0], y)
		return string(ka) == string(kb)
	}
	for _, r := range rows {
		if r.a < 0 || r.b < 0 || r.ignored {
			continue
		}
		la, lb := &pa.lines[r.a], &pb.lines[r.b]
		if la.x == nil || lb.x == nil || (r.op == rEqual && la.text == lb.text) {
			continue
		}
		ca, cb := la.x.cells, lb.x.cells
		rowA, rowB := la.x.loc.Row, lb.x.loc.Row
		for i, j := 0, 0; i < len(ca) || j < len(cb); {
			var col int
			var old, new string
			switch {
			case j >= len(cb) || (i < len(ca) && ca[i].col < cb[j].col):
				col, old = ca[i].col, ca[i].text
				i++
			case i >= len(ca) || cb[j].col < ca[i].col:
				col, new = cb[j].col, cb[j].text
				j++
			default:
				col, old, new = ca[i].col, ca[i].text, cb[j].text
				i++
				j++
			}
			if !same(old, new) {
				out = append(out, CellChange{A: xlsx.Address(col, rowA), B: xlsx.Address(col, rowB), Old: old, New: new})
			}
		}
	}
	return out
}
