// Package align turns line-level change blocks into what a reader looks
// at: it pairs deleted and inserted lines that are versions of each other,
// and marks the words that changed inside each pair.
package align

import (
	"hash/fnv"
	"slices"
	"unicode/utf8"

	"github.com/wisebrian/gdco/internal/myers"
	"github.com/wisebrian/gdco/internal/tokens"
)

// Op says whether a span is unchanged, gone from the first line or new in
// the second.
type Op uint8

// The span operations.
const (
	Equal Op = iota
	Delete
	Insert
)

// Span is a piece of one side of a pair. The spans of a side, joined, give
// that line back.
type Span struct {
	Op   Op
	Text string
}

// Config carries the comparison options that matter inside a line.
type Config struct {
	Mode tokens.Mode
	Norm tokens.Norm
}

// side is one line cut into tokens, with what the diff needs about each.
type side struct {
	toks  []tokens.Token
	skip  []bool // takes no part in the comparison (ignored white space)
	ids   []int32
	where []int // for each compared token, its index in toks
	runes []int // prefix sums of rune counts over the compared tokens
}

// Aligner compares lines with one configuration, reusing its buffers from
// one pair of lines to the next: a long, heavily edited file has tens of
// thousands of pairs, and allocating afresh for each one costs more than
// the comparison itself.
type Aligner struct {
	c       Config
	intern  map[string]int32
	scratch []byte
	a, b    side
	toks    []tokens.Token
}

// New returns an Aligner for c.
func New(c Config) *Aligner {
	return &Aligner{c: c, intern: make(map[string]int32)}
}

func (al *Aligner) prepare(sd *side, s string) {
	c := al.c
	sd.toks = tokens.Split(sd.toks[:0], s, c.Mode, c.Norm.Case)
	sd.skip = slices.Grow(sd.skip[:0], len(sd.toks))[:len(sd.toks)]
	clear(sd.skip)
	sd.ids, sd.where, sd.runes = sd.ids[:0], sd.where[:0], append(sd.runes[:0], 0)
	for i, t := range sd.toks {
		if c.Norm.Skip(sd.toks, i) {
			sd.skip[i] = true
			continue
		}
		al.scratch = c.Norm.TokenKey(al.scratch[:0], t)
		id, ok := al.intern[string(al.scratch)]
		if !ok {
			id = int32(len(al.intern))
			al.intern[string(al.scratch)] = id
		}
		sd.ids = append(sd.ids, id)
		sd.where = append(sd.where, i)
		sd.runes = append(sd.runes, sd.runes[len(sd.runes)-1]+utf8.RuneCountInString(t.Text))
	}
}

// Diff compares two lines with a throwaway Aligner.
func Diff(a, b string, c Config) (sa, sb []Span, wordsDel, wordsIns int) {
	return New(c).Diff(a, b)
}

// Diff compares two lines token by token. It returns the spans of each
// side and how many words each side lost or gained, for the statistics.
func (al *Aligner) Diff(a, b string) (sa, sb []Span, wordsDel, wordsIns int) {
	clear(al.intern)
	pa, pb := &al.a, &al.b
	al.prepare(pa, a)
	al.prepare(pb, b)
	script := myers.Diff(pa.ids, pb.ids)

	var rs []run
	for _, ch := range script.Chunks() {
		if ch.Match {
			rs = append(rs, run{op: Equal, a0: ch.A, a1: ch.A + ch.NA, b0: ch.B, b1: ch.B + ch.NB})
			continue
		}
		if ch.NA > 0 {
			rs = append(rs, run{op: Delete, a0: ch.A, a1: ch.A + ch.NA})
		}
		if ch.NB > 0 {
			rs = append(rs, run{op: Insert, b0: ch.B, b1: ch.B + ch.NB})
		}
	}
	cleanup(rs, pa.runes, pb.runes)

	opsA := make([]Op, len(pa.ids))
	opsB := make([]Op, len(pb.ids))
	for _, r := range rs {
		if r.op == Delete || r.op == replace {
			fill(opsA[r.a0:r.a1], Delete)
		}
		if r.op == Insert || r.op == replace {
			fill(opsB[r.b0:r.b1], Insert)
		}
	}
	sa = spans(a, pa, opsA, Delete)
	sb = spans(b, pb, opsB, Insert)
	return sa, sb, al.changedWords(a, sa), al.changedWords(b, sb)
}

// changedWords counts the words of line that a changed span touches. Words
// are counted as words whatever the granularity, so the statistics stay the
// same when the reader switches to character highlighting.
func (al *Aligner) changedWords(line string, sp []Span) int {
	type interval struct {
		start, end int
		changed    bool
	}
	ivs := make([]interval, 0, len(sp))
	pos := 0
	for _, s := range sp {
		ivs = append(ivs, interval{pos, pos + len(s.Text), s.Op != Equal})
		pos += len(s.Text)
	}
	n, k, p := 0, 0, 0
	al.toks = tokens.Split(al.toks[:0], line, tokens.Words, al.c.Norm.Case)
	for _, t := range al.toks {
		start, end := p, p+len(t.Text)
		p = end
		if !t.Kind.IsWord() {
			continue
		}
		for k < len(ivs) && ivs[k].end <= start {
			k++
		}
		for j := k; j < len(ivs) && ivs[j].start < end; j++ {
			if ivs[j].changed {
				n++
				break
			}
		}
	}
	return n
}

