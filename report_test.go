package gdco

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func report(t *testing.T, r *Result, o *ReportOptions) string {
	t.Helper()
	var b strings.Builder
	if err := r.WriteHTML(&b, o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// allowedTags are the only elements the report writes. Any other tag in the
// output would have come from the input, unescaped.
var allowedTags = map[string]bool{
	"!doctype": true, "html": true, "head": true, "meta": true, "title": true, "style": true,
	"body": true, "div": true, "header": true, "h1": true, "h2": true, "dl": true, "dt": true,
	"dd": true, "span": true, "ul": true, "li": true, "main": true, "p": true, "section": true,
	"table": true, "colgroup": true, "col": true, "thead": true, "tbody": true, "tr": true,
	"th": true, "td": true, "del": true, "ins": true, "footer": true,
}

// allowedAttrs are the only attributes the report writes.
var allowedAttrs = map[string]bool{
	"lang": true, "charset": true, "name": true, "content": true, "http-equiv": true,
	"class": true, "title": true, "dir": true, "colspan": true,
}

// checkHTML scans the report the way an attacker would hope a browser
// reads it: every "<" must open or close an allowed element whose
// attributes are allowed and properly quoted, and nothing may be left that
// HTML does not allow in text. Escaped text such as "&lt;img onerror=" is
// harmless and passes.
func checkHTML(t *testing.T, doc string) {
	t.Helper()
	if !utf8.ValidString(doc) {
		t.Fatal("report is not valid UTF-8")
	}
	fail := func(i int, what string) {
		t.Helper()
		t.Fatalf("%s at %d: %q", what, i, doc[max(0, i-20):min(len(doc), i+60)])
	}
	isName := func(c byte) bool {
		return c == '-' || c == '!' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; i < len(doc); i++ {
		c := doc[i]
		if c < 0x20 && c != '\n' && c != '\t' {
			fail(i, "raw control byte")
		}
		if c != '<' {
			continue
		}
		j := i + 1
		if j < len(doc) && doc[j] == '/' {
			j++
		}
		k := j
		for k < len(doc) && isName(doc[k]) {
			k++
		}
		name := strings.ToLower(doc[j:k])
		if !allowedTags[name] {
			fail(i, "unexpected tag "+name)
		}
		if name == "!doctype" {
			if !strings.HasPrefix(doc[k:], " html>") {
				fail(i, "odd doctype")
			}
			i = k
			continue
		}
		// Attributes: name="value", with no quote inside the value.
		for {
			for k < len(doc) && doc[k] == ' ' {
				k++
			}
			if k >= len(doc) {
				fail(i, "unterminated tag")
			}
			if doc[k] == '>' {
				break
			}
			a := k
			for k < len(doc) && isName(doc[k]) {
				k++
			}
			if attr := doc[a:k]; !allowedAttrs[attr] {
				fail(a, "unexpected attribute "+attr)
			}
			if !strings.HasPrefix(doc[k:], `="`) {
				fail(k, "unquoted attribute")
			}
			end := strings.IndexByte(doc[k+2:], '"')
			if end < 0 {
				fail(k, "unterminated attribute")
			}
			if v := doc[k+2 : k+2+end]; strings.ContainsAny(v, "<>") {
				fail(k, "markup inside an attribute")
			}
			k += 2 + end + 1
		}
		i = k
	}
	if strings.Contains(strings.ToLower(doc), "<script") {
		t.Fatal("report contains a script")
	}
	if !strings.Contains(doc, `http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"`) {
		t.Fatal("report lacks its Content-Security-Policy")
	}
}

func TestReportEscapes(t *testing.T) {
	// The first two lines stay unchanged, so they are written whole; the
	// third is modified and written in spans.
	a := "<script>alert('x')</script>\nTom & Jerry \"quoted\"\nold\n"
	b := "<script>alert('x')</script>\nTom & Jerry \"quoted\"\nnew <img src=x onerror=alert(1)>\n"
	r, err := Compare(TextDocument("<b>a</b>.txt", a), TextDocument("b\".txt", b), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, layout := range []Layout{LayoutSideBySide, LayoutInline} {
		doc := report(t, r, &ReportOptions{Title: "</title><script>", Layout: layout, Generated: "<now>"})
		checkHTML(t, doc)
		for _, want := range []string{"&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;", "Tom &amp; Jerry &#34;quoted&#34;", "&lt;b&gt;a&lt;/b&gt;.txt", "<title>&lt;/title&gt;&lt;script&gt;</title>", "&lt;now&gt;"} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s report lacks %q", layout, want)
			}
		}
	}
}

func TestReportMarksInvisible(t *testing.T) {
	r, err := CompareText("pay now and​later\x01\n", "pay now andlater\x01\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := report(t, r, nil)
	checkHTML(t, doc)
	for _, want := range []string{"no-break space U+00A0", "zero-width space U+200B", "␁"} {
		if !strings.Contains(doc, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	// A bidirectional override is marked even in unchanged text, since it
	// can reorder what the reader sees.
	r, err = CompareText("same ‮txt.exe\nx\n", "same ‮txt.exe\ny\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc := report(t, r, nil); !strings.Contains(doc, "bidirectional control U+202E") {
		t.Error("bidi override not marked")
	}
}

func TestReportContents(t *testing.T) {
	r, err := CompareText("one\ntwo\nthree\n", "one\n2\nthree\nfour\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := report(t, r, &ReportOptions{Generated: "2026-11-19"})
	checkHTML(t, doc)
	for _, want := range []string{
		"<title>Comparison of a and b</title>",
		"1 line changed", "1 line added", "0 lines removed",
		"<del>two</del>", "<ins>2</ins>", "<ins>four</ins>",
		"prefers-color-scheme:dark", "@media print", "Generated 2026-11-19",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	same, _ := CompareText("x\n", "x\n", nil)
	if doc := report(t, same, nil); !strings.Contains(doc, "No differences found.") {
		t.Error("equal report lacks its message")
	}
	if err := r.WriteHTML(&strings.Builder{}, &ReportOptions{Layout: "grid"}); !errors.Is(err, ErrOptions) {
		t.Errorf("unknown layout: %v", err)
	}
}

func FuzzReport(f *testing.F) {
	f.Add("a <b> & c\n", "a <i> & c\n", uint8(0))
	f.Add("\x00\x1b[31m‮", " ​", uint8(1))
	f.Fuzz(func(t *testing.T, a, b string, opt uint8) {
		o := DefaultOptions()
		o.IgnoreCase = opt&2 != 0
		o.Granularity = []Granularity{ByWord, ByChar, ByLine}[int(opt>>2)%3]
		r, err := Compare(TextDocument(a, a), TextDocument(b, b), o)
		if err != nil {
			t.Fatal(err)
		}
		layout := LayoutSideBySide
		if opt&1 != 0 {
			layout = LayoutInline
		}
		checkHTML(t, report(t, r, &ReportOptions{Title: b, Layout: layout, Generated: a}))
	})
}
