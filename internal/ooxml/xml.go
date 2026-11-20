package ooxml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/wisebrian/gdco/internal/textenc"
)

// The token types callers switch on, re-exported so that they need not
// import encoding/xml for them.
type (
	StartElement = xml.StartElement
	EndElement   = xml.EndElement
	CharData     = xml.CharData
	Name         = xml.Name
)

// Decoder reads XML tokens and counts nesting, failing with ErrLimit past
// the depth limit: encoding/xml keeps state for every open element, so a
// file of a million nested elements would otherwise grow without bound.
type Decoder struct {
	d     *xml.Decoder
	depth int
	max   int
}

// NewDecoder reads b. Parts may be UTF-8 or, as the format allows, UTF-16
// with a byte order mark; old generators also declare Latin-1, which is
// read as Windows-1252.
func NewDecoder(b []byte, maxDepth int) *Decoder {
	switch {
	case bytes.HasPrefix(b, []byte("\xEF\xBB\xBF")):
		b = b[3:]
	case bytes.HasPrefix(b, []byte("\xFF\xFE")), bytes.HasPrefix(b, []byte("\xFE\xFF")):
		if s, _, err := textenc.Decode(b); err == nil {
			b = []byte(s)
		}
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	d.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "utf-16", "utf-16le", "utf-16be", "unicode", "ucs-2":
			return r, nil // already turned into UTF-8 above
		case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252", "us-ascii", "ascii":
			raw, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			return strings.NewReader(textenc.DecodeWindows1252(raw)), nil
		}
		return nil, fmt.Errorf("unsupported encoding %q", label)
	}
	return &Decoder{d: d, max: maxDepth}
}

// Token returns the next token, or io.EOF at the end.
func (d *Decoder) Token() (xml.Token, error) {
	t, err := d.d.Token()
	if err != nil {
		if err == io.EOF {
			if d.depth > 0 {
				return nil, fmt.Errorf("%w: unexpected end of XML", ErrMalformed)
			}
			return nil, io.EOF
		}
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	switch t.(type) {
	case xml.StartElement:
		d.depth++
		if d.depth > d.max {
			return nil, fmt.Errorf("%w: XML nested deeper than %d elements", ErrLimit, d.max)
		}
	case xml.EndElement:
		d.depth--
	}
	return t, nil
}

// Skip reads past the end of the element whose start was the last token.
func (d *Decoder) Skip() error {
	for depth := 1; depth > 0; {
		t, err := d.Token()
		if err == io.EOF {
			return fmt.Errorf("%w: unexpected end of XML", ErrMalformed)
		}
		if err != nil {
			return err
		}
		switch t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return nil
}

// Text returns the character data of the element whose start was the last
// token, reading past its end; elements nested inside it are skipped.
func (d *Decoder) Text() (string, error) {
	var b strings.Builder
	for depth := 1; depth > 0; {
		t, err := d.Token()
		if err == io.EOF {
			return "", fmt.Errorf("%w: unexpected end of XML", ErrMalformed)
		}
		if err != nil {
			return "", err
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 1 {
				b.Write(t)
			}
		}
	}
	return b.String(), nil
}

// NS identifies a namespace whatever variant of the format names it.
type NS uint8

// The namespaces this module reads. Strict OOXML (ISO/IEC 29500 Strict)
// uses other URIs for the same vocabularies, so each NS stands for both.
const (
	NSOther NS = iota
	NSW        // WordprocessingML
	NSS        // SpreadsheetML
	NSR        // relationships, for r:id
	NSMC       // markup compatibility, for mc:AlternateContent
	NSM        // Office Math
	NSXML      // the xml: prefix, for xml:space
)

// Space classifies a namespace URI.
func Space(uri string) NS {
	switch uri {
	case "http://schemas.openxmlformats.org/wordprocessingml/2006/main", "http://purl.oclc.org/ooxml/wordprocessingml/main":
		return NSW
	case "http://schemas.openxmlformats.org/spreadsheetml/2006/main", "http://purl.oclc.org/ooxml/spreadsheetml/main":
		return NSS
	case "http://schemas.openxmlformats.org/officeDocument/2006/relationships", "http://purl.oclc.org/ooxml/officeDocument/relationships":
		return NSR
	case "http://schemas.openxmlformats.org/markup-compatibility/2006":
		return NSMC
	case "http://schemas.openxmlformats.org/officeDocument/2006/math", "http://purl.oclc.org/ooxml/officeDocument/math":
		return NSM
	case "http://www.w3.org/XML/1998/namespace":
		return NSXML
	}
	return NSOther
}

// Attr returns the value of the attribute local in namespace ns, or ""; ns
// NSOther matches an attribute without a namespace.
func Attr(s StartElement, ns NS, local string) string {
	for _, a := range s.Attr {
		if a.Name.Local == local && (ns == NSOther && a.Name.Space == "" || ns != NSOther && Space(a.Name.Space) == ns) {
			return a.Value
		}
	}
	return ""
}

// Node is an element of a small part read whole, such as numbering.xml or
// styles.xml, where a tree is simpler than a token stream and the size
// limit on parts bounds the memory it takes.
type Node struct {
	Name Name
	Attr []xml.Attr
	Kids []*Node
}

// ParseTree reads a whole part into a tree of its elements; character data
// is left out.
func ParseTree(b []byte, maxDepth int) (*Node, error) {
	d := NewDecoder(b, maxDepth)
	root := &Node{}
	stack := []*Node{root}
	for {
		t, err := d.Token()
		if err == io.EOF {
			return root, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := t.(type) {
		case StartElement:
			n := &Node{Name: t.Name, Attr: t.Attr}
			top := stack[len(stack)-1]
			top.Kids = append(top.Kids, n)
			stack = append(stack, n)
		case EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

// Get returns the value of an attribute, as Attr does for a StartElement.
func (n *Node) Get(ns NS, local string) string {
	if n == nil {
		return ""
	}
	return Attr(StartElement{Name: n.Name, Attr: n.Attr}, ns, local)
}

// Child returns the first child element with the given name, or nil.
func (n *Node) Child(ns NS, local string) *Node {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name.Local == local && Space(k.Name.Space) == ns {
			return k
		}
	}
	return nil
}

// Children returns the child elements with the given name.
func (n *Node) Children(ns NS, local string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, k := range n.Kids {
		if k.Name.Local == local && Space(k.Name.Space) == ns {
			out = append(out, k)
		}
	}
	return out
}
