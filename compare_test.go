package gdco

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// unified returns the unified diff of a result, failing the test on error.
func unified(t *testing.T, r *Result) string {
	t.Helper()
	var b strings.Builder
	if err := r.WriteUnified(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func opts(f func(o *Options)) *Options {
	o := DefaultOptions()
	if f != nil {
		f(o)
	}
	return o
}

func TestCompareText(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		o       *Options
		equal   bool
		diff    string
		stats   Stats
		notes   []string
		partSts PartStatus
	}{
		{name: "both empty", a: "", b: "", equal: true, partSts: StatusSame},
		{name: "identical", a: "x\ny\n", b: "x\ny\n", equal: true, partSts: StatusSame},
		{name: "insert only", a: "", b: "one\ntwo\n",
			diff:  "--- a\n+++ b\n@@ -0,0 +1,2 @@\n+one\n+two\n",
			stats: Stats{LinesAdded: 2, WordsAdded: 2}, partSts: StatusChanged},
		{name: "delete only", a: "one\ntwo\n", b: "",
			diff:  "--- a\n+++ b\n@@ -1,2 +0,0 @@\n-one\n-two\n",
			stats: Stats{LinesRemoved: 2, WordsRemoved: 2}, partSts: StatusChanged},
		{name: "single lines without newline", a: "old", b: "new",
			diff:  "--- a\n+++ b\n@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n",
			stats: Stats{LinesChanged: 1, WordsAdded: 1, WordsRemoved: 1}, partSts: StatusChanged},
		{name: "newline added at end", a: "a\nb", b: "a\nb\n",
			diff:  "--- a\n+++ b\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n",
			stats: Stats{LinesChanged: 1}, partSts: StatusChanged},
		{name: "newline ignored with trailing space option", a: "a\nb", b: "a\nb\n",
			o: opts(func(o *Options) { o.IgnoreTrailingSpace = true }), equal: true, partSts: StatusSame},
		{name: "line endings only", a: "a\r\nb\r\n", b: "a\nb\n", equal: true, partSts: StatusSame,
			notes: []string{"line-endings"}},
		{name: "modified word", a: "The fee is 30 days.\nend\n", b: "The fee is 45 days.\nend\n",
			diff:  "--- a\n+++ b\n@@ -1,2 +1,2 @@\n-The fee is 30 days.\n+The fee is 45 days.\n end\n",
			stats: Stats{LinesChanged: 1, WordsAdded: 1, WordsRemoved: 1}, partSts: StatusChanged},
		{name: "ignore case", a: "Hello World\n", b: "hello WORLD\n",
			o: opts(func(o *Options) { o.IgnoreCase = true }), equal: true, partSts: StatusSame},
		{name: "ignore all space", a: "a b  c\n", b: "abc\n",
			o: opts(func(o *Options) { o.IgnoreAllSpace = true }), equal: true, partSts: StatusSame},
		{name: "ignore space change", a: "a  b \n", b: "a b\n",
			o: opts(func(o *Options) { o.IgnoreSpaceChange = true }), equal: true, partSts: StatusSame},
		{name: "space change is not all space", a: "ab\n", b: "a b\n",
			o:     opts(func(o *Options) { o.IgnoreSpaceChange = true }),
			diff:  "--- a\n+++ b\n@@ -1 +1 @@\n-ab\n+a b\n",
			stats: Stats{LinesChanged: 1, WordsAdded: 2, WordsRemoved: 1}, partSts: StatusChanged},
		{name: "blank lines ignored", a: "a\nb\n", b: "a\n\n\nb\n",
			o: opts(func(o *Options) { o.IgnoreBlankLines = true }), equal: true, partSts: StatusSame},
		{name: "blank lines counted", a: "a\nb\n", b: "a\n\nb\n",
			diff:  "--- a\n+++ b\n@@ -1,2 +1,3 @@\n a\n+\n b\n",
			stats: Stats{LinesAdded: 1}, partSts: StatusChanged},
		{name: "by line shows delete and insert", a: "x 1\n", b: "x 2\n",
			o:     opts(func(o *Options) { o.Granularity = ByLine }),
			diff:  "--- a\n+++ b\n@@ -1 +1 @@\n-x 1\n+x 2\n",
			stats: Stats{LinesAdded: 1, LinesRemoved: 1, WordsAdded: 2, WordsRemoved: 2}, partSts: StatusChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := CompareText(tt.a, tt.b, tt.o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Equal != tt.equal {
				t.Errorf("Equal = %v, want %v", r.Equal, tt.equal)
			}
			if got := unified(t, r); got != tt.diff {
				t.Errorf("unified diff:\n%s\nwant:\n%s", got, tt.diff)
			}
			if r.Stats != tt.stats {
				t.Errorf("stats %+v, want %+v", r.Stats, tt.stats)
			}
			var codes []string
			for _, n := range r.Notes {
				codes = append(codes, n.Code)
			}
			if strings.Join(codes, ",") != strings.Join(tt.notes, ",") {
				t.Errorf("notes %v, want %v", codes, tt.notes)
			}
			if len(r.Parts) != 1 || r.Parts[0].Status != tt.partSts {
				t.Errorf("parts %+v, want one with status %s", r.Parts, tt.partSts)
			}
			if r.Approximate {
				t.Error("approximate")
			}
		})
	}
}

func TestModifyRowSpans(t *testing.T) {
	r, err := CompareText("The Supplier shall deliver within 30 days.\n", "The Supplier must deliver within 45 days.\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	row := r.Parts[0].Hunks[0].Rows[0]
	if row.Op != OpModify || row.A == nil || row.B == nil {
		t.Fatalf("row %+v, want a modify row with both sides", row)
	}
	show := func(sp []Span) string {
		var b strings.Builder
		for _, s := range sp {
			switch s.Op {
			case OpDelete:
				b.WriteString("[-" + s.Text + "-]")
			case OpInsert:
				b.WriteString("{+" + s.Text + "+}")
			default:
				b.WriteString(s.Text)
			}
		}
		return b.String()
	}
	if got, want := show(row.A.Spans), "The Supplier [-shall-] deliver within [-30-] days."; got != want {
		t.Errorf("A spans %q, want %q", got, want)
	}
	if got, want := show(row.B.Spans), "The Supplier {+must+} deliver within {+45+} days."; got != want {
		t.Errorf("B spans %q, want %q", got, want)
	}
}

func TestContext(t *testing.T) {
	// Distinct lines, so that the alignment is unambiguous.
	var a, b []string
	for i := range 30 {
		a = append(a, "line "+strconv.Itoa(i))
		b = append(b, "line "+strconv.Itoa(i))
	}
	b[5], b[12], b[25] = "line x", "line y", "line z"
	ta, tb := strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n"
	count := func(ctx int) []Hunk {
		r, err := CompareText(ta, tb, opts(func(o *Options) { o.Context = ctx }))
		if err != nil {
			t.Fatal(err)
		}
		return r.Parts[0].Hunks
	}
	// Changes at lines 6 and 13 are 6 lines apart: with 3 lines of context
	// their hunks touch and merge; line 26 is far enough to stand alone.
	if h := count(3); len(h) != 2 || h[0].A != (Range{3, 14}) || h[1].A != (Range{23, 7}) {
		t.Errorf("context 3: %d hunks %+v", len(h), ranges(h))
	}
	if h := count(0); len(h) != 3 || h[0].A != (Range{6, 1}) {
		t.Errorf("context 0: %+v", ranges(h))
	}
	if h := count(-1); len(h) != 1 || h[0].A != (Range{1, 30}) || len(h[0].Rows) != 30 {
		t.Errorf("whole document: %+v", ranges(h))
	}
}

func ranges(hs []Hunk) [][2]Range {
	var out [][2]Range
	for _, h := range hs {
		out = append(out, [2]Range{h.A, h.B})
	}
	return out
}

func TestWholeDocumentWhenEqual(t *testing.T) {
	r, err := CompareText("a\nb\n", "a\nb\n", opts(func(o *Options) { o.Context = -1 }))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Equal || len(r.Parts[0].Hunks) != 1 || len(r.Parts[0].Hunks[0].Rows) != 2 {
		t.Fatalf("equal documents in full view: %+v", r.Parts[0].Hunks)
	}
	if d := unified(t, r); d != "" {
		t.Errorf("unified diff of equal documents %q, want empty", d)
	}
}

func TestSegments(t *testing.T) {
	a := SegmentDocument("a.pdf", []Segment{{Text: "Intro\nTerms", Page: 1}, {Text: "Price: 100", Page: 2, Label: "ii"}})
	b := SegmentDocument("b.pdf", []Segment{{Text: "Intro\nTerms", Page: 1}, {Text: "Price: 120", Page: 2, Label: "ii"}})
	r, err := Compare(a, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mod *Row
	for _, h := range r.Parts[0].Hunks {
		for i := range h.Rows {
			if h.Rows[i].Op == OpModify {
				mod = &h.Rows[i]
			}
		}
	}
	if mod == nil {
		t.Fatal("no modified row")
	}
	if got, want := mod.B.Loc, (Location{Page: 2, Label: "ii", Line: 1}); got != want {
		t.Errorf("location %+v, want %+v", got, want)
	}
	if got := mod.B.Loc.String(); got != "Page ii, line 1" {
		t.Errorf("location label %q", got)
	}
	if mod.A.NoNewline || mod.B.NoNewline {
		t.Error("segments have no end-of-file newline to report")
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name string
		data string
		enc  Encoding
		err  error
	}{
		{"utf8", "a\n", EncodingUTF8, nil},
		{"bom", "\xEF\xBB\xBFa\n", EncodingUTF8BOM, nil},
		{"utf16", "\xFF\xFEa\x00\n\x00", EncodingUTF16LE, nil},
		{"1252", "caf\xE9\n", EncodingWindows1252, nil},
		{"binary", "a\x00b", "", ErrBinary},
		{"pdf", "%PDF-1.7\n", "", ErrUnsupported},
		{"zip", "PK\x03\x04rest", "", ErrUnsupported},
		{"ole", "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1", "", ErrUnsupported},
		{"rtf", `{\rtf1\ansi}`, "", ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Read(File{Name: tt.name, Data: []byte(tt.data)}, nil)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err %v, want %v", err, tt.err)
			}
			if err == nil && d.encoding != tt.enc {
				t.Errorf("encoding %s, want %s", d.encoding, tt.enc)
			}
		})
	}
}

func TestEncodingNote(t *testing.T) {
	r, err := CompareFiles(File{"a.txt", []byte("café\n")}, File{"b.txt", []byte("caf\xE9\n")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Equal || len(r.Notes) != 1 || r.Notes[0].Code != "encoding" {
		t.Errorf("equal=%v notes=%+v", r.Equal, r.Notes)
	}
	if r.A.Encoding != EncodingUTF8 || r.B.Encoding != EncodingWindows1252 {
		t.Errorf("encodings %s %s", r.A.Encoding, r.B.Encoding)
	}
}

func TestLimits(t *testing.T) {
	o := opts(func(o *Options) { o.Limits.MaxFileSize = 4 })
	if _, err := Read(File{"big", []byte("12345")}, o); !errors.Is(err, ErrLimit) {
		t.Errorf("file size: %v", err)
	}
	o = opts(func(o *Options) { o.Limits.MaxLines = 2 })
	if _, err := CompareText("a\nb\nc\n", "", o); !errors.Is(err, ErrLimit) {
		t.Errorf("lines: %v", err)
	}
	o = opts(func(o *Options) { o.Limits.MaxLines = -1 })
	if _, err := CompareText("", "", o); !errors.Is(err, ErrOptions) {
		t.Errorf("negative limit: %v", err)
	}
	o = opts(func(o *Options) { o.Granularity = "sentence" })
	if _, err := CompareText("", "", o); !errors.Is(err, ErrOptions) {
		t.Errorf("unknown granularity: %v", err)
	}
}

func TestNilDocuments(t *testing.T) {
	r, err := Compare(nil, TextDocument("b", "x\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stats.LinesAdded != 1 {
		t.Errorf("stats %+v", r.Stats)
	}
	var d *Document
	if d.Name() != "" || d.Format() != FormatText {
		t.Error("nil document accessors")
	}
}

func TestLocationString(t *testing.T) {
	tests := []struct {
		l    Location
		want string
	}{
		{Location{}, ""},
		{Location{Page: 3, Line: 12}, "Page 3, line 12"},
		{Location{Table: 2, Row: 3}, "Table 2, row 3"},
		{Location{Label: "Annex 2"}, "Annex 2"},
		{Location{TextBox: 1, Paragraph: 2}, "Text box 1, paragraph 2"},
	}
	for _, tt := range tests {
		if got := tt.l.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.l, got, tt.want)
		}
	}
	if got := describe(PartFootnotes, Location{Item: 2, Paragraph: 1}); got != "Footnote 2, paragraph 1" {
		t.Errorf("describe = %q", got)
	}
}
