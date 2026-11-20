// Package ooxml opens Office Open XML packages, the zip archives behind
// .docx and .xlsx files, as hostile input: every archive is assumed to be a
// zip bomb or a malformed file until reading it has proved otherwise.
//
// Only the parts a comparison needs are ever decompressed, never images or
// embedded objects, and each read is bounded by the size of the part, the
// total read from the archive and the compression ratio. XML is read with
// encoding/xml's streaming Decoder, which never resolves external entities
// or DTDs; Decoder adds a bound on nesting depth.
package ooxml

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// The errors callers map to their own. They wrap a description of what
// went wrong.
var (
	ErrLimit       = errors.New("safety limit exceeded")
	ErrMalformed   = errors.New("malformed package")
	ErrUnsupported = errors.New("unsupported package")
	ErrNotFound    = errors.New("part not found")
)

// Limits bound what reading a package may cost.
type Limits struct {
	Entries int   // entries in the archive
	Part    int64 // decompressed bytes of one part
	Total   int64 // decompressed bytes of all the parts read
	Ratio   int   // decompressed to compressed size, for parts over 1 MiB
	Depth   int   // XML nesting depth
}

// Package is an opened archive.
type Package struct {
	files map[string]*zip.File // by lower-case name: OPC part names ignore case
	lim   Limits
	read  int64
	types contentTypes
}

