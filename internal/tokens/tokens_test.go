package tokens

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func texts(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Hello, world!", []string{"Hello", ",", " ", "world", "!"}},
		{"a  b\t\tc", []string{"a", "  ", "b", "\t\t", "c"}},
		{"art. 3.2 (b)", []string{"art", ".", " ", "3", ".", "2", " ", "(", "b", ")"}},
		{"Ștefan și Țuțea", []string{"Ștefan", " ", "și", " ", "Țuțea"}},
		{"café ok", []string{"café", " ", "ok"}},               // e + combining acute stays in the word
		{" ́x", []string{" ", "́", "x"}},                         // a mark after space stands alone
		{"中文字", []string{"中", "文", "字"}},                         // no spaces between words: one token each
		{"東京2026年", []string{"東", "京", "2026", "年"}},             // digits still group
		{"ภาษาไทย", []string{"ภ", "า", "ษ", "า", "ไ", "ท", "ย"}}, // Thai: one per character
		{"한국어 단어", []string{"한국어", " ", "단어"}},                   // Hangul uses spaces
		{"می‌خواهم", []string{"می‌خواهم"}},                       // ZWNJ inside a Persian word
		{"👨‍👩‍👧!", []string{"👨‍👩‍👧", "!"}},                       // a ZWJ emoji sequence
		{"👍🏽👍", []string{"👍🏽", "👍"}},                             // skin tone modifier
		{"❤️", []string{"❤️"}},                                   // variation selector
		{"x y", []string{"x", " ", "y"}},                         // no-break space is space
		{"foo_bar", []string{"foo", "_", "bar"}},
	}
	for _, tt := range tests {
		got := texts(Split(nil, tt.in, Words, false))
		if strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
			t.Errorf("Split(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitChars(t *testing.T) {
	got := texts(Split(nil, "ab  cé́!", Chars, false))
	want := []string{"a", "b", "  ", "c", "é́", "!"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Split chars = %q, want %q", got, want)
	}
}

func TestKinds(t *testing.T) {
	toks := Split(nil, "a1 ,中", Words, false)
	want := []Kind{Word, Space, Punct, Ideograph}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens", len(toks))
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %q kind %d, want %d", toks[i].Text, toks[i].Kind, k)
		}
	}
}

func TestFold(t *testing.T) {
	same := [][]rune{
		{'a', 'A'},
		{'k', 'K', 'K'}, // Kelvin sign
		{'s', 'S', 'ſ'}, // long s
		{'ș', 'Ș'},
		{'ß', 'ẞ'},
		{'ͅ', 'ι', 'Ι', 'ι'},
	}
	for _, orbit := range same {
		f := Fold(orbit[0])
		for _, r := range orbit[1:] {
			if Fold(r) != f {
				t.Errorf("Fold(%U) = %U, Fold(%U) = %U", orbit[0], f, r, Fold(r))
			}
		}
		if !unicode.IsLetter(f) {
			t.Errorf("Fold(%U) = %U is not a letter", orbit[0], f)
		}
	}
	if Fold('ß') == Fold('s') {
		t.Error("ß must not fold to s: simple folding keeps it apart from ss")
	}
}

// TestFoldMatchesEqualFold checks Fold against strings.EqualFold over the
// whole Basic Multilingual Plane and beyond: two runes fold to the same
// representative exactly when EqualFold calls them equal.
func TestFoldMatchesEqualFold(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r < 0xE000 {
			continue
		}
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if Fold(f) != Fold(r) {
				t.Fatalf("Fold(%U) = %U but Fold(%U) = %U", r, Fold(r), f, Fold(f))
			}
		}
	}
}

func TestLineKey(t *testing.T) {
	tests := []struct {
		n    Norm
		in   string
		want string
	}{
		{Norm{}, " A  b ", " A  b "},
		{Norm{Case: true}, "Straße", "STRAßE"}, // ß is the least letter of its orbit {ß, ẞ}
		{Norm{SpaceChange: true}, "  a \t b  ", " a b"},
		{Norm{AllSpace: true}, " a b\tc ", "abc"},
		{Norm{TrailingSpace: true}, " a  \t", " a"},
		{Norm{Case: true, SpaceChange: true}, "Hello   World ", "HELLO WORLD"},
	}
	for _, tt := range tests {
		if got := string(tt.n.LineKey(nil, tt.in)); got != tt.want {
			t.Errorf("%+v LineKey(%q) = %q, want %q", tt.n, tt.in, got, tt.want)
		}
	}
}

var norms = []Norm{
	{}, {Case: true}, {SpaceChange: true}, {AllSpace: true}, {TrailingSpace: true},
	{Case: true, SpaceChange: true}, {Case: true, AllSpace: true}, {Case: true, TrailingSpace: true},
}

func FuzzSplit(f *testing.F) {
	for _, s := range []string{"", "Hello, world!", " ́x", "中文 字", "👨‍👩", "café", "a‌b‍", "ͅι!"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			s = strings.ToValidUTF8(s, "�")
		}
		for _, mode := range []Mode{Words, Chars} {
			for _, fold := range []bool{false, true} {
				toks := Split(nil, s, mode, fold)
				if mode == Words {
					words := 0
					for _, tok := range toks {
						if tok.Kind.IsWord() {
							words++
						}
					}
					if c := CountWords(s, fold); c != words {
						t.Fatalf("CountWords(%q) = %d, Split has %d words", s, c, words)
					}
				}
				var b strings.Builder
				for i, tok := range toks {
					if tok.Text == "" {
						t.Fatalf("empty token in %q", s)
					}
					if tok.Kind == Space && strings.TrimFunc(tok.Text, IsSpace) != "" {
						t.Fatalf("space token %q holds non-space", tok.Text)
					}
					if tok.Kind == Space && i > 0 && toks[i-1].Kind == Space {
						t.Fatalf("two space tokens in a row in %q", s)
					}
					b.WriteString(tok.Text)
				}
				if b.String() != s {
					t.Fatalf("tokens of %q join to %q", s, b.String())
				}
			}
		}
	})
}

// FuzzKeys checks that tokens and lines agree on equality under every
// option: the keyed tokens of a line, joined, are the line's key. Without
// this, a line could be "changed" with nothing highlighted in it, or the
// other way round.
func FuzzKeys(f *testing.F) {
	for _, s := range []string{"", " a  b ", "A b\t", "Straße ", "ͅx !", "x ́ "} {
		f.Add(s, "a")
	}
	f.Fuzz(func(t *testing.T, s, other string) {
		s = strings.ToValidUTF8(s, "�")
		other = strings.ToValidUTF8(other, "�")
		for _, n := range norms {
			for _, mode := range []Mode{Words, Chars} {
				toks := Split(nil, s, mode, n.Case)
				var joined []byte
				for i, tok := range toks {
					if !n.Skip(toks, i) {
						joined = n.TokenKey(joined, tok)
					}
				}
				if line := n.LineKey(nil, s); string(line) != string(joined) {
					t.Fatalf("%+v mode %d: LineKey(%q) = %q, tokens give %q", n, mode, s, line, joined)
				}
			}
		}
		// Folding must decide equality exactly as strings.EqualFold does.
		n := Norm{Case: true}
		if eq := string(n.LineKey(nil, s)) == string(n.LineKey(nil, other)); eq != strings.EqualFold(s, other) {
			t.Fatalf("folded keys of %q and %q equal=%v, EqualFold=%v", s, other, eq, !eq)
		}
	})
}
