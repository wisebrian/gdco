package docx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

var limits = ooxml.Limits{Entries: 1000, Part: 64 << 20, Total: 256 << 20, Ratio: 1000, Depth: 256}

var all = Options{Headers: true, Footers: true, Footnotes: true, Endnotes: true, Comments: true, Depth: 256}

func read(t *testing.T, data []byte, o Options) *Doc {
	t.Helper()
	pkg, err := ooxml.Open(data, limits)
	if err != nil {
		t.Fatal(err)
	}
	main, _, err := pkg.MainPart()
	if err != nil {
		t.Fatal(err)
	}
	d, err := Read(pkg, main, o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// show renders lines as "location|marker|text", the form expectations are
// written in.
func show(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		var loc []string
		add := func(k string, v int) {
			if v != 0 {
				loc = append(loc, fmt.Sprintf("%s%d", k, v))
			}
		}
		add("pg", l.Loc.Page)
		add("i", l.Loc.Item)
		add("tb", l.Loc.TextBox)
		add("t", l.Loc.Table)
		add("r", l.Loc.Row)
		add("p", l.Loc.Paragraph)
		if l.Loc.Label != "" {
			loc = append(loc, l.Loc.Label)
		}
		m := l.Marker
		if l.Shape != "" {
			m += "(" + l.Shape + ")"
		}
		out[i] = strings.Join(loc, " ") + "|" + m + "|" + l.Text
	}
	return out
}

func expect(t *testing.T, what string, got []Line, want ...string) {
	t.Helper()
	g := show(got)
	if strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot\n  %s\nwant\n  %s", what, strings.Join(g, "\n  "), strings.Join(want, "\n  "))
	}
}

func body(t *testing.T, xml string, o Options) []Line {
	t.Helper()
	return read(t, ooxmltest.Docx(xml, nil), o).Parts[0].Lines
}

func TestRuns(t *testing.T) {
	got := body(t, `
<w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t xml:space="preserve"> world</w:t></w:r><w:r><w:tab/><w:t>tab</w:t></w:r></w:p>
<w:p><w:r><w:t>line one</w:t><w:br/><w:t>line two</w:t><w:cr/><w:t>three</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:b/><w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs></w:rPr><w:t>non</w:t><w:noBreakHyphen/><w:t>breaking soft</w:t><w:softHyphen/><w:t>hyphen</w:t><w:sym w:font="Symbol" w:char="F0B7"/><w:sym w:font="Wingdings" w:char="F0FC"/><w:sym w:font="Symbol" w:char="0061"/></w:r></w:p>
<w:p><w:r><w:t>  trimmed  </w:t></w:r></w:p>
<w:p/>`, all)
	expect(t, "runs", got,
		"p1||Hello world\ttab",
		"p2||line one", "p2||line two", "p2||three",
		"p3||non-breaking softhyphen•✓α",
		"p4||trimmed",
		"p5||")
}

func TestContainersAndFields(t *testing.T) {
	got := body(t, `
<w:p><w:hyperlink r:id="x"><w:r><w:t>link</w:t></w:r></w:hyperlink><w:sdt><w:sdtPr><w:alias w:val="hidden"/></w:sdtPr><w:sdtContent><w:r><w:t xml:space="preserve"> control</w:t></w:r></w:sdtContent></w:sdt></w:p>
<w:sdt><w:sdtPr/><w:sdtContent><w:p><w:r><w:t>block control</w:t></w:r></w:p></w:sdtContent></w:sdt>
<w:customXml w:element="x"><w:p><w:smartTag w:element="y"><w:smartTagPr/><w:r><w:t>tagged</w:t></w:r></w:smartTag></w:p></w:customXml>
<w:p><w:r><w:t xml:space="preserve">Page </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>7</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:fldSimple w:instr="NUMPAGES"><w:r><w:t xml:space="preserve"> of 9</w:t></w:r></w:fldSimple></w:p>
<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>IF </w:instrText></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>MERGEFIELD x</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>inner</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>outer result</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>
<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>TOC \o</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>Entry one</w:t></w:r></w:p>
<w:p><w:r><w:t>Entry two</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>
<w:p><w:r><w:t>E</w:t></w:r><m:oMath><m:r><m:t>=mc</m:t></m:r><m:sSup><m:e><m:r><m:t>2</m:t></m:r></m:e></m:sSup></m:oMath></w:p>
<w:p><w:ruby><w:rubyPr/><w:rt><w:r><w:t>kan</w:t></w:r></w:rt><w:rubyBase><w:r><w:t>漢</w:t></w:r></w:rubyBase></w:ruby></w:p>`, all)
	expect(t, "containers", got,
		"p1||link control", "p2||block control", "p3||tagged", "p4||Page 7 of 9",
		"p5||outer result", "p6||Entry one", "p7||Entry two", "p8||E=mc2", "p9||漢")
}

