package gdco

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReportOptions carry what the report cannot know by itself.
type ReportOptions struct {
	Title     string // defaults to the two names
	Layout    Layout // LayoutSideBySide when empty
	Generated string // a date to print; the package has no clock, so the caller passes it
}

// Layout is how the HTML report places the two versions.
type Layout string

// The layouts. Inline puts the versions of a changed line one under the
// other, which prints better on a portrait page.
const (
	LayoutSideBySide Layout = "side-by-side"
	LayoutInline     Layout = "inline"
)

// WriteHTML writes a report that is safe to keep and forward: inline CSS,
// no scripts and a Content-Security-Policy that forbids them anyway, every
// piece of text escaped, and invisible characters inside changes made
// visible, since a change that looks like no change is exactly what a
// reader of a contract needs to see. Insertions are underlined and
// deletions struck through, as in a legal redline, so the report still
// reads when printed in black and white.
func (r *Result) WriteHTML(w io.Writer, o *ReportOptions) (err error) {
	defer guard(&err)
	var ro ReportOptions
	if o != nil {
		ro = *o
	}
	switch ro.Layout {
	case "":
		ro.Layout = LayoutSideBySide
	case LayoutSideBySide, LayoutInline:
	default:
		return fmt.Errorf("%w: unknown layout %q", ErrOptions, ro.Layout)
	}
	if ro.Title == "" {
		ro.Title = "Comparison of " + nameOr(r.A.Name, "A") + " and " + nameOr(r.B.Name, "B")
	}
	h := &htmlWriter{w: w, r: r, o: ro}
	h.document()
	h.flush()
	return h.err
}

func nameOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

type htmlWriter struct {
	out
	r *Result
	o ReportOptions
}

func (h *htmlWriter) document() {
	h.str("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<meta name=\"color-scheme\" content=\"light dark\">\n" +
		"<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\">\n" +
		"<title>")
	h.attr(h.o.Title)
	h.str("</title>\n<style>" + reportCSS + "</style>\n</head>\n<body>\n<div class=\"wrap\">\n<header>\n<h1>")
	h.text(h.o.Title, false)
	h.str("</h1>\n<dl class=\"files\">")
	h.side("A", h.r.A)
	h.side("B", h.r.B)
	h.str("</dl>\n")
	h.summary()
	if len(h.r.Notes) > 0 {
		h.str("<ul class=\"notes\">")
		for _, n := range h.r.Notes {
			h.str("<li>")
			h.text(n.Message, false)
			h.str("</li>")
		}
		h.str("</ul>\n")
	}
	h.str("</header>\n<main>\n")
	if h.r.Equal {
		h.str("<p class=\"same\">No differences found.</p>\n")
	}
	single := len(h.r.Parts) == 1 && h.r.Parts[0].Kind == PartBody
	for _, p := range h.r.Parts {
		h.part(p, single)
	}
	h.str("</main>\n")
	if h.o.Generated != "" {
		h.str("<footer>Generated ")
		h.text(h.o.Generated, false)
		h.str("</footer>\n")
	}
	h.str("</div>\n</body>\n</html>\n")
}

func (h *htmlWriter) side(label string, s Side) {
	h.str("<div><dt>" + label + "</dt><dd>")
	h.text(nameOr(s.Name, "(unnamed)"), false)
	var meta []string
	switch s.Format {
	case FormatDocx:
		meta = append(meta, "Word document")
	case FormatXlsx:
		meta = append(meta, "Excel workbook")
	}
	if s.Encoding != "" {
		meta = append(meta, encodingName(s.Encoding))
	}
	meta = append(meta, plural(s.Lines, "line"))
	h.str(" <span class=\"meta\">· ")
	h.text(strings.Join(meta, " · "), false)
	h.str("</span></dd></div>")
}

