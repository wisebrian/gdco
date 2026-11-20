package xlsx

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml"
)

var update = flag.Bool("update", false, "rewrite the golden files of the fixtures")

func dump(wb *Workbook) string {
	var b strings.Builder
	for _, s := range wb.Sheets {
		fmt.Fprintf(&b, "== %s hidden=%v\n", s.Name, s.Hidden)
		for _, c := range cells(s) {
			b.WriteString(c + "\n")
		}
	}
	for _, s := range wb.Skipped {
		b.WriteString("== skipped " + s + "\n")
	}
	return b.String()
}

// TestFixtures reads every .xlsx in testdata/xlsx, showing values and then
// formulas, and compares with the golden files beside them; -update
// rewrites them, to be checked by eye.
func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/xlsx/*.xlsx")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, formulas := range []bool{false, true} {
			pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 1000, Part: 64 << 20, Total: 256 << 20, Ratio: 1000, Depth: 256})
			if err != nil {
				t.Fatal(err)
			}
			main, _, err := pkg.MainPart()
			if err != nil {
				t.Fatal(err)
			}
			o := opts
			o.Formulas = formulas
			wb, err := Read(pkg, main, o)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(f, ".xlsx") + ".values.txt"
			if formulas {
				golden = strings.TrimSuffix(f, ".xlsx") + ".formulas.txt"
			}
			got := dump(wb)
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("%s differs from %s:\n%s", f, golden, got)
			}
		}
	}
}

// featuresXlsx is a workbook with what LibreOffice does not write: shared
// formulas, inline strings, booleans and errors, escapes, custom date
// formats, a hidden sheet and a chart sheet.
func featuresXlsx() []byte {
	return book{
		strings: []string{`<t>Name</t>`, `<r><t>rich </t></r><r><rPr><i/></rPr><t>text</t></r><rPh sb="0" eb="1"><t>x</t></rPh>`, `<t>two_x000D_lines</t>`},
		styles:  dateStyles,
		sheets: [][2]string{
			{"Data", `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c></row>
<row r="2"><c r="A2"><v>1</v></c><c r="B2"><f t="shared" ref="B2:B4" si="0">A2*2</f><v>2</v></c><c r="C2" s="1"><v>45000</v></c><c r="D2" s="5"><v>1.5</v></c></row>
<row r="3"><c r="A3"><v>2</v></c><c r="B3"><f t="shared" si="0"/><v>4</v></c><c r="C3" t="b"><v>1</v></c><c r="D3" t="e"><v>#N/A</v></c></row>
<row r="4"><c r="A4"><v>3</v></c><c r="B4"><f t="shared" si="0"/><v>6</v></c><c r="C4" t="inlineStr"><is><t>inline</t></is></c><c r="D4" s="4"><v>45000.25</v></c></row>`},
			{"Hidden", `<row r="1"><c r="A1"><v>42</v></c></row>`},
		},
		hidden: "Hidden",
		chart:  "Chart",
	}.build()
}

// TestWriteFixtures writes the fixtures made by this package, with
// -update; without it, it checks that the committed file is up to date.
func TestWriteFixtures(t *testing.T) {
	const name = "../../testdata/xlsx/excel-features.xlsx"
	data := featuresXlsx()
	if *update {
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	read := func(b []byte) string {
		pkg, err := ooxml.Open(b, ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 8 << 20, Ratio: 1000, Depth: 64})
		if err != nil {
			t.Fatal(err)
		}
		wb, err := Read(pkg, "xl/workbook.xml", opts)
		if err != nil {
			t.Fatal(err)
		}
		return dump(wb)
	}
	if read(old) != read(data) {
		t.Errorf("%s is out of date: run go test -run TestWriteFixtures -update", name)
	}
}

func FuzzRead(f *testing.F) {
	files, _ := filepath.Glob("../../testdata/xlsx/*.xlsx")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b)
		}
	}
	f.Add(featuresXlsx())
	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 4 << 20, Ratio: 100, Depth: 64})
		if err != nil {
			return
		}
		main, _, err := pkg.MainPart()
		if err != nil {
			return
		}
		Read(pkg, main, opts)
	})
}
