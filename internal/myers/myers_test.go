package myers

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// seq turns a string into one element per byte, which keeps test cases
// readable: "abc" is three elements.
func seq(s string) []int32 {
	out := make([]int32, len(s))
	for i := range s {
		out[i] = int32(s[i])
	}
	return out
}

// apply rebuilds b from a and the script, failing the test if the script is
// inconsistent: matched elements must be equal and both sides fully used.
func apply(t *testing.T, a, b []int32, s Script) []int32 {
	t.Helper()
	if len(s.DelA) != len(a) || len(s.InsB) != len(b) {
		t.Fatalf("script sizes %d/%d for inputs %d/%d", len(s.DelA), len(s.InsB), len(a), len(b))
	}
	var out []int32
	for _, c := range s.Chunks() {
		if c.Match {
			if c.NA != c.NB {
				t.Fatalf("match chunk with %d and %d elements", c.NA, c.NB)
			}
			for k := 0; k < c.NA; k++ {
				if a[c.A+k] != b[c.B+k] {
					t.Fatalf("matched a[%d]=%d with b[%d]=%d", c.A+k, a[c.A+k], c.B+k, b[c.B+k])
				}
			}
		}
		out = append(out, b[c.B:c.B+c.NB]...)
	}
	matchedA, matchedB := 0, 0
	for _, d := range s.DelA {
		if !d {
			matchedA++
		}
	}
	for _, i := range s.InsB {
		if !i {
			matchedB++
		}
	}
	if matchedA != matchedB {
		t.Fatalf("%d elements of a kept but %d of b", matchedA, matchedB)
	}
	return out
}

func edits(s Script) int {
	n := 0
	for _, d := range s.DelA {
		if d {
			n++
		}
	}
	for _, i := range s.InsB {
		if i {
			n++
		}
	}
	return n
}

// lcs is the textbook quadratic dynamic program, the reference that minimal
// scripts are checked against.
func lcs(a, b []int32) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func TestDiff(t *testing.T) {
	tests := []struct {
		a, b  string
		edits int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"a", "b", 2},
		{"a", "a", 0},
		{"abc", "abxc", 1},
		{"abxc", "abc", 1},
		{"abcabba", "cbabac", 5}, // the example of Myers' paper
		{"xaaa", "aaay", 2},
		{"abcdef", "fedcba", 10},
	}
	for _, tt := range tests {
		a, b := seq(tt.a), seq(tt.b)
		s := Diff(a, b)
		if got := apply(t, a, b, s); string(toBytes(got)) != tt.b {
			t.Errorf("%q -> %q: script gives %q", tt.a, tt.b, toBytes(got))
		}
		if e := edits(s); e != tt.edits {
			t.Errorf("%q -> %q: %d edits, want %d", tt.a, tt.b, e, tt.edits)
		}
		if s.Approximate {
			t.Errorf("%q -> %q: approximate", tt.a, tt.b)
		}
	}
}

func toBytes(s []int32) []byte {
	out := make([]byte, len(s))
	for i, v := range s {
		out[i] = byte(v)
	}
	return out
}

func TestChunks(t *testing.T) {
	s := Diff(seq("abXcdY"), seq("abcZd"))
	var got string
	for _, c := range s.Chunks() {
		if c.Match {
			got += fmt.Sprintf("=%d ", c.NA)
		} else {
			got += fmt.Sprintf("-%d+%d ", c.NA, c.NB)
		}
	}
	if want := "=2 -1+0 =1 -0+1 =1 -1+0 "; got != want {
		t.Errorf("chunks %q, want %q", got, want)
	}
}

// TestUnrelatedIsBounded runs two long sequences that share only a few
// values, so that every element occurs on both sides and D is close to N:
// the budget must stop the search and the script must stay valid.
func TestUnrelatedIsBounded(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	a := make([]int32, 50000)
	b := make([]int32, 50000)
	for i := range a {
		a[i] = int32(r.IntN(4))
		b[i] = int32(r.IntN(4))
	}
	s := DiffBudget(a, b, Budget{Cost: 64, Work: 2_000_000})
	apply(t, a, b, s)
	if !s.Approximate {
		t.Error("expected an approximate script")
	}
}

func FuzzDiff(f *testing.F) {
	f.Add([]byte("abcabba"), []byte("cbabac"), uint8(0))
	f.Add([]byte(""), []byte("x"), uint8(1))
	f.Add([]byte("aaaa"), []byte("aa"), uint8(2))
	f.Fuzz(func(t *testing.T, x, y []byte, cost uint8) {
		// A small alphabet makes repeated elements, the hard case.
		a, b := make([]int32, len(x)), make([]int32, len(y))
		for i, c := range x {
			a[i] = int32(c % 5)
		}
		for i, c := range y {
			b[i] = int32(c % 5)
		}
		s := Diff(a, b)
		got := apply(t, a, b, s)
		if !equal(got, b) {
			t.Fatalf("script turns a into %v, not %v", got, b)
		}
		// Small inputs never reach the default budget, so the script must
		// be as short as the longest common subsequence allows.
		if len(a) <= 12 && len(b) <= 12 {
			if want := len(a) + len(b) - 2*lcs(a, b); edits(s) != want || s.Approximate {
				t.Fatalf("%v -> %v: %d edits, minimum %d (approximate %v)", a, b, edits(s), want, s.Approximate)
			}
		}
		// A tiny budget forces the approximate path, which must still give
		// a valid script.
		s = DiffBudget(a, b, Budget{Cost: int(cost%4) + 1, Work: int64(cost) * 8})
		if got := apply(t, a, b, s); !equal(got, b) {
			t.Fatalf("budgeted script turns a into %v, not %v", got, b)
		}
	})
}

func equal(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func BenchmarkFewChanges(bm *testing.B) {
	r := rand.New(rand.NewPCG(3, 4))
	a := make([]int32, 100000)
	for i := range a {
		a[i] = int32(r.IntN(80000))
	}
	b := append([]int32(nil), a...)
	for k := range 300 {
		b[r.IntN(len(b))] = int32(80000 + k)
	}
	for bm.Loop() {
		Diff(a, b)
	}
}

func BenchmarkUnrelated(bm *testing.B) {
	r := rand.New(rand.NewPCG(5, 6))
	a := make([]int32, 100000)
	b := make([]int32, 100000)
	for i := range a {
		a[i] = int32(r.IntN(50))
		b[i] = int32(r.IntN(50))
	}
	for bm.Loop() {
		Diff(a, b)
	}
}