func (h *htmlWriter) summary() {
	s := h.r.Stats
	h.str("<ul class=\"summary\">")
	chip := func(class, text string) {
		h.str("<li class=\"chip " + class + "\">")
		h.text(text, false)
		h.str("</li>")
	}
	chip("mod", plural(s.LinesChanged, "line")+" changed")
	chip("ins", plural(s.LinesAdded, "line")+" added")
	chip("del", plural(s.LinesRemoved, "line")+" removed")
	chip("", fmt.Sprintf("+%s · −%s", plural(s.WordsAdded, "word"), plural(s.WordsRemoved, "word")))
	if s.CellsChanged > 0 {
		chip("mod", plural(s.CellsChanged, "cell")+" changed")
	}
	h.str("</ul>\n")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

func (h *htmlWriter) part(p PartDiff, single bool) {
	if !single {
		h.str("<section>\n<h2>")
		h.text(partTitle(p), false)
		h.str("</h2>\n")
		switch p.Status {
		case StatusAdded:
			h.str("<p class=\"meta\">Only in B.</p>\n")
		case StatusRemoved:
			h.str("<p class=\"meta\">Only in A.</p>\n")
		case StatusSame:
			h.str("<p class=\"meta\">No differences.</p>\n")
		}
	}
	if len(p.Hunks) > 0 {
		mono := ""
		if h.r.A.Format == FormatText || h.r.B.Format == FormatText {
			mono = " mono"
		}
		if h.o.Layout == LayoutInline {
			h.str("<table class=\"diff inl" + mono + "\">\n<colgroup><col class=\"cn\"><col class=\"cn\"><col></colgroup>\n" +
				"<thead><tr><th>A</th><th>B</th><th></th></tr></thead>\n")
		} else {
			h.str("<table class=\"diff sbs" + mono + "\">\n<colgroup><col class=\"cn\"><col><col class=\"cn\"><col></colgroup>\n<thead><tr><th colspan=\"2\">")
			h.text(nameOr(h.r.A.Name, "A"), false)
			h.str("</th><th colspan=\"2\">")
			h.text(nameOr(h.r.B.Name, "B"), false)
			h.str("</th></tr></thead>\n")
		}
		end := 0 // A lines covered by the hunks so far
		for _, hk := range p.Hunks {
			h.hunk(p, hk, end)
			end = hk.A.Start - 1 + hk.A.Count
		}
		h.str("</table>\n")
	}
	if !single {
		h.str("</section>\n")
	}
}

func partTitle(p PartDiff) string {
	switch p.Kind {
	case PartSheet:
		return "Sheet “" + p.Name + "”"
	case PartBody:
		return "Body"
	}
	s := string(p.Kind)
	return strings.ToUpper(s[:1]) + s[1:]
}

func (h *htmlWriter) hunk(p PartDiff, hk Hunk, end int) {
	cols := "4"
	if h.o.Layout == LayoutInline {
		cols = "3"
	}
	h.str("<tbody>\n")
	if gap := hk.A.Start - 1 - end; gap > 0 && hk.A.Count > 0 {
		h.str("<tr class=\"gap\"><td colspan=\"" + cols + "\">⋯ " + plural(gap, "unchanged line") + "</td></tr>\n")
	}
	h.str("<tr class=\"hh\"><td colspan=\"" + cols + "\">Lines " + span(hk.A) + " → " + span(hk.B))
	for _, row := range hk.Rows {
		if row.Op == OpEqual {
			continue
		}
		l := row.A
		if l == nil {
			l = row.B
		}
		if loc := describe(p.Kind, l.Loc); loc != "" {
			h.str(" · ")
			h.text(loc, false)
		}
		break
	}
	h.str("</td></tr>\n")
	if h.o.Layout == LayoutInline {
		h.inlineRows(p, hk.Rows)
	} else {
		h.sideRows(p, hk.Rows)
	}
	h.str("</tbody>\n")
}

// span renders a range for people: "12–18", "12", or "none".
func span(r Range) string {
	switch r.Count {
	case 0:
		return "none"
	case 1:
		return strconv.Itoa(r.Start)
	}
	return strconv.Itoa(r.Start) + "–" + strconv.Itoa(r.Start+r.Count-1)
}

// describe names a location in English, using the part to say what an
// item is.
func describe(kind PartKind, l Location) string {
	s := l.String()
	if l.Item == 0 {
		return s
	}
	var item string
	switch kind {
	case PartHeaders:
		item = "Header"
	case PartFooters:
		item = "Footer"
	case PartFootnotes:
		item = "Footnote"
	case PartEndnotes:
		item = "Endnote"
	case PartComments:
		item = "Comment"
	default:
		item = "Item"
	}
	item += " " + strconv.Itoa(l.Item)
	if s == "" {
		return item
	}
	return item + ", " + strings.ToLower(s[:1]) + s[1:]
}

// sideRows lays rows out in two columns. Lines deleted and inserted
// together without being paired are shown next to each other, as the eye
// expects, but without word marks, since they are not versions of each
// other.
func (h *htmlWriter) sideRows(p PartDiff, rows []Row) {
	for i := 0; i < len(rows); {
		row := rows[i]
		if row.Op == OpEqual || row.Op == OpModify {
			h.str("<tr>")
			h.cell(p, row.A, row)
			h.cell(p, row.B, row)
			h.str("</tr>\n")
			i++
			continue
		}
		var dels, inss []Row
		for ; i < len(rows) && (rows[i].Op == OpDelete || rows[i].Op == OpInsert); i++ {
			if rows[i].Op == OpDelete {
				dels = append(dels, rows[i])
			} else {
				inss = append(inss, rows[i])
			}
		}
		for k := 0; k < max(len(dels), len(inss)); k++ {
			h.str("<tr>")
			if k < len(dels) {
				h.cell(p, dels[k].A, dels[k])
			} else {
				h.str("<td class=\"n empty\"></td><td class=\"t empty\"></td>")
			}
			if k < len(inss) {
				h.cell(p, inss[k].B, inss[k])
			} else {
				h.str("<td class=\"n empty\"></td><td class=\"t empty\"></td>")
			}
			h.str("</tr>\n")
		}
	}
}

// inlineRows lays rows out in one column, the old version of a modified
// line above the new one, as a unified diff does.
func (h *htmlWriter) inlineRows(p PartDiff, rows []Row) {
	one := func(l *Line, row Row, isA bool) {
		h.str("<tr>")
		na, nb := "", ""
		switch {
		case row.Op == OpEqual:
			na, nb = strconv.Itoa(row.A.N), strconv.Itoa(row.B.N)
		case isA:
			na = strconv.Itoa(l.N)
		default:
			nb = strconv.Itoa(l.N)
		}
		cls := h.classOf(row, isA)
		h.str("<td class=\"n" + cls + "\">" + na + "</td><td class=\"n" + cls + "\">" + nb + "</td>")
		h.textCell(p, l, row, isA, cls)
		h.str("</tr>\n")
	}
	for _, row := range rows {
		switch row.Op {
		case OpEqual:
			one(row.A, row, true)
		case OpDelete:
			one(row.A, row, true)
		case OpInsert:
			one(row.B, row, false)
		case OpModify:
			one(row.A, row, true)
			one(row.B, row, false)
		}
	}
}

func (h *htmlWriter) classOf(row Row, isA bool) string {
	cls := ""
	switch {
	case row.Op == OpEqual:
	case isA:
		cls = " del"
	default:
		cls = " ins"
	}
	if row.Ignored {
		cls += " ign"
	}
	return cls
}

// cell writes the number and text cells of one side of a side-by-side row.
func (h *htmlWriter) cell(p PartDiff, l *Line, row Row) {
	isA := l == row.A
	if l == nil {
		h.str("<td class=\"n empty\"></td><td class=\"t empty\"></td>")
		return
	}
	cls := h.classOf(row, isA)
	h.str("<td class=\"n" + cls + "\">" + strconv.Itoa(l.N) + "</td>")
	h.textCell(p, l, row, isA, cls)
}

func (h *htmlWriter) textCell(p PartDiff, l *Line, row Row, isA bool, cls string) {
	whole := row.Op == OpDelete || row.Op == OpInsert
	if whole {
		cls += " whole"
	}
	h.str("<td class=\"t" + cls + "\" dir=\"auto\"")
	if loc := describe(p.Kind, l.Loc); loc != "" {
		h.str(" title=\"")
		h.attr(loc)
		h.str("\"")
	}
	h.str(">")
	if l.Marker != "" {
		h.str("<span class=\"mk\">")
		h.text(l.Marker, false)
		h.str("</span>")
	}
	tag := "ins"
	if isA {
		tag = "del"
	}
	switch {
	case whole:
		h.str("<" + tag + ">")
		h.text(l.Text, true)
		h.str("</" + tag + ">")
	case len(l.Spans) > 0:
		for _, s := range l.Spans {
			if s.Op == OpEqual {
				h.text(s.Text, false)
				continue
			}
			h.str("<" + tag + ">")
			h.text(s.Text, true)
			h.str("</" + tag + ">")
		}
	default:
		h.text(l.Text, row.Op == OpModify)
	}
	if l.NoNewline {
		h.str("<span class=\"eof\">no newline at end of file</span>")
	}
	h.str("</td>")
}

// text writes s as HTML content. Control characters and bidirectional
// overrides are always shown as visible markers: the first are not
// allowed in HTML text, and the second can make text display in an order
// other than the one it is compared in. With visible set, for changed
// text, so are the characters a reader cannot see: no-break and
// zero-width spaces, soft hyphens, direction marks.
func (h *htmlWriter) text(s string, visible bool) { h.escape(s, visible, true) }

// attr writes s inside an attribute or the title element, where markup is
// not allowed: hidden characters become U+FFFD there.
func (h *htmlWriter) attr(s string) { h.escape(s, false, false) }

func (h *htmlWriter) escape(s string, visible, markup bool) {
	last := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		var rep string
		switch {
		case r == '&':
			rep = "&amp;"
		case r == '<':
			rep = "&lt;"
		case r == '>':
			rep = "&gt;"
		case r == '"':
			rep = "&#34;"
		case r == '\'':
			rep = "&#39;"
		case r == utf8.RuneError && size == 1:
			// Names and titles come from the caller and may not be UTF-8.
			rep = "�"
		default:
			name := hidden(r, visible)
			switch {
			case name == "":
			case markup:
				rep = "<span class=\"ctl\" title=\"" + name + "\">" + glyph(r) + "</span>"
			default:
				rep = "�"
			}
		}
		if rep != "" {
			h.str(s[last:i])
			h.str(rep)
			last = i + size
		}
		i += size
	}
	h.str(s[last:])
}

