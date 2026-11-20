package xlsx

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wisebrian/gdco/internal/ooxml"
	"github.com/wisebrian/gdco/internal/ooxml/ooxmltest"
)

// BenchmarkLargeSheet reads a sheet of 50 000 rows and 10 columns, half
// numbers and half shared strings, half a million cells.
func BenchmarkLargeSheet(b *testing.B) {
	var s strings.Builder
	for r := 1; r <= 50_000; r++ {
		fmt.Fprintf(&s, `<row r="%d">`, r)
		for c := 1; c <= 10; c++ {
			if c%2 == 0 {
				fmt.Fprintf(&s, `<c r="%s" t="s"><v>%d</v></c>`, Address(c, r), r%3)
			} else {
				fmt.Fprintf(&s, `<c r="%s"><v>%d.5</v></c>`, Address(c, r), r*c)
			}
		}
		s.WriteString(`</row>`)
	}
	data := ooxmltest.Xlsx([]string{"alpha", "beta", "gamma"}, s.String())
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		pkg, err := ooxml.Open(data, ooxml.Limits{Entries: 100, Part: 128 << 20, Total: 256 << 20, Ratio: 1000, Depth: 64})
		if err != nil {
			b.Fatal(err)
		}
		wb, err := Read(pkg, "xl/workbook.xml", Options{Depth: 64, MaxCells: 5_000_000})
		if err != nil || len(wb.Sheets[0].Rows) != 50_000 {
			b.Fatal(err)
		}
	}
}
