// Package gdco compares two texts, Word documents or Excel workbooks and
// reports what changed. It uses only the standard library and never touches
// files, the network or the clock, so it runs as WebAssembly in the
// visitor's browser: the contracts and spreadsheets people compare never
// leave their machine.
//
// A comparison has three steps. Read, TextDocument and SegmentDocument
// turn the inputs into Documents; Compare aligns them; and the Result can
// be rendered by the caller or written as a unified diff (WriteUnified) or
// a self-contained HTML report (WriteHTML). CompareText and CompareFiles do
// the first two steps at once.
//
//	r, err := gdco.CompareText(old, new, nil)
//	if err != nil {
//		return err
//	}
//	err = r.WriteUnified(os.Stdout)
//
// # What counts as a change
//
// Lines are aligned first, with Myers' algorithm, and lines that replaced
// each other are then compared word by word, so a modified line shows the
// words that changed rather than two unrelated lines.
//
// Line endings are not compared: a file with CRLF and the same file with LF
// are equal, and Result.Notes says that their line endings differ. A
// missing newline at the end of a file is a change, as it is for git,
// unless one of the white-space options is set.
//
// Text is compared as Unicode code points without normalization, because
// the NFC and NFD tables are not in the standard library. "é" typed as one
// code point (U+00E9) and as "e" followed by a combining accent (U+0301)
// look the same but compare different. Ignoring case uses simple case
// folding, the rule of strings.EqualFold, so "ß" and "SS" also differ.
//
// # Hostile input
//
// Every exported function returns an error instead of panicking, whatever
// the bytes. Inputs are bounded by Options.Limits; the work of aligning two
// very different inputs is bounded too, and Result.Approximate reports
// when that bound shaped the result.
package gdco
