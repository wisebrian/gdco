package textenc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
		enc  Encoding
		err  error
	}{
		{"empty", nil, "", UTF8, nil},
		{"ascii", []byte("hello\n"), "hello\n", UTF8, nil},
		{"utf8", []byte("șțăîâ €"), "șțăîâ €", UTF8, nil},
		{"utf8 bom", []byte("\xEF\xBB\xBFabc"), "abc", UTF8BOM, nil},
		{"bom only", []byte("\xEF\xBB\xBF"), "", UTF8BOM, nil},
		{"bom then invalid", []byte("\xEF\xBB\xBFa\xFFb"), "a�b", UTF8BOM, nil},
		{"utf16le", []byte("\xFF\xFEa\x00\x19\x02"), "aș", UTF16LE, nil},
		{"utf16be", []byte("\xFE\xFF\x00a\x02\x19"), "aș", UTF16BE, nil},
		{"utf16le surrogate pair", []byte("\xFF\xFE\x3D\xD8\x00\xDE"), "😀", UTF16LE, nil},
		{"utf16le lone high", []byte("\xFF\xFE\x3D\xD8a\x00"), "�a", UTF16LE, nil},
		{"utf16le lone low", []byte("\xFF\xFE\x00\xDEa\x00"), "�a", UTF16LE, nil},
		{"utf16le high at end", []byte("\xFF\xFEa\x00\x3D\xD8"), "a�", UTF16LE, nil},
		{"utf16le odd byte", []byte("\xFF\xFEa\x00b"), "a�", UTF16LE, nil},
		{"utf16 nul", []byte("\xFF\xFEa\x00\x00\x00"), "", UTF16LE, ErrBinary},
		{"utf32le looks like utf16 nul", []byte("\xFF\xFE\x00\x00a\x00\x00\x00"), "", UTF16LE, ErrBinary},
		{"nul", []byte("a\x00b"), "", UTF8, ErrBinary},
		{"bom nul", []byte("\xEF\xBB\xBFa\x00"), "", UTF8BOM, ErrBinary},
		{"1252", []byte("caf\xE9 \x80 \x93q\x94"), "café € “q”", Windows1252, nil},
		{"1252 undefined", []byte("\x81\x8D\x8F\x90\x9D"), "\u0081\u008D\u008F\u0090\u009D", Windows1252, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, enc, err := Decode(tt.in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if got != tt.want || enc != tt.enc {
				t.Errorf("Decode(%q) = %q, %v; want %q, %v", tt.in, got, enc, tt.want, tt.enc)
			}
		})
	}
}

// TestWindows1252Table checks every byte above 0x7F against the WHATWG
// index, written out here independently of the table in decode.go.
func TestWindows1252Table(t *testing.T) {
	want := map[byte]rune{
		0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
		0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž',
		0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
		0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ',
		0x81: 0x81, 0x8D: 0x8D, 0x8F: 0x8F, 0x90: 0x90, 0x9D: 0x9D,
	}
	for c := 0x80; c <= 0xFF; c++ {
		// A lone byte above 0x7F is never valid UTF-8, so it takes the
		// Windows-1252 path.
		got, enc, err := Decode([]byte{byte(c)})
		if err != nil || enc != Windows1252 {
			t.Fatalf("byte %#x: enc %v, err %v", c, enc, err)
		}
		r, _ := utf8.DecodeRuneInString(got)
		w, ok := want[byte(c)]
		if !ok {
			w = rune(c) // 0xA0 to 0xFF are Latin-1
		}
		if r != w {
			t.Errorf("byte %#x = %U, want %U", c, r, w)
		}
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		in    string
		lines []string
		eols  []EOL
	}{
		{"", []string{}, []EOL{}},
		{"a", []string{"a"}, []EOL{None}},
		{"a\n", []string{"a"}, []EOL{LF}},
		{"a\r\n", []string{"a"}, []EOL{CRLF}},
		{"a\r", []string{"a"}, []EOL{CR}},
		{"\n", []string{""}, []EOL{LF}},
		{"\n\n", []string{"", ""}, []EOL{LF, LF}},
		{"a\rb\nc\r\nd", []string{"a", "b", "c", "d"}, []EOL{CR, LF, CRLF, None}},
		{"a\r\r\n", []string{"a", ""}, []EOL{CR, CRLF}},
		{"a\n\rb", []string{"a", "", "b"}, []EOL{LF, CR, None}},
	}
	for _, tt := range tests {
		lines, eols := Split(tt.in)
		if strings.Join(lines, "|") != strings.Join(tt.lines, "|") || len(lines) != len(tt.lines) {
			t.Errorf("Split(%q) lines = %q, want %q", tt.in, lines, tt.lines)
		}
		if len(eols) != len(tt.eols) {
			t.Errorf("Split(%q) eols = %v, want %v", tt.in, eols, tt.eols)
			continue
		}
		for i := range eols {
			if eols[i] != tt.eols[i] {
				t.Errorf("Split(%q) eols = %v, want %v", tt.in, eols, tt.eols)
				break
			}
		}
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		eols []EOL
		want Ending
	}{
		{nil, EndingNone},
		{[]EOL{None}, EndingNone},
		{[]EOL{LF, LF, None}, EndingLF},
		{[]EOL{CRLF}, EndingCRLF},
		{[]EOL{CR, CR}, EndingCR},
		{[]EOL{LF, CRLF}, EndingMixed},
	}
	for _, tt := range tests {
		if got := Summarize(tt.eols); got != tt.want {
			t.Errorf("Summarize(%v) = %v, want %v", tt.eols, got, tt.want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range []string{"", "abc", "\xEF\xBB\xBFx", "\xFF\xFEa\x00", "\xFE\xFF\x00a", "caf\xE9", "a\x00", "\xFF\xFE\x3D\xD8"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, enc, err := Decode(b)
		if err != nil {
			if !errors.Is(err, ErrBinary) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		if !utf8.ValidString(s) {
			t.Fatalf("Decode(%q) = %q: not valid UTF-8", b, s)
		}
		if strings.IndexByte(s, 0) >= 0 {
			t.Fatalf("Decode(%q) kept a NUL", b)
		}
		// Valid UTF-8 without a mark comes back unchanged: decoding must
		// never alter text that needed no decoding.
		if enc == UTF8 && s != string(b) {
			t.Fatalf("Decode(%q) changed UTF-8 text to %q", b, s)
		}
		if enc == Windows1252 && utf8.Valid(b) {
			t.Fatalf("valid UTF-8 %q decoded as Windows-1252", b)
		}
	})
}

func FuzzSplit(f *testing.F) {
	for _, s := range []string{"", "a", "a\n", "a\r\nb\rc\n", "\r\r\n\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		lines, eols := Split(s)
		if len(lines) != len(eols) {
			t.Fatalf("%d lines, %d terminators", len(lines), len(eols))
		}
		var b bytes.Buffer
		for i, l := range lines {
			if strings.ContainsAny(l, "\r\n") {
				t.Fatalf("line %q holds a terminator", l)
			}
			if eols[i] == None && i != len(lines)-1 {
				t.Fatalf("line %d of %d has no terminator", i, len(lines))
			}
			b.WriteString(l)
			b.WriteString(eols[i].String())
		}
		if b.String() != s {
			t.Fatalf("joined lines %q != input %q", b.String(), s)
		}
	})
}
