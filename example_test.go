package gdco_test

import (
	"fmt"
	"os"

	"github.com/wisebrian/gdco"
)

func ExampleCompareText() {
	old := "Payment is due within 30 days.\nLate fees apply.\n"
	new := "Payment is due within 45 days.\nLate fees apply.\nDisputes go to court.\n"

	r, err := gdco.CompareText(old, new, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%d changed, %d added\n", r.Stats.LinesChanged, r.Stats.LinesAdded)
	for _, row := range r.Parts[0].Hunks[0].Rows {
		if row.Op == gdco.OpModify {
			for _, s := range row.B.Spans {
				if s.Op == gdco.OpInsert {
					fmt.Printf("now reads %q\n", s.Text)
				}
			}
		}
	}
	r.WriteUnified(os.Stdout)
	// Output:
	// 1 changed, 1 added
	// now reads "45"
	// --- a
	// +++ b
	// @@ -1,2 +1,3 @@
	// -Payment is due within 30 days.
	// +Payment is due within 45 days.
	//  Late fees apply.
	// +Disputes go to court.
}

func ExampleSegmentDocument() {
	// Text extracted from two PDFs, page by page, by another tool.
	a := gdco.SegmentDocument("v1.pdf", []gdco.Segment{{Text: "Term: 12 months", Page: 1}, {Text: "Price: 100 EUR", Page: 2}})
	b := gdco.SegmentDocument("v2.pdf", []gdco.Segment{{Text: "Term: 12 months", Page: 1}, {Text: "Price: 120 EUR", Page: 2}})
	r, err := gdco.Compare(a, b, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, row := range r.Parts[0].Hunks[0].Rows {
		if row.Op != gdco.OpEqual {
			fmt.Printf("%s: %q -> %q\n", row.B.Loc, row.A.Text, row.B.Text)
		}
	}
	// Output:
	// Page 2, line 1: "Price: 100 EUR" -> "Price: 120 EUR"
}
