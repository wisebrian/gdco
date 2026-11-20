package docx

import (
	"strconv"
	"strings"

	"github.com/wisebrian/gdco/internal/ooxml"
)

// numbering holds the list definitions of word/numbering.xml and what
// word/styles.xml says about lists: a heading style is often numbered, and
// its paragraphs carry no numbering of their own.
type numbering struct {
	abstract  map[string]*abstractNum
	nums      map[string]*num
	styles    map[string]*paraStyle // paragraph styles by id
	numStyles map[string]string     // numbering styles: style id to num id
	defStyle  string                // the default paragraph style
	plain     bool                  // some level used a format shown as plain numbers
}

type level struct {
	defined bool
	start   int
	format  string // w:numFmt
	text    string // w:lvlText, with %1 to %9 standing for the levels' numbers
	restart int    // w:lvlRestart: -1 when absent, 0 for never
	legal   bool   // w:isLgl: every level shown as a decimal number
}

type abstractNum struct {
	levels    [9]level
	styleLink string // w:numStyleLink: the levels live in another definition
}

type num struct {
	abstract string
	start    [9]int
	hasStart [9]bool
	override [9]*level
}

type paraStyle struct {
	basedOn          string
	numID            string
	ilvl             int
	hasNum, hasLevel bool
}

func newNumbering() *numbering {
	return &numbering{
		abstract:  map[string]*abstractNum{},
		nums:      map[string]*num{},
		styles:    map[string]*paraStyle{},
		numStyles: map[string]string{},
	}
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func parseLevel(l *ooxml.Node) level {
	lv := level{defined: true, restart: -1}
	lv.start = atoi(l.Child(ooxml.NSW, "start").Get(ooxml.NSW, "val"), 0)
	lv.format = l.Child(ooxml.NSW, "numFmt").Get(ooxml.NSW, "val")
	if lv.format == "" {
		lv.format = "decimal"
	}
	if t := l.Child(ooxml.NSW, "lvlText"); t != nil {
		lv.text = t.Get(ooxml.NSW, "val")
	} else {
		lv.text = "%" + strconv.Itoa(atoi(l.Get(ooxml.NSW, "ilvl"), 0)+1) + "."
	}
	if r := l.Child(ooxml.NSW, "lvlRestart"); r != nil {
		lv.restart = atoi(r.Get(ooxml.NSW, "val"), -1)
	}
	if g := l.Child(ooxml.NSW, "isLgl"); g != nil {
		v := g.Get(ooxml.NSW, "val")
		lv.legal = v == "" || v == "1" || v == "true" || v == "on"
	}
	return lv
}

func (n *numbering) readNumbering(root *ooxml.Node) {
	doc := root.Child(ooxml.NSW, "numbering")
	for _, a := range doc.Children(ooxml.NSW, "abstractNum") {
		an := &abstractNum{styleLink: a.Child(ooxml.NSW, "numStyleLink").Get(ooxml.NSW, "val")}
		for _, l := range a.Children(ooxml.NSW, "lvl") {
			if i := atoi(l.Get(ooxml.NSW, "ilvl"), -1); i >= 0 && i < 9 {
				an.levels[i] = parseLevel(l)
			}
		}
		n.abstract[a.Get(ooxml.NSW, "abstractNumId")] = an
	}
	for _, m := range doc.Children(ooxml.NSW, "num") {
		nm := &num{abstract: m.Child(ooxml.NSW, "abstractNumId").Get(ooxml.NSW, "val")}
		for _, o := range m.Children(ooxml.NSW, "lvlOverride") {
			i := atoi(o.Get(ooxml.NSW, "ilvl"), -1)
			if i < 0 || i >= 9 {
				continue
			}
			if s := o.Child(ooxml.NSW, "startOverride"); s != nil {
				nm.start[i], nm.hasStart[i] = atoi(s.Get(ooxml.NSW, "val"), 0), true
			}
			if l := o.Child(ooxml.NSW, "lvl"); l != nil {
				lv := parseLevel(l)
				nm.override[i] = &lv
			}
		}
		n.nums[m.Get(ooxml.NSW, "numId")] = nm
	}
}

func (n *numbering) readStyles(root *ooxml.Node) {
	for _, s := range root.Child(ooxml.NSW, "styles").Children(ooxml.NSW, "style") {
		id := s.Get(ooxml.NSW, "styleId")
		numPr := s.Child(ooxml.NSW, "pPr").Child(ooxml.NSW, "numPr")
		switch s.Get(ooxml.NSW, "type") {
		case "paragraph":
			ps := &paraStyle{basedOn: s.Child(ooxml.NSW, "basedOn").Get(ooxml.NSW, "val")}
			if v := numPr.Child(ooxml.NSW, "numId"); v != nil {
				ps.numID, ps.hasNum = v.Get(ooxml.NSW, "val"), true
			}
			if v := numPr.Child(ooxml.NSW, "ilvl"); v != nil {
				ps.ilvl, ps.hasLevel = atoi(v.Get(ooxml.NSW, "val"), 0), true
			}
			n.styles[id] = ps
			if d := s.Get(ooxml.NSW, "default"); d == "1" || d == "true" {
				n.defStyle = id
			}
		case "numbering":
			if v := numPr.Child(ooxml.NSW, "numId"); v != nil {
				n.numStyles[id] = v.Get(ooxml.NSW, "val")
			}
		}
	}
}

// listProps is what a paragraph says about its own numbering.
type listProps struct {
	style            string
	numID            string
	ilvl             int
	hasNum, hasLevel bool
}

// resolve finds a paragraph's list and level, from its own properties or
// else from its style and the styles that style is based on.
func (n *numbering) resolve(p listProps) (string, int, bool) {
	numID, ilvl, hasNum, hasLevel := p.numID, p.ilvl, p.hasNum, p.hasLevel
	st := p.style
	if st == "" {
		st = n.defStyle
	}
	for i := 0; st != "" && i < 32 && (!hasNum || !hasLevel); i++ {
		s := n.styles[st]
		if s == nil {
			break
		}
		if !hasNum && s.hasNum {
			numID, hasNum = s.numID, true
		}
		if !hasLevel && s.hasLevel {
			ilvl, hasLevel = s.ilvl, true
		}
		st = s.basedOn
	}
	if !hasNum || numID == "" || numID == "0" {
		return "", 0, false
	}
	return numID, min(max(ilvl, 0), 8), true
}

// definition returns the num and the levels it uses, following one
// numStyleLink: a list defined through a numbering style keeps its levels
// in the definition that style points to.
func (n *numbering) definition(numID string) (*num, *abstractNum) {
	nm := n.nums[numID]
	if nm == nil {
		return nil, nil
	}
	an := n.abstract[nm.abstract]
	if an != nil && an.styleLink != "" {
		if linked := n.nums[n.numStyles[an.styleLink]]; linked != nil && n.abstract[linked.abstract] != nil {
			an = n.abstract[linked.abstract]
		}
	}
	return nm, an
}

// counters keep the running numbers of each list, per story.
type counters map[string]*[9]counter

type counter struct {
	value   int
	started bool
}

// marker advances the list a paragraph belongs to and returns what Word
// would show before it, and its shape: the kind of marker and its level,
// which is what gets compared. Comparing the number itself would mark
// every following item as changed when one item is inserted.
func (n *numbering) marker(c counters, p listProps) (marker, shape string) {
	numID, ilvl, ok := n.resolve(p)
	if !ok {
		return "", ""
	}
	nm, an := n.definition(numID)
	if nm == nil || an == nil {
		return "", ""
	}
	levelOf := func(i int) level {
		if nm.override[i] != nil {
			return *nm.override[i]
		}
		return an.levels[i]
	}
	lv := levelOf(ilvl)
	if !lv.defined {
		return "", ""
	}
	startOf := func(i int) int {
		if nm.hasStart[i] {
			return nm.start[i]
		}
		return levelOf(i).start
	}
	// Lists that share a definition continue each other's numbering, as in
	// Word, unless one restarts it with a start override.
	key := "a" + nm.abstract
	for _, h := range nm.hasStart {
		if h {
			key = "n" + numID
			break
		}
	}
	ctr := c[key]
	if ctr == nil {
		ctr = new([9]counter)
		c[key] = ctr
	}
	if ctr[ilvl].started {
		ctr[ilvl].value++
	} else {
		ctr[ilvl] = counter{value: startOf(ilvl), started: true}
	}
	for m := ilvl + 1; m < 9; m++ {
		r := levelOf(m).restart
		if r < 0 || (r > 0 && ilvl < r) {
			ctr[m].started = false
		}
	}

	if lv.format == "bullet" {
		return bullet(lv.text), "•" + strconv.Itoa(ilvl)
	}
	var b strings.Builder
	for i := 0; i < len(lv.text); i++ {
		c := lv.text[i]
		if c != '%' || i+1 >= len(lv.text) || lv.text[i+1] < '1' || lv.text[i+1] > '9' {
			b.WriteByte(c)
			continue
		}
		idx := int(lv.text[i+1] - '1')
		i++
		v := startOf(idx)
		if ctr[idx].started {
			v = ctr[idx].value
		}
		f := levelOf(idx).format
		if lv.legal {
			f = "decimal"
		}
		b.WriteString(n.format(v, f))
	}
	m := strings.TrimSpace(b.String())
	if m == "" {
		// A level that shows nothing, as outline numbering of headings often
		// is, has nothing to compare either.
		return "", ""
	}
	return m, "#" + strconv.Itoa(ilvl)
}

// format writes a list number in one of Word's formats. Formats for other
// scripts and number words are shown as decimal numbers, and the document
// gets a note saying so.
func (n *numbering) format(v int, f string) string {
	switch f {
	case "decimal", "":
		return strconv.Itoa(v)
	case "decimalZero":
		if v >= 0 && v < 10 {
			return "0" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	case "upperRoman":
		return roman(v)
	case "lowerRoman":
		return strings.ToLower(roman(v))
	case "upperLetter":
		return letters(v)
	case "lowerLetter":
		return strings.ToLower(letters(v))
	case "ordinal":
		return strconv.Itoa(v) + ordinalSuffix(v)
	case "none":
		return ""
	}
	n.plain = true
	return strconv.Itoa(v)
}

func roman(v int) string {
	if v <= 0 || v >= 4000 {
		return strconv.Itoa(v)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, x := range vals {
		for v >= x {
			b.WriteString(syms[i])
			v -= x
		}
	}
	return b.String()
}

// letters counts as Word does: A to Z, then AA to ZZ, then AAA.
func letters(v int) string {
	// Past ZZZZZZZZ a letter run would grow with the number; a hostile start
	// value of a billion would make a marker of forty million letters.
	if v <= 0 || v > 26*8 {
		return strconv.Itoa(v)
	}
	ch := byte('A' + (v-1)%26)
	return strings.Repeat(string(ch), (v-1)/26+1)
}

func ordinalSuffix(v int) string {
	switch {
	case v%100 >= 11 && v%100 <= 13:
		return "th"
	case v%10 == 1:
		return "st"
	case v%10 == 2:
		return "nd"
	case v%10 == 3:
		return "rd"
	}
	return "th"
}

// bullet maps the bullet characters Word stores in the private use area of
// the Symbol and Wingdings fonts to the characters they look like.
func bullet(text string) string {
	if text == "" {
		return "•"
	}
	r := []rune(text)[0]
	switch r {
	case 0xF0B7:
		return "•"
	case 0xF0A7:
		return "▪"
	case 0xF0D8:
		return "➢"
	case 0xF0FC:
		return "✓"
	case 0xF076:
		return "❖"
	case 0xF06E:
		return "■"
	case 0xF06C:
		return "●"
	case 0xF06F:
		return "□"
	case 0xF0A8:
		return "◻"
	}
	if r >= 0xE000 && r <= 0xF8FF {
		return "•"
	}
	return string(r)
}
