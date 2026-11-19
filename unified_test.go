package gdco

import (
	"bytes"
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tool applies a unified diff to orig and returns the patched text.
type tool struct {
	name  string
	apply func(t *testing.T, orig, diff string, ctx int) (string, error)
}

// tools returns the patch programs installed, so the property can be
// checked against real implementations rather than against this package's
// own reading of the format.
func tools(t *testing.T) []tool {
	var ts []tool
	if p, err := exec.LookPath("patch"); err == nil {
		ts = append(ts, tool{"patch", func(t *testing.T, orig, diff string, _ int) (string, error) {
			dir := t.TempDir()
			f, d, o := filepath.Join(dir, "f"), filepath.Join(dir, "d.patch"), filepath.Join(dir, "out")
			write(t, f, orig)
			write(t, d, diff)
			// -f never asks a question, -F0 forbids applying a hunk anywhere
			// but where it says.
			if out, err := run(dir, p, "-f", "-s", "-F0", "-o", o, f, d); err != nil {
				return "", fmtErr(err, out)
			}
			b, err := os.ReadFile(o)
			return string(b), err
		}})
	}
	if g, err := exec.LookPath("git"); err == nil {
		ts = append(ts, tool{"git apply", func(t *testing.T, orig, diff string, ctx int) (string, error) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "f"), orig)
			write(t, filepath.Join(dir, "d.patch"), diff)
			args := []string{"apply", "--whitespace=nowarn"}
			if ctx == 0 {
				args = append(args, "--unidiff-zero")
			}
			if out, err := run(dir, g, append(args, "d.patch")...); err != nil {
				return "", fmtErr(err, out)
			}
			b, err := os.ReadFile(filepath.Join(dir, "f"))
			return string(b), err
		}})
	}
	if len(ts) == 0 {
		t.Skip("neither patch nor git is installed; skipping the patch-application property")
	}
	return ts
}

func write(t *testing.T, name, s string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func run(dir, prog string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type cmdError struct {
	err error
	out string
}

func (e cmdError) Error() string { return e.err.Error() + ": " + e.out }

func fmtErr(err error, out string) error { return cmdError{err, out} }

// checkApplies diffs a and b, applies the diff to a with every tool and
// requires b back, byte for byte.
func checkApplies(t *testing.T, ts []tool, a, b string, ctx int) {
	t.Helper()
	o := DefaultOptions()
	o.Context = ctx
	r, err := Compare(TextDocument("a/f", a), TextDocument("b/f", b), o)
	if err != nil {
		t.Fatal(err)
	}
	diff := unified(t, r)
	if (diff == "") != (a == b) {
		t.Fatalf("diff of %q and %q is %q", a, b, diff)
	}
	if diff == "" {
		return
	}
	for _, tl := range ts {
		got, err := tl.apply(t, a, diff, ctx)
		if err != nil {
			t.Fatalf("%s failed on\n%s\nA=%q\nB=%q\n%v", tl.name, diff, a, b, err)
		}
		if got != b {
			t.Fatalf("%s gave %q, want %q, from diff\n%s", tl.name, got, b, diff)
		}
	}
}

var linePool = []string{"alpha", "beta", "gamma beta", "delta", "", "  indented", "x y z", "alpha"}

// randomText builds a text from a small pool of lines, so that repeated
// lines, the hard case for alignment, are common.
func randomText(r *rand.Rand, n int, eol string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(linePool[r.IntN(len(linePool))])
		if i < n-1 || r.IntN(3) > 0 {
			b.WriteString(eol)
		}
	}
	return b.String()
}

// edit derives a text from s by a few random line edits, so that a and b
// are related, as real versions of a file are.
func edit(r *rand.Rand, s, eol string) string {
	lines := strings.SplitAfter(s, eol)
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for k := r.IntN(4); k >= 0; k-- {
		pos := r.IntN(len(lines) + 1)
		line := linePool[r.IntN(len(linePool))] + eol
		switch r.IntN(3) {
		case 0:
			lines = append(lines[:pos], append([]string{line}, lines[pos:]...)...)
		case 1:
			if pos < len(lines) {
				lines = append(lines[:pos], lines[pos+1:]...)
			}
		default:
			if pos < len(lines) {
				lines[pos] = line
			}
		}
	}
	out := strings.Join(lines, "")
	if r.IntN(4) == 0 {
		out = strings.TrimSuffix(out, eol)
	}
	return out
}

func TestUnifiedApplies(t *testing.T) {
	ts := tools(t)
	r := rand.New(rand.NewPCG(7, 8))
	n := 150
	if testing.Short() {
		n = 20
	}
	for i := 0; i < n; i++ {
		eol := "\n"
		if i%5 == 4 {
			eol = "\r\n"
		}
		a := randomText(r, r.IntN(14), eol)
		var b string
		if i%3 == 0 {
			b = randomText(r, r.IntN(14), eol)
		} else {
			b = edit(r, a, eol)
		}
		checkApplies(t, ts, a, b, []int{3, 1, 0}[i%3])
	}
}

func TestUnifiedEdgeCases(t *testing.T) {
	ts := tools(t)
	for _, c := range [][2]string{
		{"", "a\n"},
		{"a\n", ""},
		{"a", "b"},
		{"a", "a\n"},
		{"a\n", "a"},
		{"x\na", "x\na\nb"},
		{"x\ny\nz", "x\nY\nz"},
		{"a\r\nb\r\n", "a\r\nc\r\n"},
		{"same\n", "same\n"},
	} {
		for _, ctx := range []int{3, 0} {
			checkApplies(t, ts, c[0], c[1], ctx)
		}
	}
}

func FuzzUnifiedPatch(f *testing.F) {
	f.Add("a\nb\nc\n", "a\nc\nd")
	f.Add("", "x")
	f.Add("x\n\n\ny", "\nx\ny\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		// patch splits lines at LF only and stops at NUL bytes; keep to what
		// it can represent.
		clean := func(s string) string {
			s = strings.ToValidUTF8(s, "?")
			return strings.NewReplacer("\r", "", "\x00", "").Replace(s)
		}
		a, b = clean(a), clean(b)
		if len(a) > 2000 || len(b) > 2000 || bytes.Count([]byte(a+b), []byte("\n")) > 200 {
			return
		}
		checkApplies(t, tools(t), a, b, 3)
	})
}
