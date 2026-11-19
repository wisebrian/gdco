package gdco

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

var vocabulary = strings.Fields(`the supplier shall deliver goods within thirty days of
order and the buyer will pay invoice amount net price contract party agreement terms
clause notice written consent liability damages force majeure termination period
renewal annex schedule services fees confidential information law court jurisdiction`)

// bigText makes n lines of about 50 characters of prose, about 5 MB for
// 100 000 lines, with no two lines alike, as in a long contract.
func bigText(r *rand.Rand, n int) []string {
	lines := make([]string, n)
	var b strings.Builder
	for i := range lines {
		b.Reset()
		b.WriteString("§")
		b.WriteString(strings.Repeat("x", i%3))
		for b.Len() < 50 {
			b.WriteByte(' ')
			b.WriteString(vocabulary[r.IntN(len(vocabulary))])
		}
		lines[i] = b.String()
	}
	return lines
}

// edited applies k random edits: a third replace a word in a line, a third
// insert a line, a third delete one.
func edited(r *rand.Rand, lines []string, k int) []string {
	out := append([]string(nil), lines...)
	for range k {
		i := r.IntN(len(out))
		switch r.IntN(3) {
		case 0:
			w := strings.Fields(out[i])
			w[r.IntN(len(w))] = "CHANGED"
			out[i] = strings.Join(w, " ")
		case 1:
			out = append(out[:i], append([]string{"a new line of text inserted here"}, out[i:]...)...)
		default:
			out = append(out[:i], out[i+1:]...)
		}
	}
	return out
}

func join(lines []string) string { return strings.Join(lines, "\n") + "\n" }

func largePair(b testing.TB) (string, string) {
	r := rand.New(rand.NewPCG(11, 12))
	a := bigText(r, 100_000)
	return join(a), join(edited(r, a, 300))
}

// BenchmarkLargeFewChanges is the first performance target: two 5 MB,
// 100 000-line texts with a few hundred changes, compared in under a
// second.
func BenchmarkLargeFewChanges(b *testing.B) {
	a, c := largePair(b)
	b.SetBytes(int64(len(a) + len(c)))
	for b.Loop() {
		if _, err := CompareText(a, c, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLargeUnifiedAndHTML(b *testing.B) {
	a, c := largePair(b)
	r, err := CompareText(a, c, nil)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		var sb strings.Builder
		if err := r.WriteUnified(&sb); err != nil {
			b.Fatal(err)
		}
		if err := r.WriteHTML(&sb, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUnrelated compares two 100 000-line texts that share their
// lines but not their order, the worst case for the alignment: the cost
// budget has to bound it.
func BenchmarkUnrelated(b *testing.B) {
	r := rand.New(rand.NewPCG(13, 14))
	pool := bigText(r, 60)
	mk := func() string {
		lines := make([]string, 100_000)
		for i := range lines {
			lines[i] = pool[r.IntN(len(pool))]
		}
		return join(lines)
	}
	a, c := mk(), mk()
	for b.Loop() {
		res, err := CompareText(a, c, nil)
		if err != nil {
			b.Fatal(err)
		}
		if !res.Approximate {
			b.Fatal("expected an approximate result")
		}
	}
}

// TestLargeIsFast checks the first performance target directly, with room
// for a slow machine; the benchmarks give the real figures.
func TestLargeIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	a, c := largePair(t)
	start := time.Now()
	r, err := CompareText(a, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("comparing two 5 MB texts took %v", d)
	}
	if r.Approximate {
		t.Error("a few hundred changes must be found exactly")
	}
	if r.Stats.LinesChanged+r.Stats.LinesAdded+r.Stats.LinesRemoved < 250 {
		t.Errorf("stats %+v: the edits were not found", r.Stats)
	}
}
