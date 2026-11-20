package gdco

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

func fixture(t testing.TB, name string) File {
	t.Helper()
	b, err := os.ReadFile("testdata/docx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return File{Name: name, Data: b}
}

// changes lists the changed rows of a part as "op A|B" strings, with the
// list markers, for compact expectations.
func changes(p PartDiff) []string {
	var out []string
	side := func(l *Line) string {
		if l == nil {
			return ""
		}
		if l.Marker != "" {
			return l.Marker + " " + l.Text
		}
		return l.Text
	}
	for _, h := range p.Hunks {
		for _, r := range h.Rows {
			if r.Op != OpEqual {
				out = append(out, string(r.Op)+" "+side(r.A)+"|"+side(r.B))
			}
		}
	}
	return out
}

func partByKind(r *Result, k PartKind) PartDiff {
	for _, p := range r.Parts {
		if p.Kind == k {
			return p
		}
	}
	return PartDiff{}
}

func TestCompareWordFixtures(t *testing.T) {
	r, err := CompareFiles(fixture(t, "contract-v1.docx"), fixture(t, "contract-v2.docx"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.A.Format != FormatDocx || r.B.Format != FormatDocx {
		t.Fatalf("formats %s %s", r.A.Format, r.B.Format)
	}
	body := changes(partByKind(r, PartBody))
	want := []string{
		"modify This agreement is made between Alpha SRL and Beta SA.|This agreement is made between Omega SRL and Beta SA on 1 March 2026.",
		"modify 1.1. The Supplier shall deliver the software within 30 days.|1.1. The Supplier shall deliver the software within 45 days.",
		// One inserted item, and the items after it renumbered but not
		// marked: Fees, Term and Governing law are equal.
		"insert |2. Warranty",
		"insert |Training | 2 | 300",
		"modify Support | 12 | 50|Support | 12 | 60",
		"modify Stamp area|Stamp and signature area",
	}
	if strings.Join(body, "\n") != strings.Join(want, "\n") {
		t.Errorf("body changes:\n  %s\nwant\n  %s", strings.Join(body, "\n  "), strings.Join(want, "\n  "))
	}
	if got := changes(partByKind(r, PartHeaders)); len(got) != 1 || !strings.Contains(got[0], "Strictly confidential") {
		t.Errorf("header changes %q", got)
	}
	if got := changes(partByKind(r, PartFootnotes)); len(got) != 1 || !strings.Contains(got[0], "21%") {
		t.Errorf("footnote changes %q", got)
	}
	if p := partByKind(r, PartFooters); p.Status != StatusSame {
		t.Errorf("footer status %s", p.Status)
	}
	var noted bool
	for _, n := range r.Notes {
		if n.Code == "tracked-changes" && n.Side == "b" {
			noted = true
		}
	}
	if !noted {
		t.Errorf("no tracked-changes note: %+v", r.Notes)
	}
	// Locations reach the result: the inserted row is row 3 of table 1.
	for _, h := range partByKind(r, PartBody).Hunks {
		for _, row := range h.Rows {
			if row.Op == OpInsert && strings.HasPrefix(row.B.Text, "Training") {
				if row.B.Loc.Table != 1 || row.B.Loc.Row != 3 || row.B.Loc.String() != "Table 1, row 3" {
					t.Errorf("row location %+v", row.B.Loc)
				}
			}
		}
	}

	// With the tracked changes of B rejected, its first paragraph is the
	// same as A's.
	o := DefaultOptions()
	o.Tracked = TrackedOriginal
	r, err = CompareFiles(fixture(t, "contract-v1.docx"), fixture(t, "contract-v2.docx"), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := changes(partByKind(r, PartBody)); len(got) != len(want)-1 {
		t.Errorf("original view: %q", got)
	}

	var sb strings.Builder
	if err := r.WriteUnified(&sb); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"--- contract-v1.docx (headers)", "+2. Warranty", "+++ contract-v2.docx (footnotes)"} {
		if !strings.Contains(sb.String(), s) {
			t.Errorf("unified diff lacks %q:\n%s", s, sb.String())
		}
	}
	sb.Reset()
	if err := r.WriteHTML(&sb, nil); err != nil {
		t.Fatal(err)
	}
	checkHTML(t, sb.String())
	for _, s := range []string{"<h2>Headers</h2>", "<h2>Footnotes</h2>", `title="Table 1, row 3"`, `<span class="mk">2.</span>`} {
		if !strings.Contains(sb.String(), s) {
			t.Errorf("report lacks %q", s)
		}
	}
}

func TestWordAgainstText(t *testing.T) {
	text := File{Name: "contract.txt", Data: []byte("Service Agreement\nThis agreement is made between Alpha SRL and Beta SA.\n")}
	r, err := CompareFiles(fixture(t, "contract-v1.docx"), text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Parts) != 1 || r.Parts[0].Kind != PartBody {
		t.Fatalf("parts %+v", r.Parts)
	}
	var codes []string
	for _, n := range r.Notes {
		codes = append(codes, n.Code)
	}
	if !strings.Contains(strings.Join(codes, ","), "body-only") {
		t.Errorf("notes %v", codes)
	}
}

func TestOfficeErrors(t *testing.T) {
	mk := func(ct, main string) []byte {
		return ooxmltest.Zip(
			ooxmltest.Part{Name: "[Content_Types].xml", Data: `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/` + main + `" ContentType="` + ct + `"/></Types>`},
			ooxmltest.Part{Name: "_rels/.rels", Data: `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="` + main + `"/></Relationships>`},
			ooxmltest.Part{Name: main, Data: `<x/>`})
	}
	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"pptx":     {mk("application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "ppt/presentation.xml"), ErrUnsupported},
		"odt":      {ooxmltest.Zip(ooxmltest.Part{Name: "mimetype", Data: "application/vnd.oasis.opendocument.text"}, ooxmltest.Part{Name: "content.xml", Data: "<x/>"}), ErrUnsupported},
		"zip":      {ooxmltest.Zip(ooxmltest.Part{Name: "a.txt", Data: "hello"}), ErrUnsupported},
		"bad body": {ooxmltest.Docx(`<w:p><w:r><w:t>unclosed`, nil), ErrMalformed},
		"deep":     {ooxmltest.Docx(strings.Repeat("<w:sdt><w:sdtContent>", 300)+strings.Repeat("</w:sdtContent></w:sdt>", 300), nil), ErrLimit},
	} {
		if _, err := Read(File{Name: name, Data: tc.data}, nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	// A .docm reads like a .docx: the content type decides, not the name.
	docm := ooxmltest.Docx(`<w:p><w:r><w:t>macro</w:t></w:r></w:p>`, nil)
	if d, err := Read(File{Name: "x.docm", Data: docm}, nil); err != nil || d.Format() != FormatDocx {
		t.Errorf("docm: %v", err)
	}
}
