package gdco

import (
	"encoding/json"
	"reflect"
	"testing"
)

// plain has Result's fields but not its MarshalJSON method, so
// encoding/json encodes it by reflection from the struct tags: the
// reference the hand-written encoder must agree with.
type plain Result

// checkJSON requires the two encoders to produce the same JSON value, and
// the hand-written one to produce valid JSON.
func checkJSON(t *testing.T, r *Result) {
	t.Helper()
	ours, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := json.Marshal((*plain)(r))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(ours, &a); err != nil {
		t.Fatalf("invalid JSON %s: %v", ours, err)
	}
	if err := json.Unmarshal(ref, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("hand-written JSON\n%s\ndiffers from encoding/json\n%s", ours, ref)
	}
}

func TestMarshalJSON(t *testing.T) {
	r, err := Compare(
		SegmentDocument("a\"<b>\\", []Segment{{Text: "x\ttab\nsame\nctl\x01 ", Page: 2, Label: "ii"}}),
		SegmentDocument("b", []Segment{{Text: "y tab\nsame\nnew line", Page: 2}}),
		opts(func(o *Options) { o.Context = -1 }))
	if err != nil {
		t.Fatal(err)
	}
	r.Parts[0].Cells = []CellChange{{A: "B7", B: "B8", Old: "1", New: "2"}, {B: "C1", Old: "", New: "x"}}
	r.Notes = append(r.Notes, Note{Code: "x", Side: "a", Message: "m"})
	checkJSON(t, r)
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
	empty := &Result{}
	checkJSON(t, empty)
}

func FuzzMarshalJSON(f *testing.F) {
	f.Add("a\nb\n", "a\nc", uint16(0))
	f.Add("\x00\x7f\"\\", "é ", uint16(3))
	f.Fuzz(func(t *testing.T, a, b string, bits uint16) {
		o := optionsFrom(bits)
		r, err := Compare(TextDocument(a, a), TextDocument(b, b), o)
		if err != nil {
			t.Fatal(err)
		}
		checkJSON(t, r)
	})
}
