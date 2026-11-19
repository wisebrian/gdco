package gdco

import (
	"strconv"
	"unicode/utf8"
)

// MarshalJSON encodes the result as encoding/json would from the struct
// tags, without encoding/json: its reflection costs the WebAssembly build
// about 1.8 MB, a third of the binary, to write a shape known in advance.
// A test checks that both encoders agree.
func (r *Result) MarshalJSON() (b []byte, err error) {
	defer guard(&err)
	j := jsonw{buf: make([]byte, 0, 4096)}
	j.result(r)
	return j.buf, nil
}

type jsonw struct{ buf []byte }

func (j *jsonw) raw(s string) { j.buf = append(j.buf, s...) }

// key writes a key with the separator that comes before it.
func (j *jsonw) key(first bool, k string) {
	if !first {
		j.buf = append(j.buf, ',')
	}
	j.buf = append(j.buf, '"')
	j.buf = append(j.buf, k...)
	j.buf = append(j.buf, '"', ':')
}

func (j *jsonw) int(n int) { j.buf = strconv.AppendInt(j.buf, int64(n), 10) }

func (j *jsonw) bool(v bool) { j.buf = strconv.AppendBool(j.buf, v) }

// str writes a JSON string. Text from documents is valid UTF-8 by the time
// it gets here, but invalid bytes still come out as U+FFFD rather than
// as invalid JSON.
func (j *jsonw) str(s string) {
	j.buf = append(j.buf, '"')
	last := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' && c < utf8.RuneSelf {
			i++
			continue
		}
		var esc string
		size := 1
		switch c {
		case '"':
			esc = `\"`
		case '\\':
			esc = `\\`
		case '\n':
			esc = `\n`
		case '\r':
			esc = `\r`
		case '\t':
			esc = `\t`
		default:
			if c < 0x20 {
				esc = `\u00` + string("0123456789abcdef"[c>>4]) + string("0123456789abcdef"[c&0xF])
				break
			}
			var r rune
			r, size = utf8.DecodeRuneInString(s[i:])
			if r != utf8.RuneError || size != 1 {
				i += size
				continue
			}
			esc = `�`
		}
		j.buf = append(j.buf, s[last:i]...)
		j.buf = append(j.buf, esc...)
		i += size
		last = i
	}
	j.buf = append(j.buf, s[last:]...)
	j.buf = append(j.buf, '"')
}

func (j *jsonw) result(r *Result) {
	j.raw("{")
	j.key(true, "a")
	j.side(&r.A)
	j.key(false, "b")
	j.side(&r.B)
	j.key(false, "equal")
	j.bool(r.Equal)
	j.key(false, "approximate")
	j.bool(r.Approximate)
	j.key(false, "parts")
	if r.Parts == nil {
		j.raw("null")
	} else {
		j.raw("[")
		for i := range r.Parts {
			if i > 0 {
				j.raw(",")
			}
			j.part(&r.Parts[i])
		}
		j.raw("]")
	}
	j.key(false, "stats")
	j.stats(&r.Stats)
	if len(r.Notes) > 0 {
		j.key(false, "notes")
		j.raw("[")
		for i, n := range r.Notes {
			if i > 0 {
				j.raw(",")
			}
			j.raw("{")
			j.key(true, "code")
			j.str(n.Code)
			if n.Side != "" {
				j.key(false, "side")
				j.str(n.Side)
			}
			j.key(false, "message")
			j.str(n.Message)
			j.raw("}")
		}
		j.raw("]")
	}
	j.raw("}")
}

func (j *jsonw) side(s *Side) {
	j.raw("{")
	j.key(true, "name")
	j.str(s.Name)
	j.key(false, "format")
	j.str(string(s.Format))
	if s.Encoding != "" {
		j.key(false, "encoding")
		j.str(string(s.Encoding))
	}
	if s.LineEnding != "" {
		j.key(false, "lineEnding")
		j.str(string(s.LineEnding))
	}
	j.key(false, "lines")
	j.int(s.Lines)
	j.raw("}")
}

