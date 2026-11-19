package tokens

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fold maps r to one representative of its simple case-folding orbit, the
// equivalence strings.EqualFold uses, so that comparing folded bytes gives
// the answer EqualFold would. The representative is a letter whenever the
// orbit has one: U+0345, a combining mark, shares an orbit with Greek iota,
// and folding must not turn letters into marks or words would split
// differently once case is ignored.
func Fold(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}
	least, letter := r, rune(-1)
	if unicode.IsLetter(r) {
		letter = r
	}
	// SimpleFold walks the orbit in increasing order and wraps around, so
	// the loop ends back at r after at most a few steps.
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < least {
			least = f
		}
		if (letter < 0 || f < letter) && unicode.IsLetter(f) {
			letter = f
		}
	}
	if letter >= 0 {
		return letter
	}
	return least
}

// Norm says which differences comparison keys ignore. The rules follow
// diff's -b, -w and -Z so results can be checked against diff and git.
type Norm struct {
	Case          bool // Unicode simple case folding
	SpaceChange   bool // runs of white space are equal, and white space at the end is ignored
	AllSpace      bool // white space is ignored entirely
	TrailingSpace bool // white space at the end of a line is ignored
}

// Plain reports whether keys are the text itself, so callers can skip
// building them.
func (n Norm) Plain() bool { return n == Norm{} }

// IgnoresSpace reports whether any white-space difference is ignored.
func (n Norm) IgnoresSpace() bool { return n.SpaceChange || n.AllSpace || n.TrailingSpace }

// LineKey appends the key of a whole line to dst.
func (n Norm) LineKey(dst []byte, s string) []byte {
	if n.AllSpace {
		for _, r := range s {
			if !IsSpace(r) {
				dst = n.appendRune(dst, r)
			}
		}
		return dst
	}
	if n.SpaceChange || n.TrailingSpace {
		s = strings.TrimRightFunc(s, IsSpace)
	}
	inSpace := false
	for _, r := range s {
		if n.SpaceChange && IsSpace(r) {
			if !inSpace {
				dst = append(dst, ' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		dst = n.appendRune(dst, r)
	}
	return dst
}

// TokenKey appends the key of one token to dst. A line's tokens, keyed and
// concatenated (leaving out the ones Skip names), give its LineKey: that is
// what makes the line and word levels agree on what is equal.
func (n Norm) TokenKey(dst []byte, t Token) []byte {
	if t.Kind == Space && n.SpaceChange {
		return append(dst, ' ')
	}
	if !n.Case {
		return append(dst, t.Text...)
	}
	for _, r := range t.Text {
		dst = n.appendRune(dst, r)
	}
	return dst
}

// Skip reports whether token i of toks takes no part in the comparison:
// any white space when all of it is ignored, trailing white space when that
// is.
func (n Norm) Skip(toks []Token, i int) bool {
	if toks[i].Kind != Space {
		return false
	}
	return n.AllSpace || ((n.SpaceChange || n.TrailingSpace) && i == len(toks)-1)
}

func (n Norm) appendRune(dst []byte, r rune) []byte {
	if n.Case {
		r = Fold(r)
	}
	if r < utf8.RuneSelf {
		return append(dst, byte(r))
	}
	return utf8.AppendRune(dst, r)
}
