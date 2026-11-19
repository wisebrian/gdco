package gdco

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/wisebrian/gdco/internal/textenc"
)

// File is an input whose format is not known yet. The format is decided
// from Data, not from Name, because visitors rename files.
type File struct {
	Name string
	Data []byte
}

// Segment is text extracted elsewhere, typically one PDF page, so that the
// output can say "page 4" about text this package never parsed.
type Segment struct {
	Text  string
	Page  int    // 1-based; 0 when the segment is not a page
	Label string // shown as it is, such as "iv" or "Annex 2"
}

// Document is one side of a comparison: parts (the body, headers, sheets)
// made of lines that know where they came from. It is opaque so that a
// 100 000-line text costs a few bytes per line, and so that run formatting
// can be added later without breaking callers.
type Document struct {
	name     string
	format   Format
	encoding Encoding
	ending   LineEnding
	parts    []*part
	notes    []Note
}

type part struct {
	kind  PartKind
	name  string
	lines []line
}

// line is kept small, a string and two words, because a document may have
// a million of them; what only some lines have lives behind x.
type line struct {
	text string
	eol  textenc.EOL
	x    *lineExtra
}

type lineExtra struct {
	marker string // shown before the text, not compared
	shape  string // what is compared instead of the marker
	loc    Location
}

// Name returns the name used in report headers and in the "---" and "+++"
// lines of a unified diff.
func (d *Document) Name() string {
	if d == nil {
		return ""
	}
	return d.name
}

// Format reports how the document was read, so a UI can label it.
func (d *Document) Format() Format {
	if d == nil {
		return FormatText
	}
	return d.format
}

func (d *Document) lineCount() int {
	n := 0
	for _, p := range d.parts {
		n += len(p.lines)
	}
	return n
}

// TextDocument wraps text that is already a string, such as a textarea's
// contents. It strips a leading byte order mark and replaces invalid UTF-8
// instead of failing: a string carries no encoding to detect.
func TextDocument(name, text string) *Document {
	name = valid(name)
	text = strings.TrimPrefix(text, "\uFEFF")
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	return textDocument(name, text)
}

func textDocument(name, text string) *Document {
	lines, eols := textenc.Split(text)
	p := &part{kind: PartBody, lines: make([]line, len(lines))}
	for i := range lines {
		p.lines[i] = line{text: lines[i], eol: eols[i]}
	}
	return &Document{
		name:   name,
		format: FormatText,
		ending: LineEnding(textenc.Summarize(eols).String()),
		parts:  []*part{p},
	}
}

// SegmentDocument splits each segment into lines that keep its page and
// label. This is how text from the separate PDF module comes in.
func SegmentDocument(name string, segs []Segment) *Document {
	name = valid(name)
	p := &part{kind: PartBody}
	for _, sg := range segs {
		text := sg.Text
		if !utf8.ValidString(text) {
			text = strings.ToValidUTF8(text, "�")
		}
		lines, _ := textenc.Split(text)
		for i, l := range lines {
			p.lines = append(p.lines, line{
				text: l,
				eol:  textenc.LF,
				x:    &lineExtra{loc: Location{Page: max(sg.Page, 0), Label: sg.Label, Line: i + 1}},
			})
		}
	}
	return &Document{name: name, format: FormatSegments, parts: []*part{p}}
}

// Read extracts a .docx or .xlsx file, recognised by its zip contents, and
// decodes anything else as text. Reading once lets a UI compare again with
// different options without parsing again.
func Read(f File, o *Options) (d *Document, err error) {
	defer guard(&err)
	opts, err := o.resolve()
	if err != nil {
		return nil, err
	}
	if int64(len(f.Data)) > opts.Limits.MaxFileSize {
		return nil, fmt.Errorf("%w: %s is %d bytes, more than %d", ErrLimit, display(f.Name), len(f.Data), opts.Limits.MaxFileSize)
	}
	if err := sniff(f); err != nil {
		return nil, err
	}
	s, enc, err := textenc.Decode(f.Data)
	if err != nil {
		if errors.Is(err, textenc.ErrBinary) {
			return nil, fmt.Errorf("%w: %s contains NUL bytes", ErrBinary, display(f.Name))
		}
		return nil, err
	}
	d = textDocument(valid(f.Name), s)
	d.encoding = Encoding(enc.String())
	if n := d.lineCount(); n > opts.Limits.MaxLines {
		return nil, fmt.Errorf("%w: %s has %d lines, more than %d", ErrLimit, display(f.Name), n, opts.Limits.MaxLines)
	}
	return d, nil
}

// sniff rejects, by their signatures, the formats that would otherwise be
// decoded as text and compared as noise.
func sniff(f File) error {
	b := f.Data
	name := display(f.Name)
	switch {
	case bytes.HasPrefix(b, []byte("PK\x03\x04")), bytes.HasPrefix(b, []byte("PK\x05\x06")):
		return fmt.Errorf("%w: %s is a zip archive; Word and Excel files are not supported yet", ErrUnsupported, name)
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return fmt.Errorf("%w: %s is a PDF; extract its text first", ErrUnsupported, name)
	case bytes.HasPrefix(b, []byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1")):
		return fmt.Errorf("%w: %s is a legacy .doc or .xls file, or an encrypted Office file", ErrUnsupported, name)
	case bytes.HasPrefix(b, []byte(`{\rtf`)):
		return fmt.Errorf("%w: %s is an RTF document", ErrUnsupported, name)
	}
	return nil
}

// display names an input in error messages, which are read by people.
func display(name string) string {
	if name == "" {
		return "the input"
	}
	return fmt.Sprintf("%q", name)
}

// valid repairs a string from the caller that is not UTF-8, as names and
// segment texts may be, so that everything written from it is.
func valid(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}