const textBox = `<w:p><w:r><w:t>Before</w:t></w:r><w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:anchor><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp><wps:txbx><w:txbxContent><w:p><w:r><w:t>In the box</w:t></w:r></w:p><w:p><w:r><w:t>Second box line</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:anchor></w:drawing></mc:Choice><mc:Fallback><w:pict><v:shape><v:textbox><w:txbxContent><w:p><w:r><w:t>In the box</w:t></w:r></w:p><w:p><w:r><w:t>Second box line</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></mc:Fallback></mc:AlternateContent></w:r><w:r><w:t xml:space="preserve"> after</w:t></w:r></w:p>`

func TestTextBoxReadOnce(t *testing.T) {
	got := body(t, textBox+`<w:p><w:r><w:t>Next</w:t></w:r></w:p>
<w:p><w:r><mc:AlternateContent><mc:Fallback><w:pict><v:textbox><w:txbxContent><w:p><w:r><w:t>only a fallback</w:t></w:r></w:p></w:txbxContent></v:textbox></w:pict></mc:Fallback></mc:AlternateContent></w:r></w:p>`, all)
	expect(t, "text boxes", got,
		"p1||Before after", "tb1 p1||In the box", "tb1 p2||Second box line", "p2||Next",
		"p3||", "tb2 p1||only a fallback")
}

const tables = `<w:tbl><w:tblPr/><w:tblGrid/><w:tr><w:tc><w:tcPr/><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Qty</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>Apples</w:t></w:r></w:p><w:p><w:r><w:t>green</w:t></w:r></w:p></w:tc><w:tc><w:tbl><w:tr><w:tc><w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>y</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:tc></w:tr>
<w:tr><w:trPr><w:del w:id="9" w:author="a"/></w:trPr><w:tc><w:p><w:r><w:t>Deleted row</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p/></w:tc><w:tc><w:p><w:r><w:t>5</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
<w:p><w:r><w:t>After table</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>second table</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`

func TestTables(t *testing.T) {
	expect(t, "accepted", body(t, tables, all),
		"t1 r1||Name | Qty", "t1 r2||Apples green | x | y", "t1 r3|| | 5", "p1||After table", "t2 r1||second table")
	o := all
	o.Original = true
	expect(t, "original", body(t, tables, o),
		"t1 r1||Name | Qty", "t1 r2||Apples green | x | y", "t1 r3||Deleted row", "t1 r4|| | 5", "p1||After table", "t2 r1||second table")
}

