package textenc

import "strings"

// EOL is the terminator that ended one line.
type EOL uint8

// The terminators a file can arrive with. None marks the last line of a
// file that does not end with a newline, which a unified diff has to state.
const (
	None EOL = iota
	LF
	CRLF
	CR
)

// String returns the terminator itself.
func (e EOL) String() string {
	switch e {
	case LF:
		return "\n"
	case CRLF:
		return "\r\n"
	case CR:
		return "\r"
	}
	return ""
}

// Split cuts s into lines at LF, CRLF and CR. The terminators come back
// apart from the text because they are compared apart from it: two files
// that differ only in line endings compare equal, with a note.
func Split(s string) (lines []string, eols []EOL) {
	n := strings.Count(s, "\n") + 1
	lines = make([]string, 0, n)
	eols = make([]EOL, 0, n)
	for len(s) > 0 {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			lines = append(lines, s)
			eols = append(eols, None)
			break
		}
		lines = append(lines, s[:i])
		switch {
		case s[i] == '\n':
			eols = append(eols, LF)
			s = s[i+1:]
		case i+1 < len(s) && s[i+1] == '\n':
			eols = append(eols, CRLF)
			s = s[i+2:]
		default:
			eols = append(eols, CR)
			s = s[i+1:]
		}
	}
	return lines, eols
}

// Ending sums up the terminators of a whole file.
type Ending uint8

// The conventions a file can follow. EndingNone is a file with no line
// terminator at all, which says nothing about its convention.
const (
	EndingNone Ending = iota
	EndingLF
	EndingCRLF
	EndingCR
	EndingMixed
)

// String returns the name used in reports and JSON.
func (e Ending) String() string {
	switch e {
	case EndingLF:
		return "lf"
	case EndingCRLF:
		return "crlf"
	case EndingCR:
		return "cr"
	case EndingMixed:
		return "mixed"
	}
	return ""
}

// Summarize returns the single convention eols follow, EndingMixed if they
// follow several, or EndingNone if there is no terminator.
func Summarize(eols []EOL) Ending {
	seen := EOL(None)
	for _, e := range eols {
		if e == None {
			continue
		}
		if seen == None {
			seen = e
		} else if e != seen {
			return EndingMixed
		}
	}
	switch seen {
	case LF:
		return EndingLF
	case CRLF:
		return EndingCRLF
	case CR:
		return EndingCR
	}
	return EndingNone
}