func fill(ops []Op, op Op) {
	for i := range ops {
		ops[i] = op
	}
}

// spans gives every token of the line its operation and joins neighbours
// that share one. A skipped token (ignored white space) is unchanged,
// except between two changed tokens, where a gap in the highlight would
// only look like a separate change.
func spans(s string, sd *side, ops []Op, changed Op) []Span {
	tokOps := make([]Op, len(sd.toks))
	for k, i := range sd.where {
		tokOps[i] = ops[k]
	}
	for i := range sd.toks {
		if sd.skip[i] && i > 0 && i+1 < len(sd.toks) && tokOps[i-1] == changed && tokOps[i+1] == changed && !sd.skip[i+1] {
			tokOps[i] = changed
		}
	}
	var out []Span
	pos := 0
	for i, t := range sd.toks {
		op := tokOps[i]
		end := pos + len(t.Text)
		if n := len(out); n > 0 && out[n-1].Op == op {
			out[n-1].Text = s[pos-len(out[n-1].Text) : end]
		} else {
			out = append(out, Span{Op: op, Text: s[pos:end]})
		}
		pos = end
	}
	return out
}

// Words counts the words of a whole line that was inserted or deleted.
func Words(s string, c Config) int { return tokens.CountWords(s, c.Norm.Case) }

// run is a stretch of the joint diff: equal tokens on both sides, or
// tokens only one side has. Ranges index the compared tokens.
type run struct {
	op     Op
	a0, a1 int
	b0, b1 int
}

// replace marks an unchanged run that cleanup folded into the changes
// around it: deleted on side A and inserted on side B.
const replace Op = 3

// cleanup is the semantic cleanup of Neil Fraser's diff-match-patch: an
// unchanged stretch no longer than the changes on both sides of it is
// folded into them. A minimal diff of two rewritten sentences matches every
// stray "the" and space; folding those gives one readable change instead
// of a highlight broken into fragments.
//
// Folding a stretch makes the change around it longer, which can qualify
// the stretch before it in turn. The original rescans from that earlier
// stretch each time, which is quadratic on a long line where every stretch
// folds, such as two minified files of several megabytes. Here the
// stretches still standing wait on a stack, the one before is on top when
// it needs judging again, and each is folded at most once: one linear
// pass.
func cleanup(rs []run, ra, rb []int) {
	type standing struct {
		i          int // index of an unchanged run in rs
		w          int // its length in characters
		insL, delL int // the changes between it and the stretch before it
	}
	var stack []standing
	var ins, del int // changes since the stretch on top of the stack
	settle := func() {
		for len(stack) > 0 {
			s := stack[len(stack)-1]
			if s.w > max(s.insL, s.delL) || s.w > max(ins, del) {
				return
			}
			e := &rs[s.i]
			e.op = replace
			ins += s.insL + rb[e.b1] - rb[e.b0]
			del += s.delL + ra[e.a1] - ra[e.a0]
			stack = stack[:len(stack)-1]
		}
	}
	for i, r := range rs {
		switch r.op {
		case Equal:
			settle()
			stack = append(stack, standing{i, ra[r.a1] - ra[r.a0], ins, del})
			ins, del = 0, 0
		case Insert:
			ins += rb[r.b1] - rb[r.b0]
		case Delete:
			del += ra[r.a1] - ra[r.a0]
		}
	}
	settle()
}

// Step is one row of an aligned change block: A and B index the block's
// deleted and inserted lines, and -1 means the row has no line on that
// side.
type Step struct{ A, B int }

// threshold is the similarity below which two lines are shown as one
// deleted and one inserted rather than one modified line. Below it, the
// highlighting would cover most of both lines and say nothing.
const threshold = 0.4

// Pair aligns the deleted lines a with the inserted lines b of one change
// block, keeping both orders, so that each line is shown next to the
// version it became.
func Pair(a, b []string, c Config) []Step {
	switch {
	case len(a) == 0 || len(b) == 0:
		return unpaired(len(a), len(b), nil)
	case len(a) == 1 && len(b) == 1:
		// One line replaced by one line: whatever the similarity, the two
		// are versions of each other.
		return []Step{{0, 0}}
	case len(a)+len(b) > 20000:
		// Only unrelated inputs make blocks this large; pairing them would
		// cost more than it shows.
		return unpaired(len(a), len(b), nil)
	}
	fa := make([]profile, len(a))
	fb := make([]profile, len(b))
	for i, s := range a {
		fa[i] = c.profile(s)
	}
	for j, s := range b {
		fb[j] = c.profile(s)
	}
	if len(a)*len(b) <= 4096 {
		return bestPairs(fa, fb)
	}
	return greedyPairs(fa, fb)
}

