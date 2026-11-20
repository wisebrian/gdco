package docx

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

var words = strings.Fields(`the supplier shall deliver goods within thirty days of order and buyer
will pay invoice amount net price contract party agreement terms clause notice written consent
liability damages termination period renewal annex schedule services fees confidential law`)

// largeDocx builds a document of about 200 pages the way Word writes one:
// revision ids on every paragraph, formatting on every run, proofing marks
// and bookmarks, and a table every few pages. Its document.xml is about
// 5 MB.
func largeDocx() []byte {
	r := rand.New(rand.NewPCG(1, 2))
	var b strings.Builder
	run := func(text string) {
		fmt.Fprintf(&b, `<w:r w:rsidRPr="%08X"><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="ro-RO"/></w:rPr><w:t xml:space="preserve">%s </w:t></w:r>`, r.Uint32(), text)
	}
	sentence := func(n int) string {
		w := make([]string, n)
		for i := range w {
			w[i] = words[r.IntN(len(words))]
		}
		return strings.Join(w, " ")
	}
	for page := range 200 {
		for p := range 12 {
			fmt.Fprintf(&b, `<w:p w:rsidR="%08X" w:rsidRDefault="%08X"><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/><w:jc w:val="both"/><w:rPr><w:sz w:val="22"/></w:rPr></w:pPr>`, r.Uint32(), r.Uint32())
			if p == 0 && page > 0 {
				b.WriteString(`<w:r><w:lastRenderedPageBreak/></w:r>`)
			}
			for range 5 {
				run(sentence(8))
			}
			b.WriteString(`<w:proofErr w:type="spellStart"/><w:bookmarkStart w:id="0" w:name="_GoBack"/><w:bookmarkEnd w:id="0"/><w:proofErr w:type="spellEnd"/></w:p>`)
		}
		if page%5 == 0 {
			b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>`)
			for range 10 {
				b.WriteString(`<w:tr>`)
				for range 4 {
					b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr><w:p><w:pPr><w:spacing w:after="0"/></w:pPr>`)
					run(sentence(2))
					b.WriteString(`</w:p></w:tc>`)
				}
				b.WriteString(`</w:tr>`)
			}
			b.WriteString(`</w:tbl>`)
		}
	}
	return ooxmltest.Docx(b.String(), nil)
}

func extract(tb testing.TB, data []byte) *Doc {
	pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 1000, Part: 128 << 20, Total: 256 << 20, Ratio: 200, Depth: 256})
	if err != nil {
		tb.Fatal(err)
	}
	d, err := Read(pkg, "word/document.xml", all)
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// BenchmarkLargeDocument is the extraction target: a 200-page .docx read
// in under a second.
func BenchmarkLargeDocument(b *testing.B) {
	data := largeDocx()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		extract(b, data)
	}
}

func TestLargeDocumentIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	data := largeDocx()
	start := time.Now()
	d := extract(t, data)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("reading a 200-page document took %v", took)
	}
	lines := d.Parts[0].Lines
	if len(lines) != 200*12+40*10 || lines[len(lines)-1].Loc.Page != 200 {
		t.Errorf("%d lines, last on page %d", len(lines), lines[len(lines)-1].Loc.Page)
	}
}