// Open reads the directory of an archive and its content types. It
// decompresses nothing else.
func Open(data []byte, lim Limits) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(zr.File) > lim.Entries {
		return nil, fmt.Errorf("%w: %d entries in the archive, more than %d", ErrLimit, len(zr.File), lim.Entries)
	}
	p := &Package{files: make(map[string]*zip.File, len(zr.File)), lim: lim}
	for _, f := range zr.File {
		name := key(f.Name)
		if _, dup := p.files[name]; dup {
			// Two entries for one part: which one an application reads is
			// undefined, so the file cannot be read reliably.
			return nil, fmt.Errorf("%w: the part %q appears twice", ErrMalformed, f.Name)
		}
		p.files[name] = f
	}
	if p.Has("[Content_Types].xml") {
		b, err := p.Read("[Content_Types].xml")
		if err != nil {
			return nil, err
		}
		if p.types, err = parseContentTypes(b, lim.Depth); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func key(name string) string { return strings.ToLower(strings.TrimPrefix(name, "/")) }

// Has reports whether the archive holds a part.
func (p *Package) Has(name string) bool { return p.files[key(name)] != nil }

// Read decompresses one part within the limits. The sizes the archive
// declares are checked first, so that an absurd claim fails at once, and
// then the bytes actually produced, since a declared size can lie.
func (p *Package) Read(name string) ([]byte, error) {
	f := p.files[key(name)]
	if f == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if f.Flags&0x1 != 0 {
		return nil, fmt.Errorf("%w: the part %s is encrypted", ErrUnsupported, name)
	}
	budget := min(p.lim.Part, p.lim.Total-p.read)
	if f.UncompressedSize64 > uint64(max(budget, 0)) {
		return nil, fmt.Errorf("%w: the part %s would decompress to %d bytes", ErrLimit, name, f.UncompressedSize64)
	}
	if err := p.checkRatio(name, f.UncompressedSize64, f.CompressedSize64); err != nil {
		return nil, err
	}
	rc, err := f.Open()
	if err != nil {
		if errors.Is(err, zip.ErrAlgorithm) {
			return nil, fmt.Errorf("%w: the part %s uses an unknown compression method", ErrUnsupported, name)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrMalformed, name, err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	// Grow by what the archive claims only up to a point: the claim may be
	// a lie meant to make the reader allocate memory it will never fill.
	buf.Grow(int(min(f.UncompressedSize64, 1<<20)))
	n, err := buf.ReadFrom(io.LimitReader(rc, budget+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrMalformed, name, err)
	}
	if n > budget {
		return nil, fmt.Errorf("%w: the part %s decompresses to more than %d bytes", ErrLimit, name, budget)
	}
	if err := p.checkRatio(name, uint64(n), f.CompressedSize64); err != nil {
		return nil, err
	}
	p.read += n
	return buf.Bytes(), nil
}

func (p *Package) checkRatio(name string, size, compressed uint64) error {
	if size > 1<<20 && size/max(compressed, 1) > uint64(p.lim.Ratio) {
		return fmt.Errorf("%w: the part %s is compressed %d to 1, more than %d to 1", ErrLimit, name, size/max(compressed, 1), p.lim.Ratio)
	}
	return nil
}

// ContentType returns the content type the package declares for a part.
func (p *Package) ContentType(name string) string { return p.types.of(name) }

// Rel is one relationship of a part. Type is the last segment of the
// relationship type URI, which is the same in the Transitional and Strict
// variants of the format ("officeDocument", "header", "worksheet").
type Rel struct {
	ID       string
	Type     string
	Target   string // the part it points to, resolved to a package path
	External bool   // a URL or a file outside the package; never read
}

// Rels returns the relationships of a part, or of the package itself when
// source is empty. A part without relationships has none, not an error.
func (p *Package) Rels(source string) ([]Rel, error) {
	dir, file := path.Split(strings.TrimPrefix(source, "/"))
	name := dir + "_rels/" + file + ".rels"
	if !p.Has(name) {
		return nil, nil
	}
	b, err := p.Read(name)
	if err != nil {
		return nil, err
	}
	d := NewDecoder(b, p.lim.Depth)
	var rels []Rel
	for {
		t, err := d.Token()
		if err == io.EOF {
			return rels, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		s, ok := t.(StartElement)
		if !ok || s.Name.Local != "Relationship" {
			continue
		}
		r := Rel{ID: Attr(s, NSOther, "Id"), Type: path.Base(Attr(s, NSOther, "Type")), External: Attr(s, NSOther, "TargetMode") == "External"}
		if !r.External {
			r.Target = Resolve(dir, Attr(s, NSOther, "Target"))
		}
		rels = append(rels, r)
	}
}

// Resolve turns a relationship target into a package path: relative to the
// source part's directory, or absolute when it starts with "/".
func Resolve(dir, target string) string {
	target = unescape(strings.ReplaceAll(target, "\\", "/"))
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)[1:]
	}
	return strings.TrimPrefix(path.Clean("/"+dir+target), "/")
}

// MainPart returns the package's main part, the target of its
// officeDocument relationship, and its content type.
func (p *Package) MainPart() (name, contentType string, err error) {
	rels, err := p.Rels("")
	if err != nil {
		return "", "", err
	}
	for _, r := range rels {
		if r.Type == "officeDocument" && !r.External && p.Has(r.Target) {
			return r.Target, p.ContentType(r.Target), nil
		}
	}
	return "", "", fmt.Errorf("%w: no main document part", ErrNotFound)
}

type contentTypes struct {
	defaults  map[string]string // by lower-case extension
	overrides map[string]string // by lower-case part name
}

func (c contentTypes) of(name string) string {
	if t, ok := c.overrides[key(name)]; ok {
		return t
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	return c.defaults[ext]
}

func parseContentTypes(b []byte, depth int) (contentTypes, error) {
	c := contentTypes{defaults: map[string]string{}, overrides: map[string]string{}}
	d := NewDecoder(b, depth)
	for {
		t, err := d.Token()
		if err == io.EOF {
			return c, nil
		}
		if err != nil {
			return c, fmt.Errorf("[Content_Types].xml: %w", err)
		}
		s, ok := t.(StartElement)
		if !ok {
			continue
		}
		switch s.Name.Local {
		case "Default":
			c.defaults[strings.ToLower(Attr(s, NSOther, "Extension"))] = Attr(s, NSOther, "ContentType")
		case "Override":
			c.overrides[key(Attr(s, NSOther, "PartName"))] = Attr(s, NSOther, "ContentType")
		}
	}
}

// unescape decodes the %XX escapes of a part name, which is a URI: a part
// called "my file.xml" is the target "my%20file.xml".
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	hex := func(c byte) (byte, bool) {
		switch {
		case '0' <= c && c <= '9':
			return c - '0', true
		case 'a' <= c && c <= 'f':
			return c - 'a' + 10, true
		case 'A' <= c && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			h, ok1 := hex(s[i+1])
			l, ok2 := hex(s[i+2])
			if ok1 && ok2 {
				b = append(b, h<<4|l)
				i += 2
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}
