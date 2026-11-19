package align

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wisebrian/gdco/internal/tokens"
)

// show renders spans as text with [-deleted-] and {+inserted+} marks, the
// notation git's word diff uses.
func show(sp []Span) string {
	var b strings.Builder
	for _, s := range sp {
		switch s.Op {
		case Delete:
			b.WriteString("[-" + s.Text + "-]")
		case Insert:
			b.WriteString("{+" + s.Text + "+}")
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

func TestDiff(t *testing.T) {
	words := Config{Mode: tokens.Words}
	tests := []struct {
		name   string
		c      Config
		a, b   string
		sa, sb string
		wd, wi int
	}{
		{"one word", words,
			"The Supplier shall deliver within 30 days.",
			"The Supplier shall deliver within 45 days.",
			"The Supplier shall deliver within [-30-] days.",
			"The Supplier shall deliver within {+45+} days.", 1, 1},
		{"space between changes folds", words,
			"pay the buyer now", "send a seller now",
			"[-pay the buyer-] now", "{+send a seller+} now", 3, 3},
		{"identical", words, "same", "same", "same", "same", 0, 0},
		{"insert word", words, "a c", "a b c", "a c", "a {+b +}c", 0, 1},
		{"case ignored", Config{Mode: tokens.Words, Norm: tokens.Norm{Case: true}},
			"Hello World x", "hello WORLD y", "Hello World [-x-]", "hello WORLD {+y+}", 1, 1},
		{"space change ignored", Config{Mode: tokens.Words, Norm: tokens.Norm{SpaceChange: true}},
			"a  b c ", "a b d", "a  b [-c-] ", "a b {+d+}", 1, 1},
		{"chars", Config{Mode: tokens.Chars},
			"invoice 1234", "invoice 1235", "invoice 123[-4-]", "invoice 123{+5+}", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sa, sb, wd, wi := Diff(tt.a, tt.b, tt.c)
			if got := show(sa); got != tt.sa {
				t.Errorf("A spans %q, want %q", got, tt.sa)
			}
			if got := show(sb); got != tt.sb {
				t.Errorf("B spans %q, want %q", got, tt.sb)
			}
			if wd != tt.wd || wi != tt.wi {
				t.Errorf("words -%d +%d, want -%d +%d", wd, wi, tt.wd, tt.wi)
			}
		})
	}
}

// TestLongLineIsLinear compares two 200 000-word lines that share only the
// spaces between their words: every space is folded into the changes
// around it. Done by rescanning, as diff-match-patch does, this takes
// minutes; in one pass, a fraction of a second.
func TestLongLineIsLinear(t *testing.T) {
	var a, b strings.Builder
	for i := range 200_000 {
		fmt.Fprintf(&a, "a%d ", i)
		fmt.Fprintf(&b, "b%d ", i)
	}
	start := time.Now()
	sa, sb, wd, wi := Diff(a.String(), b.String(), Config{Mode: tokens.Words})
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	if len(sa) != 2 || len(sb) != 2 || wd != 200_000 || wi != 200_000 {
		t.Fatalf("got %d and %d spans, %d and %d words: want one change per side and its trailing space", len(sa), len(sb), wd, wi)
	}
}

func TestPair(t *testing.T) {
	c := Config{Mode: tokens.Words}
	del := []string{
		"Article 1. Scope of the agreement",
		"Article 2. Payment within 30 days",
		"Article 3. Termination with notice",
	}
	ins := []string{
		"Article 1. Scope of this agreement",
		"Completely new paragraph about something else entirely",
		"Article 3. Termination with written notice",
	}
	got := Pair(del, ins, c)
	want := []Step{{0, 0}, {1, -1}, {-1, 1}, {2, 2}}
	if len(got) != len(want) {
		t.Fatalf("Pair = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Pair = %v, want %v", got, want)
		}
	}
}

func TestPairShapes(t *testing.T) {
	c := Config{Mode: tokens.Words}
	if got := Pair([]string{"x"}, []string{"completely different"}, c); len(got) != 1 || got[0] != (Step{0, 0}) {
		t.Errorf("one for one = %v, want a pair", got)
	}
	if got := Pair([]string{"a", "b"}, nil, c); len(got) != 2 || got[0] != (Step{0, -1}) || got[1] != (Step{1, -1}) {
		t.Errorf("deletions only = %v", got)
	}
	// A large block takes the greedy path and still keeps both orders.
	var a, b []string
	for i := range 100 {
		a = append(a, "line number "+strings.Repeat("x", i%7)+" of the old text")
		b = append(b, "line number "+strings.Repeat("x", i%7)+" of the new text")
	}
	checkSteps(t, Pair(a, b, c), len(a), len(b))
}

// checkSteps verifies that steps use every line exactly once, in order.
func checkSteps(t *testing.T, steps []Step, na, nb int) {
	t.Helper()
	ia, ib := 0, 0
	for _, s := range steps {
		if s.A >= 0 {
			if s.A != ia {
				t.Fatalf("A line %d out of order (want %d) in %v", s.A, ia, steps)
			}
			ia++
		}
		if s.B >= 0 {
			if s.B != ib {
				t.Fatalf("B line %d out of order (want %d) in %v", s.B, ib, steps)
			}
			ib++
		}
		if s.A < 0 && s.B < 0 {
			t.Fatalf("empty step in %v", steps)
		}
	}
	if ia != na || ib != nb {
		t.Fatalf("steps use %d/%d lines of %d/%d", ia, ib, na, nb)
	}
}

func FuzzDiff(f *testing.F) {
	f.Add("The quick brown fox", "The slow brown dog", uint8(0))
	f.Add("a  b\t", "a b", uint8(2))
	f.Add("", "x", uint8(1))
	f.Add("éé́", "Éé", uint8(5))
	f.Fuzz(func(t *testing.T, a, b string, opt uint8) {
		a = strings.ToValidUTF8(a, "�")
		b = strings.ToValidUTF8(b, "�")
		c := Config{Mode: tokens.Mode(opt & 1), Norm: tokens.Norm{
			Case: opt&2 != 0, SpaceChange: opt&4 != 0, AllSpace: opt&8 != 0, TrailingSpace: opt&16 != 0,
		}}
		sa, sb, wd, wi := Diff(a, b, c)
		check := func(sp []Span, line string, changed Op) {
			var j strings.Builder
			for i, s := range sp {
				if s.Text == "" {
					t.Fatalf("empty span in %v", sp)
				}
				if s.Op != Equal && s.Op != changed {
					t.Fatalf("span op %d on the wrong side", s.Op)
				}
				if i > 0 && sp[i-1].Op == s.Op {
					t.Fatalf("neighbouring spans share op %d: %v", s.Op, sp)
				}
				j.WriteString(s.Text)
			}
			if j.String() != line {
				t.Fatalf("spans join to %q, not %q", j.String(), line)
			}
		}
		check(sa, a, Delete)
		check(sb, b, Insert)
		if wd < 0 || wi < 0 || wd > Words(a, c) || wi > Words(b, c) {
			t.Fatalf("word counts -%d +%d out of range", wd, wi)
		}
		// Lines equal under the options have nothing to highlight. (With
		// all white space ignored, splitting a word in two is still a
		// change at word level, so that mode is left out here.)
		if !c.Norm.AllSpace && string(c.Norm.LineKey(nil, a)) == string(c.Norm.LineKey(nil, b)) {
			for _, s := range append(sa, sb...) {
				if s.Op != Equal {
					t.Fatalf("equal lines %q and %q got highlights %q / %q", a, b, show(sa), show(sb))
				}
			}
		}
	})
}

func FuzzPair(f *testing.F) {
	f.Add("a\nb\nc", "a\nx\nc")
	f.Add("one\ntwo", "")
	f.Fuzz(func(t *testing.T, a, b string) {
		la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
		checkSteps(t, Pair(la, lb, Config{}), len(la), len(lb))
	})
}
