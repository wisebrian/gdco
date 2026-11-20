package ooxml

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

var small = Limits{Entries: 50, Part: 1 << 20, Total: 2 << 20, Ratio: 100, Depth: 64}

// deflated returns n zero bytes compressed, the classic bomb payload: about
// a thousand to one.
func deflated(n int) []byte {
	var b bytes.Buffer
	w, _ := flate.NewWriter(&b, flate.BestCompression)
	chunk := make([]byte, 64<<10)
	for n > 0 {
		k := min(n, len(chunk))
		w.Write(chunk[:k])
		n -= k
	}
	w.Close()
	return b.Bytes()
}

// rawZip writes one deflated entry with the sizes given, true or not.
func rawZip(name string, payload []byte, declared uint64) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: name, Method: zip.Deflate,
		CompressedSize64: uint64(len(payload)), UncompressedSize64: declared,
	})
	if err != nil {
		panic(err)
	}
	w.Write(payload)
	zw.Close()
	return b.Bytes()
}

func TestBombs(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
	}{
		// An honest header: rejected before anything is decompressed.
		{"declared too big", rawZip("word/document.xml", deflated(8<<20), 8<<20), ErrLimit},
		// A header that lies about the size: decompression stops at the
		// limit, archive/zip refuses data longer than declared.
		{"lying header", rawZip("word/document.xml", deflated(8<<20), 1000), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Open(tt.data, small)
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Read("word/document.xml")
			if err == nil {
				t.Fatal("a bomb was read")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err %v, want %v", err, tt.want)
			}
		})
	}
	// The ratio limit, on a part over 1 MiB that fits the size limits.
	lim := small
	lim.Part, lim.Total = 4<<20, 4<<20
	payload := deflated(2 << 20)
	p, err := Open(rawZip("x.xml", payload, 2<<20), lim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read("x.xml"); !errors.Is(err, ErrLimit) || !strings.Contains(err.Error(), "compressed") {
		t.Fatalf("ratio: %v", err)
	}
	// The total limit across parts.
	lim = Limits{Entries: 10, Part: 600 << 10, Total: 1 << 20, Ratio: 10000, Depth: 64}
	text := strings.Repeat("x", 500<<10)
	p, err = Open(ooxmltest.Zip(ooxmltest.Part{Name: "a.xml", Data: text}, ooxmltest.Part{Name: "b.xml", Data: text}, ooxmltest.Part{Name: "c.xml", Data: text}), lim)
	if err != nil {
		t.Fatal(err)
	}
	_, e1 := p.Read("a.xml")
	_, e2 := p.Read("b.xml")
	_, e3 := p.Read("c.xml")
	if e1 != nil || e2 != nil || !errors.Is(e3, ErrLimit) {
		t.Fatalf("total limit: %v %v %v", e1, e2, e3)
	}
}

func TestArchiveShape(t *testing.T) {
	var parts []ooxmltest.Part
	for i := range 60 {
		parts = append(parts, ooxmltest.Part{Name: "p" + string(rune('a'+i%26)) + strings.Repeat("x", i/26) + ".xml", Data: "<a/>"})
	}
	if _, err := Open(ooxmltest.Zip(parts...), small); !errors.Is(err, ErrLimit) {
		t.Errorf("too many entries: %v", err)
	}
	if _, err := Open(ooxmltest.Zip(ooxmltest.Part{Name: "Word/Document.xml", Data: "<a/>"}, ooxmltest.Part{Name: "word/document.xml", Data: "<b/>"}), small); !errors.Is(err, ErrMalformed) {
		t.Errorf("duplicate part: %v", err)
	}
	if _, err := Open([]byte("PK\x03\x04garbage"), small); !errors.Is(err, ErrMalformed) {
		t.Errorf("garbage: %v", err)
	}
	p, err := Open(ooxmltest.Zip(ooxmltest.Part{Name: "Word/Document.XML", Data: "<a/>"}), small)
	if err != nil || !p.Has("word/document.xml") {
		t.Errorf("part names must ignore case: %v", err)
	}
	if _, err := p.Read("missing.xml"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing part: %v", err)
	}
}

func TestEncryptedEntry(t *testing.T) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "a.xml", Method: zip.Store, Flags: 0x1})
	w.Write([]byte("<a/>"))
	zw.Close()
	p, err := Open(b.Bytes(), small)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read("a.xml"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("encrypted: %v", err)
	}
}

func tokens(t *testing.T, xml string, depth int) error {
	t.Helper()
	d := NewDecoder([]byte(xml), depth)
	for {
		_, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func TestXMLHostile(t *testing.T) {
	deep := strings.Repeat("<a>", 100000) + strings.Repeat("</a>", 100000)
	if err := tokens(t, deep, 256); !errors.Is(err, ErrLimit) {
		t.Errorf("deep nesting: %v", err)
	}
	for name, xml := range map[string]string{
		"truncated":        "<a><b>",
		"mismatched":       "<a></b>",
		"undefined entity": "<a>&nope;</a>",
		"entity from DTD":  `<!DOCTYPE a [<!ENTITY x "boom">]><a>&x;</a>`,
		"external entity":  `<!DOCTYPE a [<!ENTITY x SYSTEM "file:///etc/passwd">]><a>&x;</a>`,
		"bad UTF-8":        "<a>\xff</a>",
		"bad encoding":     `<?xml version="1.0" encoding="EBCDIC"?><a/>`,
	} {
		if err := tokens(t, xml, 64); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A DTD alone is not expanded and not an error: the entity is never used.
	if err := tokens(t, `<!DOCTYPE a [<!ENTITY x "boom">]><a>ok</a>`, 64); err != nil {
		t.Errorf("unused DTD: %v", err)
	}
	if err := tokens(t, `<?xml version="1.0" encoding="ISO-8859-1"?><a>caf`+"\xe9"+`</a>`, 64); err != nil {
		t.Errorf("Latin-1: %v", err)
	}
}

func TestResolve(t *testing.T) {
	for _, c := range [][3]string{
		{"word/", "header1.xml", "word/header1.xml"},
		{"word/", "../customXml/item1.xml", "customXml/item1.xml"},
		{"", "word/document.xml", "word/document.xml"},
		{"word/", "/word/media/a.png", "word/media/a.png"},
		{"xl/", "worksheets/my%20sheet.xml", "xl/worksheets/my sheet.xml"},
		{"word/", "..\\..\\..\\x.xml", "x.xml"},
	} {
		if got := Resolve(c[0], c[1]); got != c[2] {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(ooxmltest.Docx(`<w:p><w:r><w:t>x</w:t></w:r></w:p>`, nil))
	f.Add(rawZip("a.xml", deflated(1<<16), 10))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Open(data, small)
		if err != nil {
			return
		}
		for name := range p.files {
			b, err := p.Read(name)
			if err == nil && int64(len(b)) > small.Part {
				t.Fatalf("read %d bytes past the part limit", len(b))
			}
		}
		if p.read > small.Total {
			t.Fatalf("read %d bytes past the total limit", p.read)
		}
		p.Rels("word/document.xml")
		p.MainPart()
	})
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte(`<a x="1"><b>text</b><!-- c --></a>`))
	f.Add([]byte(`<!DOCTYPE a [<!ENTITY x "y">]><a>&x;</a>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewDecoder(data, 32)
		depth := 0
		for range 100000 {
			tok, err := d.Token()
			if err != nil {
				if err != io.EOF && !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrLimit) {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			switch tok.(type) {
			case StartElement:
				depth++
				if depth > 32 {
					t.Fatal("depth limit not enforced")
				}
			case EndElement:
				depth--
			}
		}
	})
}