// hidden names a character that would not show, or would show misleadingly,
// or returns "" for one that displays as itself.
func hidden(r rune, visible bool) string {
	switch {
	case r == '\t':
		return ""
	case r < 0x20 || r == 0x7F || (0x80 <= r && r < 0xA0):
		return fmt.Sprintf("control character U+%04X", r)
	case 0x202A <= r && r <= 0x202E, 0x2066 <= r && r <= 0x2069:
		return fmt.Sprintf("bidirectional control U+%04X", r)
	case r == 0x2028:
		return "line separator U+2028"
	case r == 0x2029:
		return "paragraph separator U+2029"
	case r == 0xFFFE || r == 0xFFFF:
		return fmt.Sprintf("noncharacter U+%04X", r)
	}
	if !visible {
		return ""
	}
	switch r {
	case 0x00A0:
		return "no-break space U+00A0"
	case 0x202F:
		return "narrow no-break space U+202F"
	case 0x2007:
		return "figure space U+2007"
	case 0x00AD:
		return "soft hyphen U+00AD"
	case 0x200B:
		return "zero-width space U+200B"
	case 0x2060:
		return "word joiner U+2060"
	case 0xFEFF:
		return "zero-width no-break space U+FEFF"
	case 0x200E:
		return "left-to-right mark U+200E"
	case 0x200F:
		return "right-to-left mark U+200F"
	case 0x061C:
		return "Arabic letter mark U+061C"
	case 0x200C, 0x200D:
		return "" // joiners shape Indic, Persian and emoji text; marking them would break it
	}
	if unicode.Is(unicode.Cf, r) {
		return fmt.Sprintf("format character U+%04X", r)
	}
	return ""
}

