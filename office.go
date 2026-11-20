package gdco

import (
	"errors"
	"fmt"
	"strings"

	"github.com/wisebrian/gdco/internal/docx"
	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/textenc"
)

// readOffice reads a zip archive as an Office Open XML package. The kind of
// document is decided by the content type of its main part, so that a
// .docm or a .dotx reads like a .docx and a renamed file reads as what it
// is.
func readOffice(f File, o *Options) (*Document, error) {
	name := display(f.Name)
	pkg, err := ooxml.Open(f.Data, ooxml.Limits{
		Entries: o.Limits.MaxZipEntries,
		Part:    o.Limits.MaxPartSize,
		Total:   o.Limits.MaxTotalSize,
		Ratio:   o.Limits.MaxRatio,
		Depth:   o.Limits.MaxXMLDepth,
	})
	if err != nil {
		return nil, officeErr(name, err)
	}
	main, ct, err := pkg.MainPart()
	if err != nil {
		if pkg.Has("mimetype") && pkg.Has("content.xml") {
			return nil, fmt.Errorf("%w: %s is an OpenDocument file; save it as .docx or .xlsx first", ErrUnsupported, name)
		}
		return nil, fmt.Errorf("%w: %s is a zip archive, not a Word or Excel file", ErrUnsupported, name)
	}
	kind := officeKind(main, ct)
	switch kind {
	case "word":
		return readWord(f.Name, pkg, main, o)
	case "excel":
		return nil, fmt.Errorf("%w: %s is an Excel workbook, which is not supported yet", ErrUnsupported, name)
	case "powerpoint":
		return nil, fmt.Errorf("%w: %s is a PowerPoint presentation", ErrUnsupported, name)
	}
	return nil, fmt.Errorf("%w: %s is an Office package of an unknown kind (%s)", ErrUnsupported, name, ct)
}

func officeKind(main, contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "wordprocessingml"), strings.Contains(ct, "ms-word"):
		return "word"
	case strings.Contains(ct, "spreadsheetml"), strings.Contains(ct, "ms-excel"):
		return "excel"
	case strings.Contains(ct, "presentationml"), strings.Contains(ct, "ms-powerpoint"):
		return "powerpoint"
	case ct == "" && strings.HasPrefix(strings.ToLower(main), "word/"):
		return "word"
	case ct == "" && strings.HasPrefix(strings.ToLower(main), "xl/"):
		return "excel"
	}
	return ""
}

func officeErr(name string, err error) error {
	switch {
	case errors.Is(err, ooxml.ErrLimit):
		return fmt.Errorf("%w: %s: %v", ErrLimit, name, err)
	case errors.Is(err, ooxml.ErrUnsupported):
		return fmt.Errorf("%w: %s: %v", ErrUnsupported, name, err)
	}
	return fmt.Errorf("%w: %s: %v", ErrMalformed, name, err)
}

func readWord(fileName string, pkg *ooxml.Package, main string, o *Options) (*Document, error) {
	name := display(fileName)
	doc, err := docx.Read(pkg, main, docx.Options{
		Original:  o.Tracked == TrackedOriginal,
		Headers:   o.Headers,
		Footers:   o.Footers,
		Footnotes: o.Footnotes,
		Endnotes:  o.Endnotes,
		Comments:  o.Comments,
		Depth:     o.Limits.MaxXMLDepth,
	})
	if err != nil {
		return nil, officeErr(name, err)
	}
	d := &Document{name: valid(fileName), format: FormatDocx}
	for _, p := range doc.Parts {
		pt := &part{kind: PartKind(p.Kind), lines: make([]line, len(p.Lines))}
		extras := make([]lineExtra, len(p.Lines))
		for i, l := range p.Lines {
			extras[i] = lineExtra{marker: l.Marker, shape: l.Shape, loc: Location{
				Page: l.Loc.Page, Item: l.Loc.Item, TextBox: l.Loc.TextBox, Paragraph: l.Loc.Paragraph,
				Table: l.Loc.Table, Row: l.Loc.Row, Label: l.Loc.Label,
			}}
			pt.lines[i] = line{text: valid(l.Text), eol: textenc.LF, x: &extras[i]}
		}
		d.parts = append(d.parts, pt)
	}
	if n := d.lineCount(); n > o.Limits.MaxLines {
		return nil, fmt.Errorf("%w: %s has %d lines, more than %d", ErrLimit, name, n, o.Limits.MaxLines)
	}
	if doc.Revisions {
		view := "all of them accepted"
		if o.Tracked == TrackedOriginal {
			view = "all of them rejected, as it was before them"
		}
		d.notes = append(d.notes, Note{Code: "tracked-changes", Message: "The document has tracked changes; it was compared with " + view + "."})
	}
	if len(doc.Skipped) > 0 {
		d.notes = append(d.notes, Note{Code: "skipped-content", Message: "Not compared: " + strings.Join(doc.Skipped, ", ") + "."})
	}
	if doc.Plain {
		d.notes = append(d.notes, Note{Code: "numbering", Message: "Some list numbers use a style this tool does not render; they are shown as plain numbers."})
	}
	return d, nil
}
