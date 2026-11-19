# Test data

## `fuzz/`

Inputs that once made a fuzz target fail, in the format `go test -fuzz`
writes. `go test` replays them on every run, so each one is a regression
test. None was written by hand.

| File | What it found |
| --- | --- |
| `fuzz/FuzzReport/5a52a8d79d04c6e6` | A document name that was not valid UTF-8 reached the HTML report unchanged. Names are now repaired when a document is made, and the HTML writer replaces invalid bytes itself. |
| `fuzz/FuzzCompare/25caa8f3af603410` | `""` against `"\n"`: the test's own idea of equality was wrong (an empty file and a file with one empty line differ). The comparison was right; the test was fixed. |

## Word and Excel fixtures

Phase 2 (`.docx`) and phase 3 (`.xlsx`) add their fixtures here, each with a
line in this file saying how it was made.
