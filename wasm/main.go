//go:build js && wasm

// Command wasm exposes gdco to JavaScript as one global object, gdco, with
// three functions:
//
//	gdco.compareText(a, b, options)                          // strings
//	gdco.compareFiles(bytesA, nameA, bytesB, nameB, options) // Uint8Array
//	gdco.compareSegments(docA, docB, options)                // {name, segments: [{text, page, label}]}
//
// Each returns a string: the result as JSON for JSON.parse by default, or
// the unified diff or the HTML report when options.output is "unified" or
// "html". A failure returns {error, code} instead of throwing, with code
// one of "binary", "unsupported", "malformed", "limit", "mismatch",
// "argument" and "internal", for the page to translate.
//
// The functions are synchronous and meant to be called from a Web Worker.
// They recover from panics, because a panic would end the Go program and
// every later call with it.
package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"syscall/js"

	"github.com/wisebrian/gdco"
)

func main() {
	api := js.Global().Get("Object").New()
	api.Set("compareText", js.FuncOf(compareText))
	api.Set("compareFiles", js.FuncOf(compareFiles))
	api.Set("compareSegments", js.FuncOf(compareSegments))
	js.Global().Set("gdco", api)
	select {}
}

// argError is a call from JavaScript with arguments of the wrong shape.
type argError string

func (e argError) Error() string { return string(e) }

func compareText(_ js.Value, args []js.Value) (ret any) {
	defer recoverTo(&ret)
	if len(args) < 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeString {
		return fail(argError("compareText(a, b, options): a and b must be strings"))
	}
	o, out, err := parseOptions(arg(args, 2))
	if err != nil {
		return fail(err)
	}
	a := gdco.TextDocument(out.nameA, args[0].String())
	b := gdco.TextDocument(out.nameB, args[1].String())
	r, err := gdco.Compare(a, b, o)
	return render(r, err, out)
}

func compareFiles(_ js.Value, args []js.Value) (ret any) {
	defer recoverTo(&ret)
	if len(args) < 4 || args[1].Type() != js.TypeString || args[3].Type() != js.TypeString {
		return fail(argError("compareFiles(bytesA, nameA, bytesB, nameB, options): names must be strings"))
	}
	o, out, err := parseOptions(arg(args, 4))
	if err != nil {
		return fail(err)
	}
	a, err := bytesOf(args[0], o)
	if err != nil {
		return fail(err)
	}
	b, err := bytesOf(args[2], o)
	if err != nil {
		return fail(err)
	}
	r, err := gdco.CompareFiles(gdco.File{Name: args[1].String(), Data: a}, gdco.File{Name: args[3].String(), Data: b}, o)
	return render(r, err, out)
}

func compareSegments(_ js.Value, args []js.Value) (ret any) {
	defer recoverTo(&ret)
	if len(args) < 2 {
		return fail(argError("compareSegments(docA, docB, options): two documents are required"))
	}
	o, out, err := parseOptions(arg(args, 2))
	if err != nil {
		return fail(err)
	}
	a, err := segmentsOf(args[0], "docA")
	if err != nil {
		return fail(err)
	}
	b, err := segmentsOf(args[1], "docB")
	if err != nil {
		return fail(err)
	}
	r, err := gdco.Compare(a, b, o)
	return render(r, err, out)
}

func arg(args []js.Value, i int) js.Value {
	if i < len(args) {
		return args[i]
	}
	return js.Undefined()
}

// bytesOf copies a Uint8Array into Go memory, after checking its length
// against the limit: copying first would let one huge array exhaust the
// memory the limit is there to protect.
func bytesOf(v js.Value, o *gdco.Options) ([]byte, error) {
	if v.Type() != js.TypeObject || !v.InstanceOf(js.Global().Get("Uint8Array")) {
		return nil, argError("file contents must be a Uint8Array")
	}
	n := v.Get("length").Int()
	if limit := o.Limits.MaxFileSize; int64(n) > limit {
		return nil, fmt.Errorf("%w: a file of %d bytes is more than %d", gdco.ErrLimit, n, limit)
	}
	b := make([]byte, n)
	js.CopyBytesToGo(b, v)
	return b, nil
}

func segmentsOf(v js.Value, what string) (*gdco.Document, error) {
	if v.Type() != js.TypeObject {
		return nil, argError(what + " must be an object {name, segments}")
	}
	name, err := optString(v, "name", what+".name")
	if err != nil {
		return nil, err
	}
	list := v.Get("segments")
	if list.Type() != js.TypeObject || !js.Global().Get("Array").Call("isArray", list).Bool() {
		return nil, argError(what + ".segments must be an array")
	}
	segs := make([]gdco.Segment, list.Length())
	for i := range segs {
		s := list.Index(i)
		where := fmt.Sprintf("%s.segments[%d]", what, i)
		if s.Type() != js.TypeObject {
			return nil, argError(where + " must be an object {text, page, label}")
		}
		if segs[i].Text, err = optString(s, "text", where+".text"); err != nil {
			return nil, err
		}
		if segs[i].Label, err = optString(s, "label", where+".label"); err != nil {
			return nil, err
		}
		if p := s.Get("page"); !p.IsUndefined() && !p.IsNull() {
			if p.Type() != js.TypeNumber {
				return nil, argError(where + ".page must be a number")
			}
			segs[i].Page = p.Int()
		}
	}
	return gdco.SegmentDocument(name, segs), nil
}

