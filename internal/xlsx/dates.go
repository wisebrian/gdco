package xlsx

import (
	"math"
	"strconv"
	"strings"
)

// kind is what a number format makes of a number.
type kind uint8

const (
	plain    kind = iota // shown as written in the file
	date                 // a calendar date
	clock                // a time of day
	moment               // a date and a time
	duration             // elapsed time, [h]:mm and the like
)

// builtinKind classifies Excel's built-in number formats, which have ids
// but no format code in the file. 27 to 36 and 50 to 58 are the dates of
// East Asian locales.
func builtinKind(id int) kind {
	switch {
	case id >= 14 && id <= 17, id >= 27 && id <= 36, id >= 50 && id <= 58:
		return date
	case id >= 18 && id <= 21, id == 45, id == 47:
		return clock
	case id == 22:
		return moment
	case id == 46:
		return duration
	}
	return plain
}

// codeKind classifies a custom format code. Quoted text, escaped
// characters, fill and padding characters and bracketed colours or
// locales are not date codes and are dropped first; what remains decides:
// d or y make a date, h or s a time, [h], [m] or [s] an elapsed time. A
// lone m is a month, as Excel reads it without hours around it.
func codeKind(code string) kind {
	if i := strings.IndexByte(code, ';'); i >= 0 {
		code = code[:i] // the section for positive numbers decides
	}
	var b strings.Builder
	elapsed := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch c {
		case '"':
			j := strings.IndexByte(code[i+1:], '"')
			if j < 0 {
				i = len(code)
			} else {
				i += j + 1
			}
		case '\\', '_', '*':
			i++ // the next character is literal, padding or fill
		case '[':
			j := strings.IndexByte(code[i+1:], ']')
			if j < 0 {
				i = len(code)
				continue
			}
			inner := strings.ToLower(code[i+1 : i+1+j])
			if inner != "" && strings.Trim(inner, "hms") == "" {
				elapsed = true
			}
			i += j + 1
		default:
			b.WriteByte(c | 0x20) // lower case for letters; other bytes are only tested below
		}
	}
	s := b.String()
	hasDate := strings.ContainsAny(s, "dy")
	hasTime := strings.ContainsAny(s, "hs") || strings.Contains(s, "am/pm") || strings.Contains(s, "a/p")
	switch {
	case elapsed:
		return duration
	case hasDate && hasTime:
		return moment
	case hasDate:
		return date
	case hasTime:
		return clock
	case strings.Contains(s, "m"):
		return date
	}
	return plain
}

// The last serial number Excel shows as a date, 9999-12-31.
const maxSerial = 2958465

// format shows a serial number as ISO 8601 in the way its format asks for,
// or returns false when the number cannot be a date: Excel shows #####
// for those, and the number as written says more.
func format(v string, k kind, date1904 bool) (string, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > maxSerial {
		return "", false
	}
	days := math.Floor(f)
	secs := math.Round((f - days) * 86400)
	if secs >= 86400 {
		days++
		secs -= 86400
	}
	hms := func(total int64) string {
		h, m, s := total/3600, total/60%60, total%60
		return pad(h, 2) + ":" + pad(m, 2) + ":" + pad(s, 2)
	}
	if k == duration {
		return hms(int64(days)*86400 + int64(secs)), true
	}
	if k == clock {
		return hms(int64(secs)), true
	}
	d := ymd(int64(days), date1904)
	if k == moment {
		return d + "T" + hms(int64(secs)), true
	}
	return d, true
}

func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ymd turns a serial day into a date. In the 1900 system, day 1 is
// 1900-01-01 and day 60 is 1900-02-29, a day that never was: Lotus 1-2-3
// counted 1900 as a leap year and Excel kept the error, so later days are
// one off from a plain count. Day 0 is shown as Excel shows it.
func ymd(day int64, date1904 bool) string {
	var y, m, d int64
	switch {
	case date1904:
		y, m, d = civil(daysFromCivil(1904, 1, 1) + day)
	case day == 0:
		return "1900-01-00"
	case day == 60:
		return "1900-02-29"
	case day < 60:
		y, m, d = civil(daysFromCivil(1899, 12, 31) + day)
	default:
		y, m, d = civil(daysFromCivil(1899, 12, 30) + day)
	}
	return pad(y, 4) + "-" + pad(m, 2) + "-" + pad(d, 2)
}

// daysFromCivil and civil convert between dates of the proleptic Gregorian
// calendar and days since 1970-01-01, after Howard Hinnant's algorithms.
// They are written here rather than taken from the time package, which
// would bring the clock into a package that must not touch it.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func civil(z int64) (y, m, d int64) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = doy - (153*mp+2)/5 + 1
	m = mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}
