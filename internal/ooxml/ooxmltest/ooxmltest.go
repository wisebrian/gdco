// Package ooxmltest builds Office packages for tests: exact XML is the only
// way to cover what Word writes but other programs rarely do, such as text
// boxes inside mc:AlternateContent or a deleted paragraph mark.
package ooxmltest

import (
	"archive/zip"
	"bytes"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Part is one entry of an archive.
type Part struct {
	Name, Data string
}

// Zip builds an archive with its parts in the order given, deflated.
func Zip(parts ...Part) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, p := range parts {
		w, err := zw.Create(p.Name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(p.Data)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// NS declares the namespaces Word documents use, Transitional variant.
const NS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml"`

// Root wraps content in a part's root element with the namespaces
// declared, such as Root("w:hdr", "<w:p>...</w:p>").
func Root(name, content string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n<" + name + " " + NS + ">" + content + "</" + name + ">"
}

var wordTypes = map[string]string{
	"header":    "header",
	"footer":    "footer",
	"footnotes": "footnotes",
	"endnotes":  "endnotes",
	"comments":  "comments",
	"numbering": "numbering",
	"styles":    "styles",
}

// Docx builds a Word document whose body is body, the XML inside w:body.
// extra holds more parts by name ("word/header1.xml"); those Word links
// from the document get a relationship whose id is "r_" and the file's base
// name ("r_header1"), so that sectPr can refer to them.
func Docx(body string, extra map[string]string) []byte {
	const rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	names := make([]string, 0, len(extra))
	for n := range extra {
		names = append(names, n)
	}
	sort.Strings(names)
	var rels, overrides strings.Builder
	for _, n := range names {
		base := strings.TrimSuffix(path.Base(n), ".xml")
		kind := strings.TrimRight(base, "0123456789")
		if typ, ok := wordTypes[kind]; ok && strings.HasPrefix(n, "word/") {
			rels.WriteString(`<Relationship Id="r_` + base + `" Type="` + rel + typ + `" Target="` + strings.TrimPrefix(n, "word/") + `"/>`)
		}
		overrides.WriteString(`<Override PartName="/` + n + `" ContentType="application/xml"/>`)
	}
	parts := []Part{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			overrides.String() + `</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + rel + `officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			rels.String() + `</Relationships>`},
		{"word/document.xml", Root("w:document", "<w:body>"+body+"</w:body>")},
	}
	for _, n := range names {
		parts = append(parts, Part{n, extra[n]})
	}
	return Zip(parts...)
}

// Xlsx builds a workbook with one sheet per argument, named S1, S2 and so
// on; each argument is the XML inside sheetData. strs, if any, are the
// shared strings, as plain text.
func Xlsx(strs []string, sheets ...string) []byte {
	const rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	const ns = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	var list, rels strings.Builder
	var parts []Part
	for i, s := range sheets {
		n := strconv.Itoa(i + 1)
		list.WriteString(`<sheet name="S` + n + `" sheetId="` + n + `" r:id="rs` + n + `"/>`)
		rels.WriteString(`<Relationship Id="rs` + n + `" Type="` + rel + `worksheet" Target="worksheets/sheet` + n + `.xml"/>`)
		parts = append(parts, Part{"xl/worksheets/sheet" + n + ".xml", `<worksheet ` + ns + `><sheetData>` + s + `</sheetData></worksheet>`})
	}
	if len(strs) > 0 {
		var sst strings.Builder
		for _, s := range strs {
			sst.WriteString("<si><t>" + s + "</t></si>")
		}
		rels.WriteString(`<Relationship Id="rss" Type="` + rel + `sharedStrings" Target="sharedStrings.xml"/>`)
		parts = append(parts, Part{"xl/sharedStrings.xml", `<sst ` + ns + `>` + sst.String() + `</sst>`})
	}
	return Zip(append([]Part{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="` + rel + `officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`},
		{"xl/workbook.xml", `<workbook ` + ns + `><sheets>` + list.String() + `</sheets></workbook>`},
	}, parts...)...)
}
