package gdco

import (
	"io"
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/textenc"
)

// WriteUnified writes the result as a unified diff, the format of diff -u
// and git, which patch applies to A's decoded text. Each line keeps its own
// line ending, so patch rebuilds B byte for byte when both files use the
// same endings; differences in line endings alone are not in the diff,
// since they are not compared.
//
// Parts other than a single body each get their own "---"/"+++" header,
// named after the part, as if they were separate files.
func (r *Result) WriteUnified(w io.Writer) (err error) {
	defer guard(&err)
	o := &out{w: w}
	single := len(r.Parts) == 1 && r.Parts[0].Kind == PartBody
	for _, p := range r.Parts {
		if !p.changed() {
			continue
		}
		na, nb := headerName(r.A.Name, "a"), headerName(r.B.Name, "b")
		if !single {
			na += " (" + partLabel(p) + ")"
			nb += " (" + partLabel(p) + ")"
		}
		o.str("--- " + na + "\n+++ " + nb + "\n")
		for _, h := range p.Hunks {
			if h.changed() {
				writeHunk(o, h)
			}
		}
	}
	o.flush()
	return o.err
}

func writeHunk(o *out, h Hunk) {
	o.str("@@ -" + unifiedRange(h.A) + " +" + unifiedRange(h.B) + " @@\n")
	// Within a run of changes, every removed line comes before every
	// added one, as diff prints them.
	var minus, plus []*Line
	flush := func() {
		for _, l := range minus {
			unifiedLine(o, '-', l)
		}
		for _, l := range plus {
			unifiedLine(o, '+', l)
		}
		minus, plus = minus[:0], plus[:0]
	}
	for _, row := range h.Rows {
		// An unchanged line that ends one file without a newline but not
		// the other (only possible when white space is ignored) cannot be
		// context: the marker after it would apply to both files.
		if row.Op == OpEqual && row.A.NoNewline == row.B.NoNewline {
			flush()
			unifiedLine(o, ' ', row.A)
			continue
		}
		if row.A != nil {
			minus = append(minus, row.A)
		}
		if row.B != nil {
			plus = append(plus, row.B)
		}
	}
	flush()
}

func unifiedLine(o *out, prefix byte, l *Line) {
	o.byte(prefix)
	text := l.Text
	if l.Marker != "" {
		// The list number is part of what the reader sees, though it is not
		// compared.
		text = l.Marker + " " + text
	}
	if strings.ContainsAny(text, "\r\n") {
		// Lines are split at every terminator, so this is only a safety
		// net: a raw newline here would corrupt the diff.
		text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	}
	o.str(text)
	switch l.eol {
	case textenc.CRLF:
		o.str("\r\n")
	case textenc.None:
		o.str("\n\\ No newline at end of file\n")
	default:
		// LF, and CR, which patch could not split lines at anyway.
		o.byte('\n')
	}
}

// unifiedRange follows GNU diff: a count of one is left out, and an empty
// range names the line before it.
func unifiedRange(r Range) string {
	switch r.Count {
	case 0:
		return strconv.Itoa(r.Start-1) + ",0"
	case 1:
		return strconv.Itoa(r.Start)
	}
	return strconv.Itoa(r.Start) + "," + strconv.Itoa(r.Count)
}

// headerName makes a name safe for a "---" line: control characters would
// break the format, and patch stops reading a name at white space unless a
// tab ends it.
func headerName(name, fallback string) string {
	if name == "" {
		name = fallback
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return '?'
		}
		return r
	}, name)
	if strings.ContainsRune(name, ' ') {
		name += "\t"
	}
	return name
}

func partLabel(p PartDiff) string {
	switch p.Kind {
	case PartSheet:
		return "sheet " + p.Name
	case PartBody:
		return "body"
	}
	return string(p.Kind)
}

func (p PartDiff) changed() bool {
	for _, h := range p.Hunks {
		if h.changed() {
			return true
		}
	}
	return false
}

func (h Hunk) changed() bool {
	for _, r := range h.Rows {
		if r.Op != OpEqual && !r.Ignored {
			return true
		}
	}
	return false
}

// out buffers writes and keeps the first error, so the writers need no
// error check per line.
type out struct {
	w   io.Writer
	buf []byte
	err error
}

func (o *out) str(s string) {
	o.buf = append(o.buf, s...)
	if len(o.buf) >= 64<<10 {
		o.flush()
	}
}

func (o *out) byte(c byte) { o.buf = append(o.buf, c) }

func (o *out) flush() {
	if o.err == nil && len(o.buf) > 0 {
		_, o.err = o.w.Write(o.buf)
	}
	o.buf = o.buf[:0]
}
