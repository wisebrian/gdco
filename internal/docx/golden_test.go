package docx

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

var update = flag.Bool("update", false, "rewrite the golden files of the fixtures")

// dump renders a document as the golden files store it: one block per
// part, one line per line.
func dump(d *Doc) string {
	var b strings.Builder
	for _, p := range d.Parts {
		b.WriteString("== " + p.Kind + "\n")
		for _, l := range show(p.Lines) {
			b.WriteString(l + "\n")
		}
	}
	if d.Revisions {
		b.WriteString("== tracked changes\n")
	}
	for _, s := range d.Skipped {
		b.WriteString("== skipped " + s + "\n")
	}
	return b.String()
}

// TestFixtures reads every .docx in testdata/docx, as accepted and as
// original, and compares the text with the golden files beside them. Run
// with -update to rewrite them after checking the change by eye.
func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/docx/*.docx")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, original := range []bool{false, true} {
			o := all
			o.Original = original
			golden := strings.TrimSuffix(f, ".docx") + ".accepted.txt"
			if original {
				golden = strings.TrimSuffix(f, ".docx") + ".original.txt"
			}
			got := dump(read(t, data, o))
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

// featuresDocx is a document with the Word constructs LibreOffice does not
// write: a deleted paragraph mark, moves, nested and multi-paragraph
// fields, a first-page header, endnotes, comments, numbering from styles,
// Word's page layout record and a nested table.
func featuresDocx() []byte {
	body := `<w:p><w:r><w:t>Cover page</w:t></w:r><w:r><w:footnoteReference w:id="1"/></w:r><w:r><w:commentReference w:id="3"/></w:r><w:r><w:endnoteReference w:id="1"/></w:r></w:p>` +
		`<w:p><w:r><w:lastRenderedPageBreak/><w:t>Second page</w:t></w:r></w:p>` +
		revisions + tables + textBox +
		item("1", "0", "First") + item("1", "1", "Sub one") + item("2", "0", "bullet") +
		styled("Heading1", "Scope") + styled("Heading1Sub", "Terms") +
		`<w:sectPr><w:headerReference w:type="default" r:id="r_header1"/><w:headerReference w:type="first" r:id="r_header2"/><w:footerReference w:type="default" r:id="r_footer1"/></w:sectPr>`
	return ooxmltest.Docx(body, map[string]string{
		"word/numbering.xml": ooxmltest.Root("w:numbering", numberingXML),
		"word/styles.xml":    ooxmltest.Root("w:styles", stylesXML),
		"word/header1.xml":   ooxmltest.Root("w:hdr", `<w:p><w:r><w:t xml:space="preserve">Page </w:t></w:r><w:fldSimple w:instr="PAGE"><w:r><w:t>2</w:t></w:r></w:fldSimple></w:p>`),
		"word/header2.xml":   ooxmltest.Root("w:hdr", `<w:p><w:r><w:t>Title page header</w:t></w:r></w:p>`),
		"word/footer1.xml":   ooxmltest.Root("w:ftr", `<w:p><w:r><w:t>Footer</w:t></w:r></w:p>`),
		"word/footnotes.xml": ooxmltest.Root("w:footnotes", `<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote><w:footnote w:id="1"><w:p><w:r><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> A footnote.</w:t></w:r></w:p></w:footnote>`),
		"word/endnotes.xml":  ooxmltest.Root("w:endnotes", `<w:endnote w:id="1"><w:p><w:r><w:endnoteRef/></w:r><w:r><w:t xml:space="preserve"> An endnote.</w:t></w:r></w:p></w:endnote>`),
		"word/comments.xml":  ooxmltest.Root("w:comments", `<w:comment w:id="3" w:author="Ana"><w:p><w:r><w:t>Is the cover final?</w:t></w:r></w:p></w:comment><w:comment w:id="4" w:author="Dan"><w:p><w:r><w:t>Not anchored</w:t></w:r></w:p></w:comment>`),
	})
}

// TestWriteFixtures writes the fixtures made by this package, with
// -update; without it, it checks that the committed file is up to date.
func TestWriteFixtures(t *testing.T) {
	const name = "../../testdata/docx/word-features.docx"
	data := featuresDocx()
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
	if dump(read(t, old, all)) != dump(read(t, data, all)) {
		t.Errorf("%s is out of date: run go test -run TestWriteFixtures -update", name)
	}
}

func FuzzRead(f *testing.F) {
	files, _ := filepath.Glob("../../testdata/docx/*.docx")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b)
		}
	}
	f.Add(featuresDocx())
	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 100, Part: 1 << 20, Total: 4 << 20, Ratio: 100, Depth: 64})
		if err != nil {
			return
		}
		main, _, err := pkg.MainPart()
		if err != nil {
			return
		}
		o := all
		o.Depth = 64
		for _, original := range []bool{false, true} {
			o.Original = original
			d, err := Read(pkg, main, o)
			if err != nil {
				continue
			}
			for _, p := range d.Parts {
				for _, l := range p.Lines {
					if strings.ContainsAny(l.Text, "\r\n") {
						t.Fatalf("line with a line break: %q", l.Text)
					}
				}
			}
		}
	})
}

// FuzzBody fuzzes the XML of a document body rather than the archive
// around it: mutating compressed bytes almost always just breaks the zip,
// so FuzzRead rarely reaches the walker with interesting XML.
func FuzzBody(f *testing.F) {
	for _, s := range []string{revisions, tables, textBox, item("1", "1", "x") + styled("Heading1", "y"),
		`<w:p><w:r><w:fldChar w:fldCharType="begin"/><w:t>x</w:t></w:r></w:p>`,
		`<w:tbl><w:tr><w:tc><w:p><w:pPr><w:rPr><w:del/></w:rPr></w:pPr></w:p></w:tc></w:tr></w:tbl>`} {
		f.Add(s, false)
	}
	extra := map[string]string{
		"word/numbering.xml": ooxmltest.Root("w:numbering", numberingXML),
		"word/styles.xml":    ooxmltest.Root("w:styles", stylesXML),
	}
	f.Fuzz(func(t *testing.T, body string, original bool) {
		data := ooxmltest.Docx(body, extra)
		pkg, err := ooxml.Open(data, limits)
		if err != nil {
			t.Fatal(err)
		}
		o := all
		o.Original, o.Depth = original, 64
		d, err := Read(pkg, "word/document.xml", o)
		if err != nil {
			return
		}
		for _, l := range d.Parts[0].Lines {
			if strings.ContainsAny(l.Text, "\r\n") {
				t.Fatalf("line with a line break: %q", l.Text)
			}
		}
	})
}
