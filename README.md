# gdco

Compares two texts, two Word documents or two Excel workbooks and shows
what changed, in Go with only the standard library, so that it can run as
WebAssembly in a browser: the files people compare never leave their
machine.

- Lines are aligned with Myers' O(ND) algorithm in linear space. Lines that
  replaced each other are then compared word by word, or character by
  character.
- Case, white-space changes, all white space, trailing white space and blank
  lines can be ignored, with the same meaning as diff's `-i -b -w -Z -B`.
- Results come as a structured value (JSON for JavaScript), as a unified
  diff that `patch` and `git apply` accept, and as a self-contained HTML
  report that works in light and dark mode and prints.
- Word documents (`.docx`, `.docm`, `.dotx`) are read in reading order:
  paragraphs and table rows become lines, text boxes follow the paragraph
  that anchors them, tracked changes are read as accepted (or rejected),
  list numbers are shown but not compared, so inserting clause 3 does not
  mark clauses 4 to 40. Headers, footers, footnotes, endnotes and comments
  are compared as parts of their own. Every line knows where it is:
  "Table 2, row 3", "Footnote 1", "Page 4, paragraph 12".
- Excel workbooks (`.xlsx`, `.xlsm`) are compared sheet by sheet, matched
  by name; a sheet on one side only is reported as added or removed. Rows
  are aligned like lines, so an inserted row does not mark every row below
  it, and the cells of aligned rows are compared by column: "B7 → B8:
  1200 → 1250". Dates show as ISO dates (both the 1900 and the 1904 date
  systems), other numbers as the file writes them; formulas can be
  compared instead of their values.
- Zip and XML input is treated as hostile: sizes, entry counts, compression
  ratios and XML nesting are bounded, and no entity or DTD is ever
  resolved.
- Text arrives as bytes (UTF-8, UTF-16 with a byte order mark, or
  Windows-1252), as a string, or as labelled segments such as the pages of a
  PDF whose text was extracted elsewhere.

```go
r, err := gdco.CompareText(old, new, nil)
if err != nil {
	return err
}
fmt.Println(r.Stats.LinesChanged, "lines changed")
r.WriteUnified(os.Stdout)
r.WriteHTML(w, &gdco.ReportOptions{Generated: "2026-11-19"})
```

## WebAssembly

```sh
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o gdco.wasm ./wasm
```

Load it with the `wasm_exec.js` of the same Go version
(`$(go env GOROOT)/lib/wasm/wasm_exec.js`), from a Web Worker. It defines
one global object:

```js
gdco.compareText(a, b, options)                          // strings
gdco.compareFiles(bytesA, nameA, bytesB, nameB, options) // Uint8Array
gdco.compareSegments(docA, docB, options)                // {name, segments: [{text, page, label}]}
```

Each call returns a string: the result as JSON by default, or the unified
diff or the HTML report when `options.output` is `"unified"` or `"html"`.
A failure returns `{error, code}` instead. The options are `ignoreCase`,
`ignoreSpaceChange`, `ignoreAllSpace`, `ignoreTrailingSpace`,
`ignoreBlankLines`, `granularity` (`"word"`, `"char"`, `"line"`), `context`
(lines, `-1` for the whole document), `nameA`, `nameB`, for Word documents
`tracked` (`"accepted"`, `"original"`) and `headers`, `footers`, `footnotes`,
`endnotes`, `comments` (all `true` by default), for workbooks `formulas`,
and
`report: {title, layout, generated}`.

## Limits worth knowing

- Text is compared as Unicode code points, without normalization (NFC and
  NFD need tables that are not in the standard library). "é" typed as one
  code point and as "e" plus a combining accent look alike but compare
  different.
- Ignoring case uses simple case folding, the rule of `strings.EqualFold`:
  "ß" and "SS" differ.
- Line endings are not compared. A missing newline at the end of a file is
  a change, as in git, unless a white-space option is on.
- Files that are not UTF-8 and have no byte order mark are read as
  Windows-1252. Text in other legacy encodings, such as Windows-1250 for
  Romanian, still compares correctly against itself but shows the wrong
  letters.

## Tests

```sh
go test ./...
go test -run '^$' -fuzz FuzzCompare -fuzztime 60s .
```

The unified-diff tests apply every diff with `patch` and `git apply`, when
they are installed, and require the second file back byte for byte.