// outOptions are the options that shape the answer rather than the
// comparison.
type outOptions struct {
	output       string
	nameA, nameB string
	report       gdco.ReportOptions
}

func parseOptions(v js.Value) (*gdco.Options, outOptions, error) {
	o := gdco.DefaultOptions()
	out := outOptions{output: "json", nameA: "a", nameB: "b"}
	if v.IsUndefined() || v.IsNull() {
		return o, out, nil
	}
	if v.Type() != js.TypeObject {
		return nil, out, argError("options must be an object")
	}
	errs := []error{
		optBool(v, "ignoreCase", &o.IgnoreCase),
		optBool(v, "ignoreSpaceChange", &o.IgnoreSpaceChange),
		optBool(v, "ignoreAllSpace", &o.IgnoreAllSpace),
		optBool(v, "ignoreTrailingSpace", &o.IgnoreTrailingSpace),
		optBool(v, "ignoreBlankLines", &o.IgnoreBlankLines),
		optBool(v, "headers", &o.Headers),
		optBool(v, "footers", &o.Footers),
		optBool(v, "footnotes", &o.Footnotes),
		optBool(v, "endnotes", &o.Endnotes),
		optBool(v, "comments", &o.Comments),
		optBool(v, "formulas", &o.Formulas),
		optInt(v, "context", &o.Context),
		optEnum(v, "granularity", (*string)(&o.Granularity), "word", "char", "line"),
		optEnum(v, "tracked", (*string)(&o.Tracked), "accepted", "original"),
		optEnum(v, "output", &out.output, "json", "unified", "html"),
		optField(v, "nameA", &out.nameA),
		optField(v, "nameB", &out.nameB),
	}
	if r := v.Get("report"); !r.IsUndefined() && !r.IsNull() {
		if r.Type() != js.TypeObject {
			return nil, out, argError("options.report must be an object")
		}
		errs = append(errs,
			optField(r, "title", &out.report.Title),
			optField(r, "generated", &out.report.Generated),
			optEnum(r, "layout", (*string)(&out.report.Layout), "side-by-side", "inline"),
		)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, out, err
	}
	return o, out, nil
}

func optBool(v js.Value, key string, dst *bool) error {
	x := v.Get(key)
	switch x.Type() {
	case js.TypeUndefined, js.TypeNull:
		return nil
	case js.TypeBoolean:
		*dst = x.Bool()
		return nil
	}
	return argError("options." + key + " must be a boolean")
}

func optInt(v js.Value, key string, dst *int) error {
	x := v.Get(key)
	switch x.Type() {
	case js.TypeUndefined, js.TypeNull:
		return nil
	case js.TypeNumber:
		f := x.Float()
		if f != float64(int(f)) {
			return argError("options." + key + " must be an integer")
		}
		*dst = int(f)
		return nil
	}
	return argError("options." + key + " must be a number")
}

func optField(v js.Value, key string, dst *string) error {
	s, err := optString(v, key, "options."+key)
	if err == nil && !v.Get(key).IsUndefined() && !v.Get(key).IsNull() {
		*dst = s
	}
	return err
}

func optString(v js.Value, key, where string) (string, error) {
	x := v.Get(key)
	switch x.Type() {
	case js.TypeUndefined, js.TypeNull:
		return "", nil
	case js.TypeString:
		return x.String(), nil
	}
	return "", argError(where + " must be a string")
}

func optEnum(v js.Value, key string, dst *string, allowed ...string) error {
	var s string
	if err := optField(v, key, &s); err != nil || s == "" {
		return err
	}
	if slices.Contains(allowed, s) {
		*dst = s
		return nil
	}
	return argError(fmt.Sprintf("options.%s must be one of %s", key, strings.Join(allowed, ", ")))
}

func render(r *gdco.Result, err error, out outOptions) any {
	if err != nil {
		return fail(err)
	}
	var sb strings.Builder
	switch out.output {
	case "unified":
		err = r.WriteUnified(&sb)
	case "html":
		err = r.WriteHTML(&sb, &out.report)
	default:
		var b []byte
		b, err = r.MarshalJSON()
		sb.Write(b)
	}
	if err != nil {
		return fail(err)
	}
	return sb.String()
}

func fail(err error) any {
	return map[string]any{"error": err.Error(), "code": code(err)}
}

func code(err error) string {
	var ae argError
	switch {
	case errors.As(err, &ae), errors.Is(err, gdco.ErrOptions):
		return "argument"
	case errors.Is(err, gdco.ErrBinary):
		return "binary"
	case errors.Is(err, gdco.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, gdco.ErrMalformed):
		return "malformed"
	case errors.Is(err, gdco.ErrLimit):
		return "limit"
	case errors.Is(err, gdco.ErrMismatch):
		return "mismatch"
	}
	return "internal"
}

func recoverTo(ret *any) {
	if p := recover(); p != nil {
		*ret = map[string]any{"error": fmt.Sprintf("gdco: internal error: %v", p), "code": "internal"}
	}
}
