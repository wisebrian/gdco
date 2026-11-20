// Package myers computes an edit script between two sequences with Myers'
// O(ND) algorithm ("An O(ND) Difference Algorithm and Its Variations",
// 1986) in its linear-space form: each step searches from both ends at once
// for the middle snake of an optimal path, then solves the two halves, so
// memory stays proportional to the input rather than to N×D.
//
// Unrelated inputs make D approach N, and O(ND) then means minutes. A cost
// budget handles that the way GNU diff and git do: a search that runs past
// it settles for the furthest point it reached. The script stays valid but
// may be longer than necessary, and Script.Approximate says so.
package myers

import "math"

// Script marks the elements of a that are deleted and the elements of b
// that are inserted. The others match one to one, in order.
type Script struct {
	DelA, InsB  []bool
	Approximate bool
}

// Budget bounds the work Diff may do. Zero fields are derived from the input
// size.
type Budget struct {
	// Cost is the number of edit steps one middle-snake search may take
	// before it settles for the best point it has reached.
	Cost int
	// Work is the number of steps the whole diff may take; past it, what
	// is left is reported as replaced outright.
	Work int64
}

// Diff returns a script that turns a into b. Elements are compared as
// integers: callers intern lines or words first, which makes each
// comparison a single instruction however long the line.
func Diff(a, b []int32) Script { return DiffBudget(a, b, Budget{}) }

// DiffBudget is Diff with explicit bounds, for tests that need the
// approximate path on small inputs.
func DiffBudget(a, b []int32, bud Budget) Script {
	n, m := len(a), len(b)
	s := Script{DelA: make([]bool, n), InsB: make([]bool, m)}

	// The common prefix and suffix need no search.
	lo := 0
	for lo < n && lo < m && a[lo] == b[lo] {
		lo++
	}
	hiA, hiB := n, m
	for hiA > lo && hiB > lo && a[hiA-1] == b[hiB-1] {
		hiA--
		hiB--
	}
	if lo == hiA || lo == hiB {
		mark(s.DelA[lo:hiA])
		mark(s.InsB[lo:hiB])
		return s
	}

	// An element missing from the other side can never be matched, so
	// leaving it out of the search keeps the script minimal and turns two
	// unrelated texts, whose lines are mostly unique, into a small problem.
	ra, ia := keep(a[lo:hiA], b[lo:hiB], s.DelA[lo:hiA])
	rb, ib := keep(b[lo:hiB], a[lo:hiA], s.InsB[lo:hiB])

	d := newDiffer(ra, rb, bud)
	d.run()
	for i, del := range d.del {
		if del {
			s.DelA[lo+ia[i]] = true
		}
	}
	for j, ins := range d.ins {
		if ins {
			s.InsB[lo+ib[j]] = true
		}
	}
	s.Approximate = d.approx
	return s
}

func mark(f []bool) {
	for i := range f {
		f[i] = true
	}
}

// keep returns the elements of x that also occur in y, with their indexes
// in x, and marks the others in gone.
func keep(x, y []int32, gone []bool) ([]int32, []int) {
	present := presence(y)
	var kx []int32
	var idx []int
	for i, v := range x {
		if present(v) {
			kx = append(kx, v)
			idx = append(idx, i)
		} else {
			gone[i] = true
		}
	}
	return kx, idx
}

// presence returns a membership test for the values of y. Interned values
// are small and dense, so a bit per value is enough; anything else falls
// back to a map.
func presence(y []int32) func(int32) bool {
	maxV := int32(-1)
	dense := true
	for _, v := range y {
		if v < 0 || int(v) > 4*len(y)+1024 {
			dense = false
			break
		}
		maxV = max(maxV, v)
	}
	if dense {
		set := make([]bool, int(maxV)+1)
		for _, v := range y {
			set[v] = true
		}
		return func(v int32) bool { return v >= 0 && int(v) < len(set) && set[v] }
	}
	set := make(map[int32]struct{}, len(y))
	for _, v := range y {
		set[v] = struct{}{}
	}
	return func(v int32) bool { _, ok := set[v]; return ok }
}

