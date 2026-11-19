package gdco

import (
	"fmt"

	"github.com/wisebrian/gdco/internal/align"
	"github.com/wisebrian/gdco/internal/tokens"
)

// Options configure reading and comparing together, so that the one options
// object a UI sends can be passed to every function. A nil *Options means
// DefaultOptions(); a non-nil value is used as it is, so start from
// DefaultOptions() to change a single field.
type Options struct {
	// IgnoreCase compares with Unicode simple case folding, the rule of
	// strings.EqualFold. "ß" and "SS" stay different: full folding, like
	// NFC/NFD normalization, needs tables the standard library does not
	// have.
	IgnoreCase bool

	// The white-space options mean what diff's -b, -w, -Z and -B mean, so
	// results can be checked against diff and git. IgnoreSpaceChange
	// treats any run of white space as one space and ignores it at the end
	// of a line. IgnoreBlankLines ignores changes that only add or remove
	// blank lines; their rows stay in the result, marked Ignored, so that
	// a side-by-side view still lines up.
	IgnoreSpaceChange   bool
	IgnoreAllSpace      bool
	IgnoreTrailingSpace bool
	IgnoreBlankLines    bool

	// Granularity is how finely modified lines are highlighted.
	Granularity Granularity

	// Context is the number of unchanged lines kept around each change, in
	// Hunks and in the unified diff. Negative keeps the whole document in
	// one hunk, for a view that shows everything.
	Context int

	// Tracked chooses which version of a Word document with tracked
	// changes is compared.
	Tracked Tracked

	// The optional parts of a Word document. Each becomes its own PartDiff,
	// so a UI can fold it away instead of mixing it with the body.
	Headers, Footers, Footnotes, Endnotes, Comments bool

	// Formulas compares the formulas of Excel cells instead of their
	// cached values: reviewing a model rather than its numbers.
	Formulas bool

	// Limits bound what hostile input can cost.
	Limits Limits
}

// DefaultOptions highlights words, keeps 3 lines of context as diff -u
// does, compares Word documents with their tracked changes accepted,
// includes every Word part and spells out the default limits.
func DefaultOptions() *Options {
	return &Options{
		Granularity: ByWord,
		Context:     3,
		Tracked:     TrackedAccepted,
		Headers:     true,
		Footers:     true,
		Footnotes:   true,
		Endnotes:    true,
		Comments:    true,
		Limits:      defaultLimits,
	}
}

// Granularity sets how finely a modified line is highlighted.
type Granularity string

// The granularities. ByLine does not pair lines at all: changed lines are
// shown as deleted and inserted, as a classic diff shows them.
const (
	ByWord Granularity = "word" // words, white-space runs and single punctuation marks
	ByChar Granularity = "char" // for codes, amounts and identifiers, where one character matters
	ByLine Granularity = "line" // no highlighting inside lines; the cheapest
)

// Tracked selects a view of a Word document that contains tracked changes.
type Tracked string

// The views.
const (
	TrackedAccepted Tracked = "accepted" // what the document says once every change is accepted
	TrackedOriginal Tracked = "original" // the text before the tracked edits
)

// Limits bound memory and time on hostile input: a zip bomb or a file of
// 100 000 nested XML elements must end in ErrLimit, not in a dead browser
// tab, because a Go program cannot recover from running out of memory. Zero
// fields take the defaults below, which keep a comparison under about a
// gigabyte of memory.
type Limits struct {
	MaxFileSize   int64 // bytes of one input: 64 MiB
	MaxLines      int   // lines, or rows, of one document: 1 000 000
	MaxZipEntries int   // entries in one zip archive: 10 000
	MaxPartSize   int64 // decompressed bytes of one zip entry read: 128 MiB
	MaxTotalSize  int64 // decompressed bytes read from one file: 256 MiB
	MaxRatio      int   // compression ratio of an entry over 1 MiB: 200
	MaxXMLDepth   int   // nesting depth of XML elements: 256
	MaxCells      int   // cells of one workbook: 5 000 000
}

// defaultLimits are the values zero fields of Limits stand for.
var defaultLimits = Limits{
	MaxFileSize:   64 << 20,
	MaxLines:      1_000_000,
	MaxZipEntries: 10_000,
	MaxPartSize:   128 << 20,
	MaxTotalSize:  256 << 20,
	MaxRatio:      200,
	MaxXMLDepth:   256,
	MaxCells:      5_000_000,
}

func (l Limits) resolve() (Limits, error) {
	d := defaultLimits
	def := func(v *int64, d int64) error {
		if *v < 0 {
			return fmt.Errorf("%w: negative limit %d", ErrOptions, *v)
		}
		if *v == 0 {
			*v = d
		}
		return nil
	}
	defi := func(v *int, d int) error {
		w := int64(*v)
		err := def(&w, int64(d))
		*v = int(w)
		return err
	}
	for _, err := range []error{
		def(&l.MaxFileSize, d.MaxFileSize),
		defi(&l.MaxLines, d.MaxLines),
		defi(&l.MaxZipEntries, d.MaxZipEntries),
		def(&l.MaxPartSize, d.MaxPartSize),
		def(&l.MaxTotalSize, d.MaxTotalSize),
		defi(&l.MaxRatio, d.MaxRatio),
		defi(&l.MaxXMLDepth, d.MaxXMLDepth),
		defi(&l.MaxCells, d.MaxCells),
	} {
		if err != nil {
			return l, err
		}
	}
	return l, nil
}

// resolve returns a copy of the options with defaults filled in, or
// ErrOptions for a value no default can stand for.
func (o *Options) resolve() (Options, error) {
	if o == nil {
		o = DefaultOptions()
	}
	r := *o
	switch r.Granularity {
	case ByWord, ByChar, ByLine:
	case "":
		r.Granularity = ByWord
	default:
		return r, fmt.Errorf("%w: unknown granularity %q", ErrOptions, r.Granularity)
	}
	switch r.Tracked {
	case TrackedAccepted, TrackedOriginal:
	case "":
		r.Tracked = TrackedAccepted
	default:
		return r, fmt.Errorf("%w: unknown tracked-changes view %q", ErrOptions, r.Tracked)
	}
	var err error
	r.Limits, err = r.Limits.resolve()
	return r, err
}

func (o *Options) norm() tokens.Norm {
	return tokens.Norm{
		Case:          o.IgnoreCase,
		SpaceChange:   o.IgnoreSpaceChange,
		AllSpace:      o.IgnoreAllSpace,
		TrailingSpace: o.IgnoreTrailingSpace,
	}
}

func (o *Options) alignConfig() align.Config {
	c := align.Config{Mode: tokens.Words, Norm: o.norm()}
	if o.Granularity == ByChar {
		c.Mode = tokens.Chars
	}
	return c
}