func unpaired(na, nb int, out []Step) []Step {
	for i := range na {
		out = append(out, Step{i, -1})
	}
	for j := range nb {
		out = append(out, Step{-1, j})
	}
	return out
}

// bestPairs finds the order-keeping pairing with the greatest total
// similarity, by dynamic programming over the whole block.
func bestPairs(fa, fb []profile) []Step {
	n, m := len(fa), len(fb)
	score := make([]float64, (n+1)*(m+1))
	at := func(i, j int) *float64 { return &score[i*(m+1)+j] }
	sim := make([]float64, n*m)
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			best := max(*at(i-1, j), *at(i, j-1))
			s := similarity(fa[i-1], fb[j-1])
			sim[(i-1)*m+j-1] = s
			if s >= threshold {
				best = max(best, *at(i-1, j-1)+s)
			}
			*at(i, j) = best
		}
	}
	var rev []Step
	i, j := n, m
	for i > 0 && j > 0 {
		s := sim[(i-1)*m+j-1]
		switch {
		case s >= threshold && *at(i, j) == *at(i-1, j-1)+s:
			rev = append(rev, Step{i - 1, j - 1})
			i--
			j--
		case *at(i, j) == *at(i-1, j):
			rev = append(rev, Step{i - 1, -1})
			i--
		default:
			rev = append(rev, Step{-1, j - 1})
			j--
		}
	}
	for ; i > 0; i-- {
		rev = append(rev, Step{i - 1, -1})
	}
	for ; j > 0; j-- {
		rev = append(rev, Step{-1, j - 1})
	}
	slices.Reverse(rev)
	return order(rev)
}

// greedyPairs handles blocks too large for the full program: each deleted
// line looks for its best match among the next few inserted lines.
func greedyPairs(fa, fb []profile) []Step {
	const window = 8
	var out []Step
	j0 := 0
	for i := range fa {
		best, bj := threshold, -1
		for j := j0; j < len(fb) && j < j0+window; j++ {
			if s := similarity(fa[i], fb[j]); s >= best {
				best, bj = s, j
			}
		}
		if bj < 0 {
			out = append(out, Step{i, -1})
			continue
		}
		for ; j0 < bj; j0++ {
			out = append(out, Step{-1, j0})
		}
		out = append(out, Step{i, bj})
		j0 = bj + 1
	}
	for ; j0 < len(fb); j0++ {
		out = append(out, Step{-1, j0})
	}
	return order(out)
}

// order puts, between two pairs, the lone deleted lines before the lone
// inserted ones, the order a unified diff prints them in.
func order(steps []Step) []Step {
	out := make([]Step, 0, len(steps))
	for i := 0; i < len(steps); {
		if steps[i].A >= 0 && steps[i].B >= 0 {
			out = append(out, steps[i])
			i++
			continue
		}
		j := i
		for j < len(steps) && (steps[j].A < 0 || steps[j].B < 0) {
			j++
		}
		for _, s := range steps[i:j] {
			if s.B < 0 {
				out = append(out, s)
			}
		}
		for _, s := range steps[i:j] {
			if s.A < 0 {
				out = append(out, s)
			}
		}
		i = j
	}
	return out
}

// profile is what similarity needs of a line: the hashes of its word and
// punctuation keys, sorted, with their lengths. White space is left out so
// that re-indenting a line does not make it look different.
type profile struct {
	keys   []keyLen
	weight int
}

type keyLen struct {
	h uint64
	n int
}

func (c Config) profile(s string) profile {
	var p profile
	var buf []byte
	for _, t := range tokens.Split(nil, s, tokens.Words, c.Norm.Case) {
		if t.Kind == tokens.Space {
			continue
		}
		buf = c.Norm.TokenKey(buf[:0], t)
		h := fnv.New64a()
		h.Write(buf)
		n := utf8.RuneCount(buf)
		p.keys = append(p.keys, keyLen{h.Sum64(), n})
		p.weight += n
	}
	slices.SortFunc(p.keys, func(x, y keyLen) int {
		switch {
		case x.h < y.h:
			return -1
		case x.h > y.h:
			return 1
		}
		return 0
	})
	return p
}

// similarity is the Dice coefficient of the two lines' words weighted by
// length: twice the shared text over the total, from 0 (nothing shared) to
// 1. Lines with no words at all count as similar to each other.
func similarity(a, b profile) float64 {
	if a.weight+b.weight == 0 {
		return 1
	}
	common := 0
	for i, j := 0, 0; i < len(a.keys) && j < len(b.keys); {
		switch {
		case a.keys[i].h < b.keys[j].h:
			i++
		case a.keys[i].h > b.keys[j].h:
			j++
		default:
			common += a.keys[i].n
			i++
			j++
		}
	}
	return 2 * float64(common) / float64(a.weight+b.weight)
}