const revisions = `<w:p><w:r><w:t xml:space="preserve">The fee is </w:t></w:r><w:del w:id="1" w:author="A"><w:r><w:delText>30</w:delText></w:r></w:del><w:ins w:id="2" w:author="A"><w:r><w:t>45</w:t></w:r></w:ins><w:r><w:t xml:space="preserve"> days.</w:t></w:r></w:p>
<w:p><w:pPr><w:rPr><w:del w:id="3" w:author="A"/></w:rPr></w:pPr><w:del w:id="4"><w:r><w:delText>A removed paragraph.</w:delText></w:r></w:del></w:p>
<w:p><w:pPr><w:rPr><w:del w:id="5"/></w:rPr></w:pPr><w:r><w:t xml:space="preserve">Joined </w:t></w:r></w:p>
<w:p><w:r><w:t>with next.</w:t></w:r></w:p>
<w:p><w:moveFrom w:id="6"><w:r><w:t>moved away</w:t></w:r></w:moveFrom><w:moveTo w:id="7"><w:r><w:t>moved here</w:t></w:r></w:moveTo></w:p>
<w:p><w:pPr><w:rPr><w:ins w:id="8"/></w:rPr></w:pPr><w:ins w:id="9"><w:r><w:t>New paragraph.</w:t></w:r></w:ins></w:p>
<w:p><w:r><w:t>Last.</w:t></w:r></w:p>`

func TestTrackedChanges(t *testing.T) {
	d := read(t, ooxmltest.Docx(revisions, nil), all)
	if !d.Revisions {
		t.Error("revisions not reported")
	}
	expect(t, "accepted", d.Parts[0].Lines,
		"p1||The fee is 45 days.", "p2||Joined with next.", "p3||moved here", "p4||New paragraph.", "p5||Last.")
	o := all
	o.Original = true
	expect(t, "original", body(t, revisions, o),
		"p1||The fee is 30 days.", "p2||A removed paragraph.", "p3||Joined ", "p4||with next.", "p5||moved away", "p6||Last.")
}

const numberingXML = `<w:abstractNum w:abstractNumId="0">
<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1.%2."/></w:lvl>
<w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="(%3)"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="` + "\uF0B7" + `"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="2"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="Article %1"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>
<w:num w:numId="3"><w:abstractNumId w:val="2"/></w:num>
<w:num w:numId="4"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`

const stylesXML = `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:basedOn w:val="Normal"/><w:pPr><w:numPr><w:numId w:val="3"/></w:numPr></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1Sub"><w:basedOn w:val="Heading1"/></w:style>`

