// Package tokens splits a line into the units a word-level comparison works
// on, and builds the comparison keys that make the ignore-case and
// ignore-white-space options mean the same thing for whole lines and for
// single words.
package tokens

import (
	"unicode"
	"unicode/utf8"
)

// Kind classifies a token.
type Kind uint8

// The kinds of token. Ideograph is kept apart from Word because Chinese,
// Japanese, Thai and a few other scripts do not separate words with
// spaces: a whole sentence would otherwise be one "word", and changing one
// character would highlight all of it.
const (
	Space     Kind = iota // a run of white space
	Word                  // letters and digits, with the marks that combine with them
	Ideograph             // one character of a script written without spaces
	Punct                 // one punctuation mark or symbol
)

// IsWord reports whether a token of this kind counts as a word in
// statistics.
func (k Kind) IsWord() bool { return k == Word || k == Ideograph }

// Token is a piece of a line. The tokens of a line, concatenated, give the
// line back exactly.
type Token struct {
	Text string
	Kind Kind
}

// Mode chooses how finely Split cuts.
type Mode uint8

// The two granularities. Chars still keeps a run of white space as one
// token, so that "ignore changes in white space" can treat runs of
// different lengths as equal in both modes.
const (
	Words Mode = iota
	Chars
)

const (
	zwnj = '‌'
	zwj  = '‍'
)

// Split appends the tokens of s to dst.
//
// With fold set, characters are classified by their case-folded form, so
// two lines that are equal ignoring case also split at the same places and
// can be compared token by token.
func Split(dst []Token, s string, mode Mode, fold bool) []Token {
	var (
		start int
		cur   Kind
		open  bool // a token is in progress
		grow  bool // it absorbs following characters of its own kind
		join  bool // the last character was a zero-width joiner
	)
	for i, r := range s {
		c := r
		if fold {
			c = Fold(r)
		}
		if open {
			if (join && !IsSpace(c)) || extends(c, cur) {
				join = c == zwj
				continue
			}
			if grow && class(c) == cur {
				continue
			}
			dst = append(dst, Token{s[start:i], cur})
		}
		start, open, join = i, true, c == zwj
		cur = class(c)
		if isMark(c) || c == zwj || c == zwnj {
			cur = Punct // a combining character with nothing to combine with
		}
		grow = cur == Space || (cur == Word && mode == Words)
	}
	if open {
		dst = append(dst, Token{s[start:], cur})
	}
	return dst
}

// CountWords returns the number of word tokens Split would give for s in
// Words mode, without building them: statistics count the words of every
// inserted and deleted line, and two unrelated files have many.
func CountWords(s string, fold bool) int {
	n := 0
	var (
		cur  Kind
		open bool
		join bool
	)
	for _, r := range s {
		c := r
		if fold {
			c = Fold(r)
		}
		if open {
			if (join && !IsSpace(c)) || extends(c, cur) {
				join = c == zwj
				continue
			}
			if (cur == Space || cur == Word) && class(c) == cur {
				continue
			}
		}
		open, join = true, c == zwj
		cur = class(c)
		if isMark(c) || c == zwj || c == zwnj {
			cur = Punct
		}
		if cur.IsWord() {
			n++
		}
	}
	return n
}

// extends reports whether c attaches to the token before it instead of
// starting one. Combining marks belong to the character before them, so "é"
// written as e plus U+0301 stays one character; the zero-width joiners sit
// inside words in Indic scripts and Persian, and inside emoji sequences
// together with skin-tone modifiers.
func extends(c rune, cur Kind) bool {
	switch {
	case cur == Space:
		return false
	case isMark(c), c == zwj:
		return true
	case c == zwnj:
		return cur == Word
	case 0x1F3FB <= c && c <= 0x1F3FF:
		return cur == Punct
	}
	return false
}

func isMark(c rune) bool {
	return c >= 0x300 && unicode.Is(unicode.Mark, c)
}

// class gives the kind of token c starts. Only letters and digits make
// words, as their Unicode categories say; anything else that is not white
// space stands alone.
func class(c rune) Kind {
	if c < utf8.RuneSelf {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
			return Word
		case c == ' ', '\t' <= c && c <= '\r':
			return Space
		}
		return Punct
	}
	switch {
	case unicode.IsSpace(c):
		return Space
	case unicode.IsLetter(c) || unicode.IsDigit(c):
		if c >= 0x0E00 && unicode.In(c, unicode.Han, unicode.Hiragana, unicode.Katakana,
			unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar) {
			return Ideograph
		}
		return Word
	}
	return Punct
}

// IsSpace is the white-space test that keys and tokens share. It is
// unicode.IsSpace, so a no-break space counts as space: Word inserts them
// freely and a reader cannot tell them from ordinary spaces.
func IsSpace(r rune) bool {
	if r < utf8.RuneSelf {
		return r == ' ' || '\t' <= r && r <= '\r'
	}
	return unicode.IsSpace(r)
}
