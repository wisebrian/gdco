package xlsx

import (
	"strconv"
	"strings"
)

// The size of a worksheet, which references cannot leave.
const (
	maxCol = 16384   // XFD
	maxRow = 1048576 // 2^20
)

// colName turns a 1-based column number into letters: 1 is A, 28 is AB.
func colName(c int) string {
	var b []byte
	for c > 0 {
		c--
		b = append(b, byte('A'+c%26))
		c /= 26
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// colNumber reads column letters, or returns 0 if they are not a column.
func colNumber(s string) int {
	if s == "" || len(s) > 3 {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c < 'a' || c > 'z' {
			return 0
		}
		n = n*26 + int(c-'a') + 1
	}
	if n > maxCol {
		return 0
	}
	return n
}

// Address returns the A1 address of a cell.
func Address(col, row int) string { return colName(col) + strconv.Itoa(row) }

// parseRef splits an A1 reference such as "$B7" into its column and row,
// or reports false.
func parseRef(s string) (col, row int, ok bool) {
	i := 0
	for i < len(s) && (s[i]|0x20 >= 'a' && s[i]|0x20 <= 'z') {
		i++
	}
	col = colNumber(s[:i])
	if col == 0 || i == len(s) {
		return 0, 0, false
	}
	r, err := strconv.Atoi(s[i:])
	if err != nil || r < 1 || r > maxRow || s[i] == '+' || s[i] == '-' {
		return 0, 0, false
	}
	return col, r, true
}

// shift moves the relative references of a shared formula from the cell
// that defines it to a cell dr rows and dc columns away, as Excel does when
// it fills a formula: A1 two rows down is A3, $A$1 stays. A reference
// pushed off the sheet becomes #REF!. Text in quotes, sheet names, function
// names and structured references are copied as they are.
func shift(f string, dr, dc int) string {
	if dr == 0 && dc == 0 {
		return f
	}
	var b strings.Builder
	isWord := func(c byte) bool {
		return c == '$' || c == '_' || c == '.' || c >= '0' && c <= '9' || c|0x20 >= 'a' && c|0x20 <= 'z'
	}
	for i := 0; i < len(f); {
		c := f[i]
		switch {
		case c == '"' || c == '\'':
			// A string, or a quoted sheet name; a doubled quote stays inside.
			j := i + 1
			for j < len(f) {
				if f[j] == c {
					if j+1 < len(f) && f[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			j = min(j+1, len(f))
			b.WriteString(f[i:j])
			i = j
		case c == '[':
			// A structured reference or an external workbook number.
			depth, j := 0, i
			for j < len(f) {
				if f[j] == '[' {
					depth++
				} else if f[j] == ']' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			j = min(j+1, len(f))
			b.WriteString(f[i:j])
			i = j
		case isWord(c):
			j := i
			for j < len(f) && isWord(f[j]) {
				j++
			}
			tok := f[i:j]
			next := byte(0)
			if j < len(f) {
				next = f[j]
			}
			prev := byte(0)
			if i > 0 {
				prev = f[i-1]
			}
			switch {
			case next == '(' || next == '!':
				b.WriteString(tok) // a function, or a sheet name
			case prev == ':' || next == ':':
				b.WriteString(shiftPart(tok, dr, dc))
			default:
				if col, row, ok := refParts(tok); ok {
					b.WriteString(shiftRef(tok, col, row, dr, dc))
				} else {
					b.WriteString(tok)
				}
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// refParts reads a reference with its $ signs: "$B$7".
func refParts(tok string) (col, row int, ok bool) {
	return parseRef(strings.ReplaceAll(tok, "$", ""))
}

func shiftRef(tok string, col, row, dr, dc int) string {
	colAbs := strings.HasPrefix(tok, "$")
	rowAbs := strings.Contains(strings.TrimPrefix(tok, "$"), "$")
	if !colAbs {
		col += dc
	}
	if !rowAbs {
		row += dr
	}
	if col < 1 || col > maxCol || row < 1 || row > maxRow {
		return "#REF!"
	}
	var b strings.Builder
	if colAbs {
		b.WriteByte('$')
	}
	b.WriteString(colName(col))
	if rowAbs {
		b.WriteByte('$')
	}
	b.WriteString(strconv.Itoa(row))
	return b.String()
}

// shiftPart moves one end of a range: a cell, a whole column (A:C) or a
// whole row (1:3).
func shiftPart(tok string, dr, dc int) string {
	if col, row, ok := refParts(tok); ok {
		return shiftRef(tok, col, row, dr, dc)
	}
	abs := strings.HasPrefix(tok, "$")
	bare := strings.TrimPrefix(tok, "$")
	if n := colNumber(bare); n > 0 {
		if !abs {
			n += dc
		}
		if n < 1 || n > maxCol {
			return "#REF!"
		}
		if abs {
			return "$" + colName(n)
		}
		return colName(n)
	}
	if n, err := strconv.Atoi(bare); err == nil && bare != "" && bare[0] != '+' && bare[0] != '-' {
		if !abs {
			n += dr
		}
		if n < 1 || n > maxRow {
			return "#REF!"
		}
		if abs {
			return "$" + strconv.Itoa(n)
		}
		return strconv.Itoa(n)
	}
	return tok
}

// ColName returns the letters of a 1-based column: 1 is A, 28 is AB.
func ColName(c int) string { return colName(c) }
