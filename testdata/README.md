# Test data

## `docx/`

| File | How it was made |
| --- | --- |
| `src/contract-v1.fodt`, `src/contract-v2.fodt` | Written by hand as flat OpenDocument XML: a short contract with a numbered two-level list, a bullet list, a table, a footnote, a header, a footer and a text frame. Version 2 changes words, inserts a list item and a table row, edits the header, the footnote and the frame, and records three tracked changes (two insertions, one deletion). |
| `contract-v1.docx`, `contract-v2.docx` | Converted from the sources by LibreOffice 26.8 with `make-libreoffice.sh` (`soffice --headless --convert-to docx`). LibreOffice writes the frame as a text box inside `mc:AlternateContent`, with a DrawingML Choice and a VML Fallback holding the same text. |
| `word-features.docx` | Written by `TestWriteFixtures` in `internal/docx` (`go test ./internal/docx -run TestWriteFixtures -update`) from XML in the tests, for what LibreOffice does not write: a deleted paragraph mark, moves, nested and multi-paragraph fields, a first-page header, endnotes, comments (one not anchored), list numbering that comes from a paragraph style, Word's page-layout record (`w:lastRenderedPageBreak`) and a nested table. |
| `*.accepted.txt`, `*.original.txt` | The text read from each document, with tracked changes accepted and rejected, one line per line: location, list marker and shape, text. `TestFixtures` in `internal/docx` compares against them; `-update` rewrites them, to be checked by eye. |

## `fuzz/`

Inputs that once made a fuzz target fail, in the format `go test -fuzz`
writes. `go test` replays them on every run, so each one is a regression
test. None was written by hand.

| File | What it found |
| --- | --- |
| `fuzz/FuzzReport/5a52a8d79d04c6e6` | A document name that was not valid UTF-8 reached the HTML report unchanged. Names are now repaired when a document is made, and the HTML writer replaces invalid bytes itself. |
| `fuzz/FuzzCompare/25caa8f3af603410` | `""` against `"\n"`: the test's own idea of equality was wrong (an empty file and a file with one empty line differ). The comparison was right; the test was fixed. |

## Excel fixtures

Phase 3 (`.xlsx`) adds its fixtures here, each with a line in this file
saying how it was made.