// glyph is what stands in for a hidden character: the Control Pictures
// block where Unicode has one, a shouldered box for no-break spaces, and
// the code point otherwise.
func glyph(r rune) string {
	switch {
	case r < 0x20:
		return string(rune(0x2400 + r))
	case r == 0x7F:
		return "␡"
	case r == 0x00A0 || r == 0x202F || r == 0x2007:
		return "⍽"
	case r == 0x00AD:
		return "-"
	}
	return fmt.Sprintf("U+%04X", r)
}

const reportCSS = `
:root{color-scheme:light dark;--bg:#ffffff;--fg:#1c2127;--muted:#5d6874;--line:#d5dbe1;--head:#f4f6f8;--gap:#eef1f4;
--del-row:#fdedef;--del-mark:#f6bcc3;--del-ink:#7d1520;--ins-row:#e7f6eb;--ins-mark:#a9e3b7;--ins-ink:#13502b;
--num:#88929c;--ctl:#6b4500;--ctl-bg:#fff1c7}
@media (prefers-color-scheme:dark){:root{--bg:#111417;--fg:#e2e7ec;--muted:#9ba6b1;--line:#2d343c;--head:#191e24;--gap:#161b20;
--del-row:#3b1c21;--del-mark:#7e2c36;--del-ink:#ffdadd;--ins-row:#15301f;--ins-mark:#2d6c41;--ins-ink:#cbf7d7;
--num:#6d7782;--ctl:#ffd27a;--ctl-bg:#3e3316}}
@media print{:root{--bg:#fff;--fg:#000;--muted:#444;--line:#bbb;--head:#f1f1f1;--gap:#f1f1f1;--del-row:#fdedef;--del-mark:#f4b4bc;
--del-ink:#000;--ins-row:#e7f6eb;--ins-mark:#a6e0b4;--ins-ink:#000;--num:#666;--ctl:#000;--ctl-bg:#fff1c7}
*{-webkit-print-color-adjust:exact;print-color-adjust:exact}body{font-size:9pt;padding:0}tr{break-inside:avoid}
thead{display:table-header-group}.diff th{position:static}}
*{box-sizing:border-box}
html{background:var(--bg)}
body{margin:0;padding:24px 16px 48px;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif}
.wrap{max-width:1440px;margin:0 auto}
h1{font-size:22px;line-height:1.3;margin:0 0 12px;font-weight:600;overflow-wrap:anywhere}
h2{font-size:17px;margin:28px 0 8px;font-weight:600}
.files{display:grid;grid-template-columns:auto 1fr;gap:2px 12px;margin:0 0 12px}
.files div{display:contents}
.files dt{font-weight:600;color:var(--muted)}
.files dd{margin:0;overflow-wrap:anywhere}
.meta{color:var(--muted);font-size:14px}
.summary{display:flex;flex-wrap:wrap;gap:8px;margin:12px 0;padding:0;list-style:none}
.chip{border:1px solid var(--line);border-radius:999px;padding:2px 10px;font-size:13px}
.chip.ins{background:var(--ins-row)}.chip.del{background:var(--del-row)}.chip.mod{background:var(--head)}
.notes{margin:8px 0 0;padding-left:20px;color:var(--muted);font-size:14px}
.same{padding:14px 16px;border:1px solid var(--line);border-radius:8px;background:var(--head)}
table.diff{width:100%;border-collapse:collapse;table-layout:fixed;border:1px solid var(--line);font-size:13.5px;margin:8px 0 16px}
.diff col.cn{width:4.2em}
.diff th{position:sticky;top:0;background:var(--head);color:var(--muted);font-weight:600;text-align:left;padding:4px 8px;border-bottom:1px solid var(--line);overflow-wrap:anywhere}
.diff td{vertical-align:top;padding:1px 8px}
.diff .n{text-align:right;color:var(--num);white-space:nowrap;font-variant-numeric:tabular-nums;font-size:12px;padding-top:2px}
.diff .t{white-space:pre-wrap;overflow-wrap:anywhere}
.sbs td.t+td.n{border-left:1px solid var(--line)}
.mono .t{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;font-size:12.5px;tab-size:4}
tr.hh td{background:var(--head);color:var(--muted);border-top:1px solid var(--line);border-bottom:1px solid var(--line);padding:3px 8px;font-size:12.5px}
tr.gap td{background:var(--gap);color:var(--muted);text-align:center;font-size:12px;padding:2px 8px}
td.del{background:var(--del-row)}td.ins{background:var(--ins-row)}td.empty{background:var(--gap)}
del{background:var(--del-mark);color:var(--del-ink);text-decoration:line-through;text-decoration-thickness:1px;border-radius:2px}
ins{background:var(--ins-mark);color:var(--ins-ink);text-decoration:underline;text-underline-offset:2px;border-radius:2px}
.whole del,.whole ins{background:none;color:inherit}
.ign{opacity:.55}
.mk{color:var(--muted);margin-right:.45em}
.ctl{background:var(--ctl-bg);color:var(--ctl);border-radius:3px;padding:0 2px;font:11px/1.4 ui-monospace,Menlo,Consolas,monospace;text-decoration:none}
.eof{color:var(--muted);font-size:11px;margin-left:.6em;font-style:italic}
footer{margin-top:24px;color:var(--muted);font-size:12px}
@media (max-width:640px){body{padding:16px}.diff col.cn{width:3em}table.diff{font-size:12.5px}}
`
