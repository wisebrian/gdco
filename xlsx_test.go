package gdco

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

func workbook(t testing.TB, name string) File {
	t.Helper()
	b, err := os.ReadFile("testdata/xlsx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return File{Name: name, Data: b}
}

func cellList(p PartDiff) []string {
	var out []string
	for _, c := range p.Cells {
		out = append(out, c.A+"→"+c.B+": "+c.Old+" → "+c.New)
	}
	return out
}

func TestCompareWorkbooks(t *testing.T) {
	r, err := CompareFiles(workbook(t, "budget-v1.xlsx"), workbook(t, "budget-v2.xlsx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, p := range r.Parts {
		parts = append(parts, p.Name+":"+string(p.Status))
	}
	if got := strings.Join(parts, ","); got != "Budget:changed,Notes:changed,Archive:removed,Forecast:added" {
		t.Fatalf("parts %s", got)
	}
	budget := r.Parts[0]
	want := []string{
		"B2→B2: 1200 → 1250",
		"C3→C3: 2026-02-15 → 2026-02-20",
		"B5→B6: 1500 → 1700",
	}
	if got := cellList(budget); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("cells:\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// One inserted row; the row below it, Software, moved from 4 to 5 and
	// stays equal.
	if got := changes(budget); len(got) != 4 || got[2] != "insert |4 Insurance | 150 | 2026-03-15" {
		t.Errorf("rows %q", got)
	}
	if budget.Stats.LinesAdded != 1 || budget.Stats.LinesChanged != 3 || budget.Stats.CellsChanged != 3 {
		t.Errorf("budget stats %+v", budget.Stats)
	}
	if r.Stats.CellsChanged != 4 {
		t.Errorf("total stats %+v", r.Stats)
	}

	o := DefaultOptions()
	o.Formulas = true
	r, err = CompareFiles(workbook(t, "budget-v1.xlsx"), workbook(t, "budget-v2.xlsx"), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := cellList(r.Parts[0]); len(got) != 3 || got[2] != "B5→B6: =SUM(B2:B4) → =SUM(B2:B5)" {
		t.Errorf("formula cells %q", got)
	}

	var sb strings.Builder
	if err := r.WriteHTML(&sb, nil); err != nil {
		t.Fatal(err)
	}
	checkHTML(t, sb.String())
	for _, s := range []string{"Sheet “Budget”", "B5 → B6", "=SUM(B2:B5)", "Only in A.", "Rows 1–5 → 1–6"} {
		if !strings.Contains(sb.String(), s) {
			t.Errorf("report lacks %q", s)
		}
	}
	sb.Reset()
	if err := r.WriteUnified(&sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "--- budget-v1.xlsx (sheet Budget)") || !strings.Contains(sb.String(), "+4 Insurance | 150 | 2026-03-15") {
		t.Errorf("unified:\n%s", sb.String())
	}
	b, err := r.MarshalJSON()
	if err != nil || !strings.Contains(string(b), `"cells":[{"a":"B2","b":"B2","old":"1200","new":"1250"}`) {
		t.Errorf("JSON %.300s %v", b, err)
	}
	checkJSON(t, r)
}

func TestWorkbookIgnoresCase(t *testing.T) {
	o := DefaultOptions()
	o.IgnoreCase = true
	r, err := CompareFiles(workbook(t, "budget-v1.xlsx"), workbook(t, "budget-v1.xlsx"), o)
	if err != nil || !r.Equal || r.Stats.CellsChanged != 0 {
		t.Errorf("a workbook against itself: %v %+v", err, r.Stats)
	}
}

func TestWorkbookMismatch(t *testing.T) {
	_, err := CompareFiles(workbook(t, "budget-v1.xlsx"), File{Name: "x.txt", Data: []byte("x\n")}, nil)
	if !errors.Is(err, ErrMismatch) {
		t.Errorf("workbook against text: %v", err)
	}
	_, err = CompareFiles(workbook(t, "budget-v1.xlsx"), fixture(t, "contract-v1.docx"), nil)
	if !errors.Is(err, ErrMismatch) {
		t.Errorf("workbook against Word: %v", err)
	}
}

func TestRowText(t *testing.T) {
	for _, c := range []struct {
		cells []sheetCell
		want  string
	}{
		{[]sheetCell{{1, "a"}, {2, "b"}}, "a | b"},
		{[]sheetCell{{1, "a"}, {3, "c"}}, "a |  | c"},
		{[]sheetCell{{2, "b"}}, " | b"},
		{[]sheetCell{{1, "two\nlines"}}, "two↵lines"},
		{[]sheetCell{{1, "a"}, {16384, "far"}}, "A: a | XFD: far"},
	} {
		if got := rowText(c.cells); got != c.want {
			t.Errorf("rowText(%v) = %q, want %q", c.cells, got, c.want)
		}
	}
}

func TestExcelFeatures(t *testing.T) {
	d, err := Read(workbook(t, "excel-features.xlsx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, n := range d.notes {
		codes = append(codes, n.Code)
	}
	if strings.Join(codes, ",") != "hidden-sheets,skipped-content" {
		t.Errorf("notes %v", codes)
	}
	r, err := Compare(d, d, nil)
	if err != nil || !r.Equal {
		t.Errorf("a workbook differs from itself: %v", err)
	}
}

// FuzzSheets compares two one-sheet workbooks made from fuzzed sheet XML,
// checking what a UI relies on: rows give both sheets back, and every cell
// change has addresses on the rows it compares.
func FuzzSheets(f *testing.F) {
	f.Add(`<row r="1"><c r="A1"><v>1</v></c><c r="B1" t="s"><v>0</v></c></row>`, `<row r="1"><c r="A1"><v>2</v></c></row><row r="3"><c r="C3"><f>A1</f><v>2</v></c></row>`, uint16(0))
	f.Add(`<row><c><v>x</v></c></row>`, `<row><c t="inlineStr"><is><t>X</t></is></c></row>`, uint16(1))
	f.Fuzz(func(t *testing.T, a, b string, bits uint16) {
		o := optionsFrom(bits)
		o.Formulas = bits&256 != 0
		strs := []string{"one", "two"}
		da, err := Read(File{Name: "a.xlsx", Data: ooxmltest.Xlsx(strs, a)}, o)
		if err != nil {
			return
		}
		db, err := Read(File{Name: "b.xlsx", Data: ooxmltest.Xlsx(strs, b)}, o)
		if err != nil {
			return
		}
		r, err := Compare(da, db, o)
		if err != nil {
			t.Fatal(err)
		}
		checkResult(t, r, da, db)
		for _, p := range r.Parts {
			if p.Stats.CellsChanged != len(p.Cells) {
				t.Fatalf("%d cells changed, %d listed", p.Stats.CellsChanged, len(p.Cells))
			}
			for _, c := range p.Cells {
				if c.A == "" || c.B == "" || c.Old == c.New && !o.IgnoreCase && !o.IgnoreSpaceChange && !o.IgnoreAllSpace && !o.IgnoreTrailingSpace {
					t.Fatalf("cell change %+v", c)
				}
			}
		}
		var sb strings.Builder
		if err := r.WriteHTML(&sb, nil); err != nil {
			t.Fatal(err)
		}
		checkHTML(t, sb.String())
		checkJSON(t, r)
	})
}
