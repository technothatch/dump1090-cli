package render

import "testing"

func TestTableRenderGolden(t *testing.T) {
	newTable := func() *Table {
		t := NewTable(
			Column{Header: "HEX", Align: AlignLeft, HeaderColor: ColorBold},
			Column{Header: "COUNT", Align: AlignRight, HeaderColor: ColorBold},
			Column{Header: "NOTE", Align: AlignLeft, HeaderColor: ColorBold},
		)
		t.Add(Cell{Text: "3c673a"}, Cell{Text: "2"}, Cell{Text: "near"})
		t.Add(Cell{Text: "abc"}, Cell{Text: "10"}, Cell{Text: ""})
		return t
	}

	tests := []struct {
		name  string
		style Style
		want  string
	}{
		{
			name:  "plain",
			style: NewStyle(false),
			want: "HEX     COUNT  NOTE\n" +
				"3c673a      2  near\n" +
				"abc        10\n",
		},
		{
			name:  "colored with padding outside the codes",
			style: NewStyle(true),
			want: "\x1b[1mHEX\x1b[0m     \x1b[1mCOUNT\x1b[0m  \x1b[1mNOTE\x1b[0m\n" +
				"\x1b[36m3c673a\x1b[0m      2  near\n" +
				"\x1b[36mabc\x1b[0m        10\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := newTable()
			// Apply per-cell colors only in the colored variant: build
			// once, recolor by re-adding rows.
			if tt.style.Enabled() {
				tbl = NewTable(tbl.columns...)
				tbl.Add(Cell{Text: "3c673a", Color: ColorCyan}, Cell{Text: "2"}, Cell{Text: "near"})
				tbl.Add(Cell{Text: "abc", Color: ColorCyan}, Cell{Text: "10"}, Cell{Text: ""})
			}
			got := tbl.Render(tt.style)
			if got != tt.want {
				t.Fatalf("Render:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestTableHeaderOnly(t *testing.T) {
	tbl := NewTable(Column{Header: "SQUAWK", Align: AlignLeft, HeaderColor: ColorBold})
	got := tbl.Render(NewStyle(false))
	if got != "SQUAWK\n" {
		t.Fatalf("Render = %q, want %q", got, "SQUAWK\n")
	}
	if tbl.Rows() != 0 {
		t.Fatalf("Rows = %d, want 0", tbl.Rows())
	}
}

func TestTableAddCellBounds(t *testing.T) {
	tbl := NewTable(Column{Header: "A", Align: AlignLeft}, Column{Header: "B", Align: AlignLeft})
	tbl.Add()                                                  // too few cells
	tbl.Add(Cell{Text: "1"}, Cell{Text: "2"}, Cell{Text: "3"}) // extra dropped
	if tbl.Rows() != 2 {
		t.Fatalf("Rows = %d, want 2", tbl.Rows())
	}
	got := tbl.Render(NewStyle(false))
	want := "A  B\n" +
		"\n" +
		"1  2\n"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

// TestTableWideCellsShift verifies the layout adapts when data outgrows
// the header, and that trailing padding is trimmed.
func TestTableWideCellsShift(t *testing.T) {
	tbl := NewTable(
		Column{Header: "ID", Align: AlignLeft},
		Column{Header: "STATE", Align: AlignRight},
	)
	tbl.Add(Cell{Text: "long-address-here"}, Cell{Text: "tracking"})
	got := tbl.Render(NewStyle(false))
	want := "ID                    STATE\n" +
		"long-address-here  tracking\n"
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}
