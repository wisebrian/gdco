package gdco

import (
	"errors"
	"strings"
	"testing"
)

// checkResult verifies the invariants a UI relies on, for a result made
// with the whole document in one hunk: the rows give back both documents
// line for line, spans give back their lines, the statistics count the
// rows, and Equal agrees with them.
func checkResult(t *testing.T, r *Result, a, b *Document) {
	t.Helper()
	if len(r.Parts) != len(a.parts) && len(r.Parts) != len(b.parts) {
		t.Fatalf("%d parts for documents of %d and %d", len(r.Parts), len(a.parts), len(b.parts))
	}
	var total Stats
	equal := true
	for pi, p := range r.Parts {
		var la, lb []string
		var st Stats
		for _, h := range p.Hunks {
			na, nb := h.A.Start, h.B.Start
			for _, row := range h.Rows {
				switch row.Op {
				case OpEqual, OpModify:
					if row.A == nil || row.B == nil {
						t.Fatalf("%s row without both sides", row.Op)
					}
				case OpDelete:
					if row.A == nil || row.B != nil {
						t.Fatal("delete row with a B side")
					}
				case OpInsert:
					if row.B == nil || row.A != nil {
						t.Fatal("insert row with an A side")
					}
				default:
					t.Fatalf("unknown op %q", row.Op)
				}
				if row.A != nil {
					if row.A.N != na {
						t.Fatalf("A line %d where %d was due", row.A.N, na)
					}
					na++
					la = append(la, row.A.Text)
					checkSpans(t, row.A, row.Op, OpDelete)
				}
				if row.B != nil {
					if row.B.N != nb {
						t.Fatalf("B line %d where %d was due", row.B.N, nb)
					}
					nb++
					lb = append(lb, row.B.Text)
					checkSpans(t, row.B, row.Op, OpInsert)
				}
				if row.Op != OpEqual && !row.Ignored {
					equal = false
					switch row.Op {
					case OpInsert:
						st.LinesAdded++
					case OpDelete:
						st.LinesRemoved++
					case OpModify:
						st.LinesChanged++
					}
				}
			}
			if na-h.A.Start != h.A.Count || nb-h.B.Start != h.B.Count {
				t.Fatalf("hunk ranges %+v %+v do not match its rows", h.A, h.B)
			}
		}
		if pi < len(a.parts) && pi < len(b.parts) {
			if want := texts(a.parts[pi]); strings.Join(la, "\n") != want {
				t.Fatalf("rows give A %q, want %q", strings.Join(la, "\n"), want)
			}
			if want := texts(b.parts[pi]); strings.Join(lb, "\n") != want {
				t.Fatalf("rows give B %q, want %q", strings.Join(lb, "\n"), want)
			}
		}
		if st.LinesAdded != p.Stats.LinesAdded || st.LinesRemoved != p.Stats.LinesRemoved || st.LinesChanged != p.Stats.LinesChanged {
			t.Fatalf("part stats %+v, rows count %+v", p.Stats, st)
		}
		total.add(p.Stats)
	}
	if total != r.Stats {
		t.Fatalf("result stats %+v, parts sum to %+v", r.Stats, total)
	}
	if equal != r.Equal {
		t.Fatalf("Equal = %v, rows say %v", r.Equal, equal)
	}
}

func texts(p *part) string {
	s := make([]string, len(p.lines))
	for i, l := range p.lines {
		s[i] = l.text
	}
	return strings.Join(s, "\n")
}

func checkSpans(t *testing.T, l *Line, op, changed Op) {
	t.Helper()
	if op != OpModify {
		if len(l.Spans) != 0 {
			t.Fatalf("%s row has spans", op)
		}
		return
	}
	var b strings.Builder
	for _, s := range l.Spans {
		if s.Op != OpEqual && s.Op != changed {
			t.Fatalf("span %q with op %s on the wrong side", s.Text, s.Op)
		}
		b.WriteString(s.Text)
	}
	if len(l.Spans) > 0 && b.String() != l.Text {
		t.Fatalf("spans join to %q, line is %q", b.String(), l.Text)
	}
}

func optionsFrom(bits uint16) *Options {
	o := DefaultOptions()
	o.Context = -1
	o.IgnoreCase = bits&1 != 0
	o.IgnoreSpaceChange = bits&2 != 0
	o.IgnoreAllSpace = bits&4 != 0
	o.IgnoreTrailingSpace = bits&8 != 0
	o.IgnoreBlankLines = bits&16 != 0
	o.Granularity = []Granularity{ByWord, ByChar, ByLine}[int(bits>>5)%3]
	return o
}

func FuzzCompare(f *testing.F) {
	f.Add("a\nb\nc\n", "a\nB\nc\nd", uint16(0))
	f.Add("x  y\r\n\r\n", "x y\n", uint16(2|16))
	f.Add("Straße\n", "STRASSE\n", uint16(1))
	f.Add("", "\n\n", uint16(16))
	f.Fuzz(func(t *testing.T, a, b string, bits uint16) {
		o := optionsFrom(bits)
		da, db := TextDocument("a", a), TextDocument("b", b)
		r, err := Compare(da, db, o)
		if err != nil {
			t.Fatal(err)
		}
		checkResult(t, r, da, db)
		var sb strings.Builder
		if err := r.WriteUnified(&sb); err != nil {
			t.Fatal(err)
		}
		if (sb.Len() == 0) != r.Equal {
			t.Fatalf("unified diff empty=%v but Equal=%v", sb.Len() == 0, r.Equal)
		}
		if err := r.WriteHTML(&sb, nil); err != nil {
			t.Fatal(err)
		}
		// Without options, equality is byte equality once line endings are
		// made the same.
		if bits&31 == 0 {
			lf := strings.NewReplacer("\r\n", "\n", "\r", "\n")
			if same := lf.Replace(da.parts[0].text()) == lf.Replace(db.parts[0].text()); r.Equal != same {
				t.Fatalf("Equal=%v for %q and %q", r.Equal, a, b)
			}
		}
	})
}

func (p *part) text() string {
	var b strings.Builder
	for _, l := range p.lines {
		b.WriteString(l.text)
		b.WriteString(l.eol.String())
	}
	return b.String()
}

func FuzzRead(f *testing.F) {
	for _, name := range []string{"contract-v1.docx", "contract-v2.docx", "word-features.docx"} {
		f.Add(fixture(f, name).Data)
	}
	for _, name := range []string{"budget-v1.xlsx", "budget-v2.xlsx", "excel-features.xlsx"} {
		f.Add(workbook(f, name).Data)
	}
	for _, s := range []string{"", "a\n", "\xEF\xBB\xBFa", "\xFF\xFEa\x00", "caf\xE9", "%PDF-1", "PK\x03\x04", "a\x00"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Read(File{Name: "f", Data: data}, nil)
		if errors.Is(err, ErrInternal) {
			t.Fatal(err)
		}
		if err != nil {
			return
		}
		r, err := Compare(d, d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !r.Equal {
			t.Fatal("a document differs from itself")
		}
	})
}