func item(numID, ilvl, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` + numID + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func styled(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func TestNumbering(t *testing.T) {
	doc := item("1", "0", "First") + item("1", "1", "Sub one") + item("1", "1", "Sub two") + item("1", "2", "deep") +
		item("1", "0", "Second") + item("1", "1", "Sub again") + item("2", "0", "bullet") +
		styled("Heading1", "Scope") + styled("Heading1Sub", "Terms") +
		`<w:p><w:pPr><w:pStyle w:val="Heading1"/><w:numPr><w:numId w:val="0"/></w:numPr></w:pPr><w:r><w:t>Not numbered</w:t></w:r></w:p>` +
		item("4", "0", "Restarted") + item("1", "0", "Third")
	d := read(t, ooxmltest.Docx(doc, map[string]string{
		"word/numbering.xml": ooxmltest.Root("w:numbering", numberingXML),
		"word/styles.xml":    ooxmltest.Root("w:styles", stylesXML),
	}), all)
	expect(t, "numbering", d.Parts[0].Lines,
		"p1|1.(#0)|First", "p2|1.1.(#1)|Sub one", "p3|1.2.(#1)|Sub two", "p4|(a)(#2)|deep",
		"p5|2.(#0)|Second", "p6|2.1.(#1)|Sub again", "p7|•(•0)|bullet",
		"p8|Article I(#0)|Scope", "p9|Article II(#0)|Terms", "p10||Not numbered",
		"p11|1.(#0)|Restarted", "p12|3.(#0)|Third")
}

func TestFormats(t *testing.T) {
	n := newNumbering()
	for _, c := range []struct {
		v      int
		format string
		want   string
	}{
		{4, "upperRoman", "IV"}, {1994, "lowerRoman", "mcmxciv"}, {1, "upperLetter", "A"},
		{27, "lowerLetter", "aa"}, {3, "decimalZero", "03"}, {2, "ordinal", "2nd"}, {13, "ordinal", "13th"},
	} {
		if got := n.format(c.v, c.format); got != c.want {
			t.Errorf("format(%d, %s) = %q, want %q", c.v, c.format, got, c.want)
		}
	}
	if n.plain {
		t.Error("known formats marked plain")
	}
	if n.format(5, "chineseCounting") != "5" || !n.plain {
		t.Error("an unknown format must fall back to decimal and say so")
	}
}

func TestStoriesAndNotes(t *testing.T) {
	body := `<w:p><w:r><w:t>Body text</w:t></w:r><w:r><w:footnoteReference w:id="2"/></w:r><w:r><w:t xml:space="preserve"> and</w:t></w:r><w:r><w:footnoteReference w:id="1"/></w:r><w:commentRangeStart w:id="7"/><w:r><w:t xml:space="preserve"> more</w:t></w:r><w:commentRangeEnd w:id="7"/><w:r><w:commentReference w:id="7"/></w:r><w:r><w:endnoteReference w:id="1"/></w:r></w:p>
<w:sectPr><w:headerReference w:type="default" r:id="r_header1"/><w:headerReference w:type="first" r:id="r_header2"/><w:footerReference w:type="default" r:id="r_footer1"/></w:sectPr>`
	note := func(id, typ, text string) string {
		t := ""
		if typ != "" {
			t = ` w:type="` + typ + `"`
		}
		return `<w:footnote w:id="` + id + `"` + t + `><w:p><w:r><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> ` + text + `</w:t></w:r></w:p></w:footnote>`
	}
	d := read(t, ooxmltest.Docx(body, map[string]string{
		"word/header1.xml":   ooxmltest.Root("w:hdr", `<w:p><w:r><w:t>Default header</w:t></w:r></w:p>`),
		"word/header2.xml":   ooxmltest.Root("w:hdr", `<w:p><w:r><w:t>First page header</w:t></w:r></w:p>`),
		"word/header3.xml":   ooxmltest.Root("w:hdr", `<w:p><w:r><w:t>Never used</w:t></w:r></w:p>`),
		"word/footer1.xml":   ooxmltest.Root("w:ftr", `<w:p><w:r><w:t>Footer</w:t></w:r></w:p>`),
		"word/footnotes.xml": ooxmltest.Root("w:footnotes", note("-1", "separator", "")+note("0", "continuationSeparator", "")+note("1", "", "First note")+note("2", "", "Second note")+note("3", "", "Unreferenced")),
		"word/endnotes.xml":  ooxmltest.Root("w:endnotes", `<w:endnote w:id="1"><w:p><w:r><w:t>End note</w:t></w:r></w:p></w:endnote>`),
		"word/comments.xml":  ooxmltest.Root("w:comments", `<w:comment w:id="7" w:author="Ana" w:date="2026-01-01T00:00:00Z"><w:p><w:r><w:annotationRef/></w:r><w:r><w:t>Check this</w:t></w:r></w:p></w:comment>`),
	}), all)
	kinds := []string{}
	for _, p := range d.Parts {
		kinds = append(kinds, p.Kind)
	}
	if strings.Join(kinds, ",") != "body,headers,footers,footnotes,endnotes,comments" {
		t.Fatalf("parts %v", kinds)
	}
	expect(t, "body", d.Parts[0].Lines, "p1||Body text[footnote] and[footnote] more[endnote]")
	expect(t, "headers", d.Parts[1].Lines, "i1 p1||Default header", "i2 p1 first page||First page header")
	expect(t, "footers", d.Parts[2].Lines, "i1 p1||Footer")
	expect(t, "footnotes", d.Parts[3].Lines, "i1 p1||Second note", "i2 p1||First note")
	expect(t, "endnotes", d.Parts[4].Lines, "i1 p1||End note")
	expect(t, "comments", d.Parts[5].Lines, "i1 p1||Ana: Check this")

	o := all
	o.Headers, o.Comments = false, false
	if d := read(t, ooxmltest.Docx(body, map[string]string{"word/header1.xml": ooxmltest.Root("w:hdr", `<w:p/>`)}), o); len(d.Parts) != 1 {
		t.Errorf("parts left out still read: %d", len(d.Parts))
	}
}

func TestPages(t *testing.T) {
	got := body(t, `<w:p><w:r><w:t>page one</w:t></w:r></w:p>
<w:p><w:r><w:lastRenderedPageBreak/><w:t>page two</w:t></w:r></w:p>
<w:p><w:r><w:t>still two</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:lastRenderedPageBreak/><w:t>three</w:t></w:r></w:p>`, all)
	expect(t, "pages", got, "pg1 p1||page one", "pg2 p2||page two", "pg2 p3||still two", "pg3 p3||three")
	got = body(t, `<w:p><w:r><w:t>one</w:t><w:br w:type="page"/><w:t>two</w:t></w:r></w:p>`, all)
	expect(t, "no rendered layout", got, "p1||one", "p1||two")
}

func TestStrictAndUTF16(t *testing.T) {
	doc := ooxmltest.Root("w:document", `<w:body><w:p><w:r><w:t>strict ș</w:t></w:r></w:p></w:body>`)
	doc = strings.Replace(doc, "http://schemas.openxmlformats.org/wordprocessingml/2006/main", "http://purl.oclc.org/ooxml/wordprocessingml/main", 1)
	utf16 := utf16le(strings.Replace(ooxmltest.Root("w:document", `<w:body><w:p><w:r><w:t>utf-16 ț</w:t></w:r></w:p></w:body>`), `encoding="UTF-8"`, `encoding="UTF-16"`, 1))
	for name, part := range map[string]string{"strict": doc, "utf-16": utf16} {
		data := replacePart(ooxmltest.Docx("", nil), "word/document.xml", part)
		lines := read(t, data, all).Parts[0].Lines
		if len(lines) != 1 || !strings.HasPrefix(lines[0].Text, name) {
			t.Errorf("%s: %v", name, show(lines))
		}
	}
}

func utf16le(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		if r > 0xFFFF {
			panic("test text outside the BMP")
		}
		b = append(b, byte(r), byte(r>>8))
	}
	return string(b)
}

// replacePart rebuilds an archive with one part's content replaced.
func replacePart(data []byte, name, content string) []byte {
	pkg, err := ooxml.Open(data, limits)
	if err != nil {
		panic(err)
	}
	var parts []ooxmltest.Part
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "word/_rels/document.xml.rels", "word/document.xml"} {
		b, err := pkg.Read(n)
		if err != nil {
			panic(err)
		}
		if n == name {
			b = []byte(content)
		}
		parts = append(parts, ooxmltest.Part{Name: n, Data: string(b)})
	}
	return ooxmltest.Zip(parts...)
}

// TestManyReferences reads a body with a hundred thousand footnote
// references to a few notes: keeping their order must stay linear.
func TestManyReferences(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<w:p>`)
	for i := range 100_000 {
		fmt.Fprintf(&b, `<w:r><w:footnoteReference w:id="%d"/></w:r>`, i)
	}
	b.WriteString(`</w:p>`)
	start := time.Now()
	d := read(t, ooxmltest.Docx(b.String(), map[string]string{
		"word/footnotes.xml": ooxmltest.Root("w:footnotes", `<w:footnote w:id="5"><w:p><w:r><w:t>five</w:t></w:r></w:p></w:footnote>`),
	}), all)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v", took)
	}
	if n := len(d.Parts); n != 2 || d.Parts[1].Lines[0].Text != "five" {
		t.Errorf("parts %+v", d.Parts)
	}
	if got := letters(1_000_000_000); got != "1000000000" {
		t.Errorf("letters of a huge number: %d characters", len(got))
	}
}
