// Package textenc turns the bytes of a text file into a string and splits
// it into lines, keeping what a comparison has to report about the file
// besides its text: the encoding it was written in and its line endings.
package textenc

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Encoding is the encoding Decode recognised.
type Encoding uint8

// The encodings Decode recognises. UTF8BOM is kept apart from UTF8 because
// a mark is invisible in an editor, and two files that differ only by one
// should be explained, not shown as equal without comment.
const (
	UTF8 Encoding = iota
	UTF8BOM
	UTF16LE
	UTF16BE
	Windows1252
)

// String returns the name used in reports and JSON.
func (e Encoding) String() string {
	switch e {
	case UTF8BOM:
		return "utf-8-bom"
	case UTF16LE:
		return "utf-16le"
	case UTF16BE:
		return "utf-16be"
	case Windows1252:
		return "windows-1252"
	}
	return "utf-8"
}

// ErrBinary reports a NUL character. Text files do not contain one, and a
// binary file compared line by line shows nothing but noise.
var ErrBinary = errors.New("binary data")

// Decode returns b as a string.
//
// A byte order mark decides the encoding. Without one, valid UTF-8 is read
// as UTF-8 and anything else as Windows-1252: it is the legacy encoding that
// files reaching a web page most often use, and it maps every byte to a
// character, so decoding cannot fail.
func Decode(b []byte) (string, Encoding, error) {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		b = b[3:]
		if bytes.IndexByte(b, 0) >= 0 {
			return "", UTF8BOM, ErrBinary
		}
		if utf8.Valid(b) {
			return string(b), UTF8BOM, nil
		}
		// The mark says UTF-8, so an invalid byte is damage to repair,
		// not a sign of another encoding.
		return string(bytes.ToValidUTF8(b, []byte(string(utf8.RuneError)))), UTF8BOM, nil
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return decodeUTF16(b[2:], UTF16LE)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return decodeUTF16(b[2:], UTF16BE)
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return "", UTF8, ErrBinary
	}
	if utf8.Valid(b) {
		return string(b), UTF8, nil
	}
	return decode1252(b), Windows1252, nil
}

// decodeUTF16 decodes unit by unit instead of through utf16.Decode, which
// would hold the whole text as runes: four bytes per character of a file
// that may be megabytes long.
func decodeUTF16(b []byte, enc Encoding) (string, Encoding, error) {
	unit := func(i int) rune {
		if enc == UTF16BE {
			return rune(b[2*i])<<8 | rune(b[2*i+1])
		}
		return rune(b[2*i+1])<<8 | rune(b[2*i])
	}
	n := len(b) / 2
	var sb strings.Builder
	sb.Grow(n + n/2)
	for i := 0; i < n; i++ {
		r := unit(i)
		if utf16.IsSurrogate(r) {
			// A high surrogate followed by a low one is one character;
			// anything else is a broken pair, shown as U+FFFD.
			if r < 0xDC00 && i+1 < n {
				if r2 := unit(i + 1); 0xDC00 <= r2 && r2 < 0xE000 {
					r = utf16.DecodeRune(r, r2)
					i++
				} else {
					r = utf8.RuneError
				}
			} else {
				r = utf8.RuneError
			}
		}
		if r == 0 {
			return "", enc, ErrBinary
		}
		sb.WriteRune(r)
	}
	if len(b)%2 == 1 {
		sb.WriteRune(utf8.RuneError)
	}
	return sb.String(), enc, nil
}

// cp1252 maps bytes 0x80 to 0x9F, the only range where Windows-1252
// differs from Latin-1. The five bytes Windows-1252 leaves undefined map to
// the C1 controls of the same value, as the WHATWG encoding standard does,
// so every byte still decodes to something and nothing is lost.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

func decode1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/4)
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(cp1252[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// DecodeWindows1252 decodes b as Windows-1252, for XML parts that declare
// a Latin encoding: Latin-1 is a subset of it.
func DecodeWindows1252(b []byte) string { return decode1252(b) }