type differ struct {
	a, b     []int32
	del, ins []bool
	// fwd and bwd hold, for each diagonal k = x-y, the furthest x the
	// forward and the backward searches have reached; off shifts k so
	// that negative diagonals index the slices.
	fwd, bwd []int
	off      int
	maxCost  int
	work     int64
	maxWork  int64
	approx   bool
}

func newDiffer(a, b []int32, bud Budget) *differ {
	n, m := len(a), len(b)
	d := &differ{
		a: a, b: b,
		del: make([]bool, n), ins: make([]bool, m),
		fwd: make([]int, n+m+3), bwd: make([]int, n+m+3),
		off:     m + 1,
		maxCost: bud.Cost,
		maxWork: bud.Work,
	}
	if d.maxCost <= 0 {
		// Big enough that a few hundred changes in a long file are always
		// found exactly; growing with the square root of the size, as git
		// and GNU diff do, keeps the worst case near N·√N.
		d.maxCost = max(1024, 2*int(math.Sqrt(float64(n+m))))
	}
	if d.maxWork <= 0 {
		// About a fifth of a second natively for the worst case, two long
		// unrelated texts made of the same few lines; an edited file stays
		// far below it.
		d.maxWork = 30_000_000 + 100*int64(n+m)
	}
	return d
}

type box struct {
	xoff, xlim, yoff, ylim int
	minimal                bool
}

// run solves the whole problem with an explicit stack: recursion depth
// would depend on the input, and a Go program cannot recover from running
// out of stack.
func (d *differ) run() {
	stack := []box{{0, len(d.a), 0, len(d.b), false}}
	for len(stack) > 0 {
		bx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		xoff, xlim, yoff, ylim := bx.xoff, bx.xlim, bx.yoff, bx.ylim
		for xoff < xlim && yoff < ylim && d.a[xoff] == d.b[yoff] {
			xoff++
			yoff++
		}
		for xlim > xoff && ylim > yoff && d.a[xlim-1] == d.b[ylim-1] {
			xlim--
			ylim--
		}
		switch {
		case xoff == xlim || yoff == ylim:
			mark(d.del[xoff:xlim])
			mark(d.ins[yoff:ylim])
		case !bx.minimal && d.work > d.maxWork:
			d.approx = true
			mark(d.del[xoff:xlim])
			mark(d.ins[yoff:ylim])
		default:
			xm, ym, loMin, hiMin := d.split(xoff, xlim, yoff, ylim, bx.minimal)
			stack = append(stack,
				box{xm, xlim, ym, ylim, hiMin},
				box{xoff, xm, yoff, ym, loMin})
		}
	}
}

