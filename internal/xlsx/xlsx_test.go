package xlsx

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

const nsS = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

// book describes a workbook for a test: sheets in order, by name, as the
// XML inside sheetData.
type book struct {
	sheets   [][2]string
	strings  []string // shared string items, as the XML inside si
	styles   string   // the XML inside styleSheet
	date1904 bool
	hidden   string // name of a sheet to mark hidden
	chart    string // name of a chart sheet to add
}

func (bk book) build() []byte {
	const rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	var sheets, rels, ct strings.Builder
	var parts []ooxmltest.Part
	for i, s := range bk.sheets {
		state := ""
		if s[0] == bk.hidden {
			state = ` state="hidden"`
		}
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d"%s r:id="rs%d"/>`, s[0], i+1, state, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rs%d" Type="%sworksheet" Target="worksheets/sheet%d.xml"/>`, i+1, rel, i+1)
		parts = append(parts, ooxmltest.Part{Name: fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			Data: `<worksheet ` + nsS + `><sheetData>` + s[1] + `</sheetData></worksheet>`})
	}
	if bk.chart != "" {
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="99" r:id="rc"/>`, bk.chart)
		fmt.Fprintf(&rels, `<Relationship Id="rc" Type="%schartsheet" Target="chartsheets/sheet1.xml"/>`, rel)
		parts = append(parts, ooxmltest.Part{Name: "xl/chartsheets/sheet1.xml", Data: `<chartsheet ` + nsS + `/>`})
	}
	if bk.strings != nil {
		fmt.Fprintf(&rels, `<Relationship Id="rss" Type="%ssharedStrings" Target="sharedStrings.xml"/>`, rel)
		parts = append(parts, ooxmltest.Part{Name: "xl/sharedStrings.xml",
			Data: `<sst ` + nsS + `><si>` + strings.Join(bk.strings, `</si><si>`) + `</si></sst>`})
	}
	if bk.styles != "" {
		fmt.Fprintf(&rels, `<Relationship Id="rst" Type="%sstyles" Target="styles.xml"/>`, rel)
		parts = append(parts, ooxmltest.Part{Name: "xl/styles.xml", Data: `<styleSheet ` + nsS + `>` + bk.styles + `</styleSheet>`})
	}
	pr := ""
	if bk.date1904 {
		pr = `<workbookPr date1904="1"/>`
	}
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`)
	return ooxmltest.Zip(append([]ooxmltest.Part{
		{Name: "[Content_Types].xml", Data: ct.String()},
		{Name: "_rels/.rels", Data: `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="` + rel + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{Name: "xl/_rels/workbook.xml.rels", Data: `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
		{Name: "xl/workbook.xml", Data: `<workbook ` + nsS + `>` + pr + `<sheets>` + sheets.String() + `</sheets></workbook>`},
	}, parts...)...)
}

var opts = Options{Depth: 64, MaxCells: 1_000_000}

func readBook(t *testing.T, bk book, o Options) *Workbook {
	t.Helper()
	pkg, err := ooxml.Open(bk.build(), ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 8 << 20, Ratio: 1000, Depth: 64})
	if err != nil {
		t.Fatal(err)
	}
	main, ct, err := pkg.MainPart()
	if err != nil || !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("main part %q %q %v", main, ct, err)
	}
	wb, err := Read(pkg, main, o)
	if err != nil {
		t.Fatal(err)
	}
	return wb
}

// cells renders a sheet as "B7=value" lines.
func cells(s Sheet) []string {
	var out []string
	for _, r := range s.Rows {
		for _, c := range r.Cells {
			out = append(out, Address(c.Col, r.N)+"="+c.Text)
		}
	}
	return out
}

