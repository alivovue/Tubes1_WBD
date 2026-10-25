package backend

import (
	"encoding/json"
	"strings"
)

const maxSchemaDiffCells = 2_000_000

type SchemaDiffCell struct {
	Line   int
	Text   string
	Kind   string
	Marker string
}

type SchemaDiffRow struct {
	Before SchemaDiffCell
	After  SchemaDiffCell
}

type SchemaDiffView struct {
	Rows    []SchemaDiffRow
	Added   int
	Removed int
}

type schemaDiffOperation struct {
	kind string
	line int
	text string
}

// BuildSchemaDiffView turns two complete schema snapshots into aligned rows for
// a single, side-by-side diff viewport. Unchanged rows keep their original line
// numbers while additions and removals receive their usual +/- markers.
func BuildSchemaDiffView(before, after any) SchemaDiffView {
	return alignSchemaDiffLines(prettyJSONLines(before), prettyJSONLines(after))
}

func prettyJSONLines(value any) []string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return []string{"null"}
	}
	return strings.Split(string(encoded), "\n")
}

func alignSchemaDiffLines(before, after []string) SchemaDiffView {
	if len(before) == 0 && len(after) == 0 {
		return SchemaDiffView{}
	}

	// The regular path uses an LCS table so insertions and removals stay aligned.
	// Avoid allocating an unbounded matrix for unusually large schemas.
	if len(before) > 0 && len(after) > 0 && len(before) > maxSchemaDiffCells/len(after) {
		return alignLargeSchemaDiff(before, after)
	}

	columns := len(after) + 1
	lcs := make([]int, (len(before)+1)*columns)
	for beforeIndex := len(before) - 1; beforeIndex >= 0; beforeIndex-- {
		for afterIndex := len(after) - 1; afterIndex >= 0; afterIndex-- {
			cell := beforeIndex*columns + afterIndex
			if before[beforeIndex] == after[afterIndex] {
				lcs[cell] = lcs[(beforeIndex+1)*columns+afterIndex+1] + 1
			} else {
				removeScore := lcs[(beforeIndex+1)*columns+afterIndex]
				addScore := lcs[beforeIndex*columns+afterIndex+1]
				if removeScore > addScore {
					lcs[cell] = removeScore
				} else {
					lcs[cell] = addScore
				}
			}
		}
	}

	operations := make([]schemaDiffOperation, 0, len(before)+len(after))
	beforeIndex, afterIndex := 0, 0
	for beforeIndex < len(before) || afterIndex < len(after) {
		switch {
		case beforeIndex < len(before) && afterIndex < len(after) && before[beforeIndex] == after[afterIndex]:
			operations = append(operations, schemaDiffOperation{kind: "equal", line: beforeIndex + 1, text: before[beforeIndex]})
			beforeIndex++
			afterIndex++
		case afterIndex < len(after) && (beforeIndex == len(before) || lcs[beforeIndex*columns+afterIndex+1] >= lcs[(beforeIndex+1)*columns+afterIndex]):
			operations = append(operations, schemaDiffOperation{kind: "added", line: afterIndex + 1, text: after[afterIndex]})
			afterIndex++
		default:
			operations = append(operations, schemaDiffOperation{kind: "removed", line: beforeIndex + 1, text: before[beforeIndex]})
			beforeIndex++
		}
	}

	return rowsFromSchemaDiffOperations(operations)
}

func rowsFromSchemaDiffOperations(operations []schemaDiffOperation) SchemaDiffView {
	view := SchemaDiffView{Rows: make([]SchemaDiffRow, 0, len(operations))}
	for index := 0; index < len(operations); {
		if operations[index].kind == "equal" {
			operation := operations[index]
			view.Rows = append(view.Rows, SchemaDiffRow{
				Before: SchemaDiffCell{Line: operation.line, Text: operation.text, Kind: "equal"},
				After:  SchemaDiffCell{Line: operation.line, Text: operation.text, Kind: "equal"},
			})
			index++
			continue
		}

		end := index
		var removed, added []schemaDiffOperation
		for end < len(operations) && operations[end].kind != "equal" {
			if operations[end].kind == "removed" {
				removed = append(removed, operations[end])
			} else {
				added = append(added, operations[end])
			}
			end++
		}

		rowCount := len(removed)
		if len(added) > rowCount {
			rowCount = len(added)
		}
		for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
			row := SchemaDiffRow{
				Before: SchemaDiffCell{Kind: "empty"},
				After:  SchemaDiffCell{Kind: "empty"},
			}
			if rowIndex < len(removed) {
				operation := removed[rowIndex]
				row.Before = SchemaDiffCell{Line: operation.line, Text: operation.text, Kind: "removed", Marker: "−"}
				view.Removed++
			}
			if rowIndex < len(added) {
				operation := added[rowIndex]
				row.After = SchemaDiffCell{Line: operation.line, Text: operation.text, Kind: "added", Marker: "+"}
				view.Added++
			}
			view.Rows = append(view.Rows, row)
		}
		index = end
	}

	beforeLine, afterLine := 0, 0
	for index := range view.Rows {
		if view.Rows[index].Before.Kind != "empty" {
			beforeLine++
			view.Rows[index].Before.Line = beforeLine
		}
		if view.Rows[index].After.Kind != "empty" {
			afterLine++
			view.Rows[index].After.Line = afterLine
		}
	}
	return view
}

// For very large snapshots, keep common prefix/suffix rows and align the
// changed middle by position. This remains bounded while preserving one shared
// viewport and correct red/green indicators.
func alignLargeSchemaDiff(before, after []string) SchemaDiffView {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}

	operations := make([]schemaDiffOperation, 0, len(before)+len(after))
	for index := 0; index < prefix; index++ {
		operations = append(operations, schemaDiffOperation{kind: "equal", line: index + 1, text: before[index]})
	}
	for index := prefix; index < len(before)-suffix; index++ {
		operations = append(operations, schemaDiffOperation{kind: "removed", line: index + 1, text: before[index]})
	}
	for index := prefix; index < len(after)-suffix; index++ {
		operations = append(operations, schemaDiffOperation{kind: "added", line: index + 1, text: after[index]})
	}
	for offset := suffix; offset > 0; offset-- {
		beforeIndex := len(before) - offset
		operations = append(operations, schemaDiffOperation{kind: "equal", line: beforeIndex + 1, text: before[beforeIndex]})
	}

	return rowsFromSchemaDiffOperations(operations)
}
