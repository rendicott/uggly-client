package boxes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestScrollDivBoxPullsHidden(t *testing.T) {
	st := tcell.StyleDefault
	// 3-col, height 4 with borderW=0 → fill 4 rows. Put lines 0..3 visible, 4..5 hidden.
	bi := &DivBox{
		Name:     "main",
		Width:    3,
		Height:   4,
		BorderW:  0,
		FillChar: ' ',
		FillSt:   &st,
		RawContents: [][]*Pixel{
			{{C: 'A', St: st}, {C: 'B', St: st}, {C: 'C', St: st}, {C: 'D', St: st}},
			{{C: '1', St: st}, {C: '2', St: st}, {C: '3', St: st}, {C: '4', St: st}},
			{{C: 'w', St: st}, {C: 'x', St: st}, {C: 'y', St: st}, {C: 'z', St: st}},
		},
		HiddenContents: [][]*Pixel{
			{{C: 'E', St: st}, {C: 'F', St: st}},
			{{C: '5', St: st}, {C: '6', St: st}},
			{{C: '!', St: st}, {C: '?', St: st}},
		},
	}
	ScrollDivBox(bi, 1)
	// After scroll 1: visible should be B,C,D,E
	if bi.RawContents[0][0].C != 'B' || bi.RawContents[0][3].C != 'E' {
		t.Fatalf("col0 after scroll: %c %c %c %c",
			bi.RawContents[0][0].C, bi.RawContents[0][1].C,
			bi.RawContents[0][2].C, bi.RawContents[0][3].C)
	}
	if bi.RawContents[1][0].C != '2' || bi.RawContents[1][3].C != '5' {
		t.Fatalf("col1 after scroll unexpected")
	}
	ScrollDivBox(bi, 0) // no-op
	if bi.RawContents[0][0].C != 'B' {
		t.Fatal("offset 0 should not change")
	}
}