func check(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot\n  %s\nwant\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestCellTypes(t *testing.T) {
	wb := readBook(t, book{
		strings: []string{`<t>plain</t>`, `<r><rPr><b/></rPr><t>rich </t></r><r><t>text</t></r><rPh sb="0" eb="1"><t>ふり</t></rPh>`, `<t>line_x000D_break_x005F_x</t>`},
		sheets: [][2]string{{"Data", `
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row>
<row r="2"><c r="A2" t="inlineStr"><is><t>inline</t></is></c><c r="B2" t="str"><f>"x"&amp;"y"</f><v>xy</v></c><c r="C2" t="b"><v>1</v></c><c r="D2" t="b"><v>0</v></c></row>
<row r="3"><c r="A3" t="e"><f>1/0</f><v>#DIV/0!</v></c><c r="B3"><v>0.30000000000000004</v></c><c r="C3" t="n"><v>1E-3</v></c><c r="D3" t="d"><v>2024-03-15T00:00:00</v></c></row>
<row r="5"><c r="A5" s="0"/><c r="B5"/></row>
<row><c><v>7</v></c><c><v>8</v></c><c r="E6"><v>9</v></c></row>`}},
	}, opts)
	check(t, "cells", cells(wb.Sheets[0]),
		"A1=plain", "B1=rich text", "C1=line\rbreak_x",
		"A2=inline", "B2=xy", "C2=TRUE", "D2=FALSE",
		"A3=#DIV/0!", "B3=0.30000000000000004", "C3=1E-3", "D3=2024-03-15T00:00:00",
		"A6=7", "B6=8", "E6=9")
	if n := len(wb.Sheets[0].Rows); n != 4 {
		t.Errorf("%d rows: empty row 5 must be left out", n)
	}
}

const dateStyles = `<numFmts count="4">
<numFmt numFmtId="164" formatCode="yyyy-mm-dd hh:mm"/>
<numFmt numFmtId="165" formatCode="[h]:mm:ss"/>
<numFmt numFmtId="166" formatCode="0 &quot;days&quot;"/>
<numFmt numFmtId="167" formatCode="[$-409]mmmm d, yyyy;@"/>
</numFmts>
<cellXfs count="8">
<xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="22"/><xf numFmtId="20"/>
<xf numFmtId="164"/><xf numFmtId="165"/><xf numFmtId="166"/><xf numFmtId="167"/>
</cellXfs>`

func TestDates(t *testing.T) {
	sheet := `<row r="1">
<c r="A1" s="1"><v>45000</v></c><c r="B1" s="1"><v>60</v></c><c r="C1" s="1"><v>59</v></c><c r="D1" s="1"><v>61</v></c><c r="E1" s="1"><v>1</v></c>
<c r="F1" s="2"><v>45000.75</v></c><c r="G1" s="3"><v>0.5</v></c><c r="H1" s="4"><v>45000.5</v></c><c r="I1" s="5"><v>1.25</v></c>
<c r="J1" s="6"><v>3</v></c><c r="K1" s="7"><v>45000</v></c><c r="L1" s="0"><v>45000</v></c><c r="M1" s="1"><v>-1</v></c><c r="N1" s="1"><v>abc</v></c></row>`
	wb := readBook(t, book{styles: dateStyles, sheets: [][2]string{{"S", sheet}}}, opts)
	check(t, "1900", cells(wb.Sheets[0]),
		"A1=2023-03-15", "B1=1900-02-29", "C1=1900-02-28", "D1=1900-03-01", "E1=1900-01-01",
		"F1=2023-03-15T18:00:00", "G1=12:00:00", "H1=2023-03-15T12:00:00", "I1=30:00:00",
		"J1=3", "K1=2023-03-15", "L1=45000", "M1=-1", "N1=abc")
	wb = readBook(t, book{styles: dateStyles, date1904: true, sheets: [][2]string{{"S", `<row r="1"><c r="A1" s="1"><v>0</v></c><c r="B1" s="1"><v>43538</v></c></row>`}}}, opts)
	check(t, "1904", cells(wb.Sheets[0]), "A1=1904-01-01", "B1=2023-03-15")
}

func TestCodeKind(t *testing.T) {
	for code, want := range map[string]kind{
		"General": plain, "0.00": plain, "#,##0": plain, "0%": plain, "0.00E+00": plain, "@": plain,
		`0 "days"`: plain, "[Red]0.00": plain, `#,##0 [$€-x-euro]`: plain,
		"dd/mm/yyyy": date, "mmm-yy": date, "d-mmm": date, `yyyy\-mm\-dd`: date, "mm": date,
		"h:mm AM/PM": clock, "mm:ss": clock, "hh:mm:ss": clock,
		"m/d/yy h:mm": moment, "[h]:mm:ss": duration, "[mm]:ss": duration,
	} {
		if got := codeKind(code); got != want {
			t.Errorf("codeKind(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestShift(t *testing.T) {
	for _, c := range []struct {
		f      string
		dr, dc int
		want   string
	}{
		{"A1*2", 1, 0, "A2*2"},
		{"$A$1+A1+$A1+A$1", 2, 1, "$A$1+B3+$A3+B$1"},
		{"SUM(A1:A3)", 1, 0, "SUM(A2:A4)"},
		{"Sheet2!A1+'My Sheet'!B2", 1, 1, "Sheet2!B2+'My Sheet'!C3"},
		{`IF(A1="A1",B1,"x")`, 1, 0, `IF(A2="A1",B2,"x")`},
		{"SUM(A:A)+SUM(1:1)", 2, 3, "SUM(D:D)+SUM(3:3)"},
		{"A1+1.5+2E+3", 0, 1, "B1+1.5+2E+3"},
		{"A1", -1, 0, "#REF!"},
		{"Table1[Amount]*A1", 1, 0, "Table1[Amount]*A2"},
		{"_xlfn.XLOOKUP(A1,B:B,C:C)", 1, 0, "_xlfn.XLOOKUP(A2,B:B,C:C)"},
		{"TRUE+LOG10(A1)", 1, 0, "TRUE+LOG10(A2)"},
		{"XFD1+A1", 0, 1, "#REF!+B1"},
	} {
		if got := shift(c.f, c.dr, c.dc); got != c.want {
			t.Errorf("shift(%q, %d, %d) = %q, want %q", c.f, c.dr, c.dc, got, c.want)
		}
	}
}

func TestFormulas(t *testing.T) {
	sheet := `<row r="1"><c r="A1"><v>1</v></c><c r="B1"><f t="shared" ref="B1:B3" si="0">A1*2</f><v>2</v></c><c r="C1"><f>SUM(A1:A3)</f><v>6</v></c></row>
<row r="2"><c r="A2"><v>2</v></c><c r="B2"><f t="shared" si="0"/><v>4</v></c></row>
<row r="3"><c r="A3"><v>3</v></c><c r="B3"><f t="shared" si="0"/><v>6</v></c><c r="C3"><f>A1+1</f></c></row>`
	bk := book{sheets: [][2]string{{"S", sheet}}}
	wb := readBook(t, bk, opts)
	check(t, "values", cells(wb.Sheets[0]), "A1=1", "B1=2", "C1=6", "A2=2", "B2=4", "A3=3", "B3=6")
	if !wb.Missing {
		t.Error("a formula without a cached value must be reported")
	}
	o := opts
	o.Formulas = true
	wb = readBook(t, bk, o)
	check(t, "formulas", cells(wb.Sheets[0]), "A1=1", "B1==A1*2", "C1==SUM(A1:A3)", "A2=2", "B2==A2*2", "A3=3", "B3==A3*2", "C3==A1+1")
}

func TestSheets(t *testing.T) {
	wb := readBook(t, book{
		sheets: [][2]string{{"Second", `<row r="1"><c r="A1"><v>2</v></c></row>`}, {"First", `<row r="1"><c r="A1"><v>1</v></c></row>`}, {"Empty", ``}},
		hidden: "First", chart: "Chart1",
	}, opts)
	var names []string
	for _, s := range wb.Sheets {
		names = append(names, fmt.Sprintf("%s hidden=%v rows=%d", s.Name, s.Hidden, len(s.Rows)))
	}
	check(t, "sheets", names, "Second hidden=false rows=1", "First hidden=true rows=1", "Empty hidden=false rows=0")
	if len(wb.Skipped) != 1 || wb.Skipped[0] != "Chart1" {
		t.Errorf("skipped %v", wb.Skipped)
	}
}

func TestBadSheets(t *testing.T) {
	for name, bk := range map[string]book{
		"bad reference":  {sheets: [][2]string{{"S", `<row r="1"><c r="1A"><v>1</v></c></row>`}}},
		"missing string": {strings: []string{`<t>a</t>`}, sheets: [][2]string{{"S", `<row r="1"><c r="A1" t="s"><v>5</v></c></row>`}}},
		"row zero":       {sheets: [][2]string{{"S", `<row r="0"><c r="A1"><v>1</v></c></row>`}}},
		"column too far": {sheets: [][2]string{{"S", `<row r="1"><c r="XFE1"><v>1</v></c></row>`}}},
	} {
		pkg, err := ooxml.Open(bk.build(), ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 8 << 20, Ratio: 1000, Depth: 64})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Read(pkg, "xl/workbook.xml", opts); !errors.Is(err, ooxml.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var many strings.Builder
	for i := range 100 {
		fmt.Fprintf(&many, `<row r="%d"><c r="A%d"><v>1</v></c><c r="B%d"><v>1</v></c></row>`, i+1, i+1, i+1)
	}
	pkg, _ := ooxml.Open(book{sheets: [][2]string{{"S", many.String()}}}.build(), ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 8 << 20, Ratio: 1000, Depth: 64})
	o := opts
	o.MaxCells = 150
	if _, err := Read(pkg, "xl/workbook.xml", o); !errors.Is(err, ErrLimit) {
		t.Errorf("cell limit: %v", err)
	}
}

func TestAddresses(t *testing.T) {
	for _, c := range []struct {
		col  int
		name string
	}{{1, "A"}, {26, "Z"}, {27, "AA"}, {52, "AZ"}, {53, "BA"}, {702, "ZZ"}, {703, "AAA"}, {16384, "XFD"}} {
		if got := colName(c.col); got != c.name {
			t.Errorf("colName(%d) = %q, want %q", c.col, got, c.name)
		}
		if got := colNumber(c.name); got != c.col {
			t.Errorf("colNumber(%q) = %d, want %d", c.name, got, c.col)
		}
	}
	if colNumber("XFE") != 0 || colNumber("ABCD") != 0 {
		t.Error("columns past XFD accepted")
	}
}

func FuzzShift(f *testing.F) {
	f.Add("SUM($A1:B$2)+'S'!C3&\"D4\"", 1, -1)
	f.Fuzz(func(t *testing.T, formula string, dr, dc int) {
		dr, dc = dr%2000000, dc%40000
		got := shift(formula, dr, dc)
		if dr == 0 && dc == 0 && got != formula {
			t.Fatalf("shift by nothing changed %q to %q", formula, got)
		}
	})
}

func FuzzSheet(f *testing.F) {
	f.Add(`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><f t="shared" si="0" ref="B1:B2">A1</f><v>1</v></c></row><row><c><f t="shared" si="0"/></c></row>`, false)
	f.Fuzz(func(t *testing.T, sheet string, formulas bool) {
		data := book{strings: []string{`<t>s</t>`}, styles: dateStyles, sheets: [][2]string{{"S", sheet}}}.build()
		pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 8 << 20, Ratio: 1000, Depth: 64})
		if err != nil {
			t.Fatal(err)
		}
		o := opts
		o.Formulas = formulas
		wb, err := Read(pkg, "xl/workbook.xml", o)
		if err != nil {
			return
		}
		for _, s := range wb.Sheets {
			for _, r := range s.Rows {
				if r.N < 1 || r.N > maxRow || len(r.Cells) == 0 {
					t.Fatalf("row %d with %d cells", r.N, len(r.Cells))
				}
				for i, c := range r.Cells {
					if c.Col < 1 || c.Col > maxCol || c.Text == "" || (i > 0 && c.Col < r.Cells[i-1].Col) {
						t.Fatalf("cell %+v in row %d", c, r.N)
					}
				}
			}
		}
	})
}