func (j *jsonw) part(p *PartDiff) {
	j.raw("{")
	j.key(true, "kind")
	j.str(string(p.Kind))
	if p.Name != "" {
		j.key(false, "name")
		j.str(p.Name)
	}
	j.key(false, "status")
	j.str(string(p.Status))
	j.key(false, "hunks")
	if p.Hunks == nil {
		j.raw("null")
	} else {
		j.raw("[")
		for i := range p.Hunks {
			if i > 0 {
				j.raw(",")
			}
			j.hunk(&p.Hunks[i])
		}
		j.raw("]")
	}
	if len(p.Cells) > 0 {
		j.key(false, "cells")
		j.raw("[")
		for i, c := range p.Cells {
			if i > 0 {
				j.raw(",")
			}
			j.raw("{")
			first := true
			if c.A != "" {
				j.key(true, "a")
				j.str(c.A)
				first = false
			}
			if c.B != "" {
				j.key(first, "b")
				j.str(c.B)
				first = false
			}
			j.key(first, "old")
			j.str(c.Old)
			j.key(false, "new")
			j.str(c.New)
			j.raw("}")
		}
		j.raw("]")
	}
	j.key(false, "stats")
	j.stats(&p.Stats)
	j.raw("}")
}

func (j *jsonw) hunk(h *Hunk) {
	j.raw("{")
	j.key(true, "a")
	j.rng(h.A)
	j.key(false, "b")
	j.rng(h.B)
	j.key(false, "rows")
	if h.Rows == nil {
		j.raw("null")
	} else {
		j.raw("[")
		for i := range h.Rows {
			if i > 0 {
				j.raw(",")
			}
			row := &h.Rows[i]
			j.raw("{")
			j.key(true, "op")
			j.str(string(row.Op))
			if row.A != nil {
				j.key(false, "a")
				j.line(row.A)
			}
			if row.B != nil {
				j.key(false, "b")
				j.line(row.B)
			}
			if row.Ignored {
				j.key(false, "ignored")
				j.bool(true)
			}
			j.raw("}")
		}
		j.raw("]")
	}
	j.raw("}")
}

func (j *jsonw) rng(r Range) {
	j.raw("{")
	j.key(true, "start")
	j.int(r.Start)
	j.key(false, "count")
	j.int(r.Count)
	j.raw("}")
}

func (j *jsonw) line(l *Line) {
	j.raw("{")
	j.key(true, "n")
	j.int(l.N)
	j.key(false, "text")
	j.str(l.Text)
	if l.Marker != "" {
		j.key(false, "marker")
		j.str(l.Marker)
	}
	if l.Loc != (Location{}) {
		j.key(false, "loc")
		j.loc(&l.Loc)
	}
	if len(l.Spans) > 0 {
		j.key(false, "spans")
		j.raw("[")
		for i, s := range l.Spans {
			if i > 0 {
				j.raw(",")
			}
			j.raw("{")
			j.key(true, "op")
			j.str(string(s.Op))
			j.key(false, "text")
			j.str(s.Text)
			j.raw("}")
		}
		j.raw("]")
	}
	if l.NoNewline {
		j.key(false, "noNewline")
		j.bool(true)
	}
	j.raw("}")
}

func (j *jsonw) loc(l *Location) {
	j.raw("{")
	first := true
	num := func(k string, n int) {
		if n != 0 {
			j.key(first, k)
			j.int(n)
			first = false
		}
	}
	num("page", l.Page)
	num("item", l.Item)
	num("textBox", l.TextBox)
	num("paragraph", l.Paragraph)
	num("table", l.Table)
	num("row", l.Row)
	num("line", l.Line)
	if l.Label != "" {
		j.key(first, "label")
		j.str(l.Label)
	}
	j.raw("}")
}

func (j *jsonw) stats(s *Stats) {
	j.raw("{")
	j.key(true, "linesAdded")
	j.int(s.LinesAdded)
	j.key(false, "linesRemoved")
	j.int(s.LinesRemoved)
	j.key(false, "linesChanged")
	j.int(s.LinesChanged)
	j.key(false, "wordsAdded")
	j.int(s.WordsAdded)
	j.key(false, "wordsRemoved")
	j.int(s.WordsRemoved)
	j.key(false, "cellsChanged")
	j.int(s.CellsChanged)
	j.raw("}")
}
