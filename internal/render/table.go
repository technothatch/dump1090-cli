package render

import (
	"strings"
	"unicode/utf8"
)

// Align controls where a column's text sits inside its width.
type Align uint8

// The two alignments a table needs: labels to the left, numbers to the
// right so decimal points line up.
const (
	AlignLeft Align = iota
	AlignRight
)

// columnGap is inserted between adjacent columns.
const columnGap = "  "

// Column describes one table column: its header and how cells align.
type Column struct {
	// Header is the column heading, written in plain text.
	Header string
	// Align is how cells sit inside the column width.
	Align Align
	// HeaderColor styles the header (usually ColorBold).
	HeaderColor Color
}

// Cell is one table cell: plain text plus the color it renders in.
type Cell struct {
	// Text is the cell content without any escape sequences; width is
	// measured from it.
	Text string
	// Color styles the text when color output is enabled.
	Color Color
}

// Table is an aligned columnar table. Build it with NewTable and Add,
// then Render it once.
type Table struct {
	columns []Column
	rows    [][]Cell
}

// NewTable creates a table with the given columns.
func NewTable(columns ...Column) *Table {
	return &Table{columns: columns}
}

// Add appends a row. Missing cells are treated as empty, extra cells are
// dropped, so a malformed row can never misalign the table.
func (t *Table) Add(cells ...Cell) {
	row := make([]Cell, len(t.columns))
	copy(row, cells)
	t.rows = append(t.rows, row)
}

// Rows reports how many data rows the table holds.
func (t *Table) Rows() int { return len(t.rows) }

// Render writes the header and every row as aligned lines, each ending in
// a newline. Padding is computed from the plain text and colors are
// wrapped around it, so enabling color never changes column alignment.
// Trailing whitespace is trimmed from each line.
func (t *Table) Render(s Style) string {
	widths := make([]int, len(t.columns))
	for i, c := range t.columns {
		widths[i] = utf8.RuneCountInString(c.Header)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if w := utf8.RuneCountInString(cell.Text); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	b.WriteString(t.renderLine(s, func(i int) (string, Color) {
		return t.columns[i].Header, t.columns[i].HeaderColor
	}, widths))
	for _, row := range t.rows {
		row := row
		b.WriteString(t.renderLine(s, func(i int) (string, Color) {
			return row[i].Text, row[i].Color
		}, widths))
	}
	return b.String()
}

// renderLine lays out one line: each cell padded to its column width in
// its column's alignment, two spaces between columns, trailing padding
// removed.
func (t *Table) renderLine(s Style, cellAt func(i int) (string, Color), widths []int) string {
	var b strings.Builder
	for i, col := range t.columns {
		text, color := cellAt(i)
		width := widths[i]
		pad := width - utf8.RuneCountInString(text)
		if pad < 0 {
			pad = 0
		}
		if i > 0 {
			b.WriteString(columnGap)
		}
		if col.Align == AlignRight {
			b.WriteString(strings.Repeat(" ", pad))
			b.WriteString(s.Wrap(color, text))
		} else {
			b.WriteString(s.Wrap(color, text))
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return strings.TrimRight(b.String(), " ") + "\n"
}