// split finds a point on an optimal path through the box and reports, for
// each half, whether it is known to be cheap enough to solve exactly. Both
// halves are when the middle snake was found: each then costs less than the
// budget. After settling for a best-so-far point, only the half the search
// actually crossed is.
func (d *differ) split(xoff, xlim, yoff, ylim int, minimal bool) (xmid, ymid int, loMin, hiMin bool) {
	fwd, bwd, off := d.fwd, d.bwd, d.off
	dmin, dmax := xoff-ylim, xlim-yoff // the diagonals the box contains
	fmid, bmid := xoff-yoff, xlim-ylim // where each search starts
	fmin, fmax, bmin, bmax := fmid, fmid, bmid, bmid
	odd := (fmid-bmid)&1 != 0 // the searches can only meet after a forward step
	fwd[off+fmid] = xoff
	bwd[off+bmid] = xlim

	for c := 1; ; c++ {
		// Widen the forward search by a diagonal on each side where the box
		// allows it, writing a sentinel just outside so the moves below
		// never come from a diagonal that was not searched.
		if fmin > dmin {
			fmin--
			fwd[off+fmin-1] = -1
		} else {
			fmin++
		}
		if fmax < dmax {
			fmax++
			fwd[off+fmax+1] = -1
		} else {
			fmax--
		}
		for k := fmax; k >= fmin; k -= 2 {
			// Arrive on k by deleting (from k-1) or inserting (from
			// k+1), whichever gets further, then follow matches.
			x := fwd[off+k-1] + 1
			if from := fwd[off+k+1]; from >= x {
				x = from
			}
			x0 := x
			y := x - k
			for x < xlim && y < ylim && d.a[x] == d.b[y] {
				x++
				y++
			}
			d.work += int64(x-x0) + 1
			fwd[off+k] = x
			if odd && bmin <= k && k <= bmax && bwd[off+k] <= x {
				return x, y, true, true
			}
		}

		// The same, backwards from the far corner.
		if bmin > dmin {
			bmin--
			bwd[off+bmin-1] = math.MaxInt
		} else {
			bmin++
		}
		if bmax < dmax {
			bmax++
			bwd[off+bmax+1] = math.MaxInt
		} else {
			bmax--
		}
		for k := bmax; k >= bmin; k -= 2 {
			x := bwd[off+k+1] - 1
			if from := bwd[off+k-1]; from <= x {
				x = from
			}
			x0 := x
			y := x - k
			for x > xoff && y > yoff && d.a[x-1] == d.b[y-1] {
				x--
				y--
			}
			d.work += int64(x0-x) + 1
			bwd[off+k] = x
			if !odd && fmin <= k && k <= fmax && x <= fwd[off+k] {
				return x, y, true, true
			}
		}

		if minimal || (c < d.maxCost && d.work <= d.maxWork) {
			continue
		}

		// Over budget: split at whichever search made the most progress.
		d.approx = true
		fbest, fx := -1, 0
		for k := fmax; k >= fmin; k -= 2 {
			x := min(fwd[off+k], xlim)
			y := x - k
			if y > ylim {
				x, y = ylim+k, ylim
			}
			if x+y > fbest {
				fbest, fx = x+y, x
			}
		}
		bbest, bx := math.MaxInt, 0
		for k := bmax; k >= bmin; k -= 2 {
			x := max(bwd[off+k], xoff)
			y := x - k
			if y < yoff {
				x, y = yoff+k, yoff
			}
			if x+y < bbest {
				bbest, bx = x+y, x
			}
		}
		if (xlim+ylim)-bbest < fbest-(xoff+yoff) {
			return fx, fbest - fx, true, false
		}
		return bx, bbest - bx, false, true
	}
}

// Chunk is either a run of matching elements, a[A:A+NA] equal to
// b[B:B+NB] with NA == NB, or a block of changes replacing a[A:A+NA] with
// b[B:B+NB].
type Chunk struct {
	A, B   int
	NA, NB int
	Match  bool
}

// Chunks groups the script into alternating matches and change blocks, the
// shape the rest of the comparison works in.
func (s Script) Chunks() []Chunk {
	var out []Chunk
	n, m := len(s.DelA), len(s.InsB)
	i, j := 0, 0
	for i < n || j < m {
		i0, j0 := i, j
		if i < n && j < m && !s.DelA[i] && !s.InsB[j] {
			for i < n && j < m && !s.DelA[i] && !s.InsB[j] {
				i++
				j++
			}
			out = append(out, Chunk{A: i0, B: j0, NA: i - i0, NB: j - j0, Match: true})
			continue
		}
		for i < n && s.DelA[i] {
			i++
		}
		for j < m && s.InsB[j] {
			j++
		}
		if i == i0 && j == j0 {
			// Unmatched elements left on one side only: not a script
			// Diff produces, but the rest is reported as changed rather
			// than looping.
			i, j = n, m
		}
		out = append(out, Chunk{A: i0, B: j0, NA: i - i0, NB: j - j0})
	}
	return out
}
