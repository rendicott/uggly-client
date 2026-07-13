package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rendicott/uggly-client/boxes"
)

// mockScreen records SetContent calls for dirty-region assertions.
type mockScreen struct {
	tcell.SimulationScreen
	sets int
}

func newMockScreen(w, h int) *mockScreen {
	s := tcell.NewSimulationScreen("UTF-8")
	_ = s.Init()
	s.SetSize(w, h)
	return &mockScreen{SimulationScreen: s}
}

func TestPaintFrameDiffSkipsUnchanged(t *testing.T) {
	view := newMockScreen(20, 10)
	defer view.Fini()

	var pair paintFramePair
	// Frame 1: full paint
	f := pair.begin(20, 10)
	f.put(1, 1, 'A', tcell.StyleDefault.Foreground(tcell.ColorRed))
	f.put(2, 1, 'B', tcell.StyleDefault)
	w1, s1 := pair.flushDiff(view)
	if w1 == 0 {
		t.Fatalf("first frame should write cells, got written=%d skipped=%d", w1, s1)
	}
	// Frame 2: identical content
	f = pair.begin(20, 10)
	f.put(1, 1, 'A', tcell.StyleDefault.Foreground(tcell.ColorRed))
	f.put(2, 1, 'B', tcell.StyleDefault)
	w2, s2 := pair.flushDiff(view)
	if w2 != 0 {
		t.Fatalf("identical frame should write 0 cells, got written=%d skipped=%d", w2, s2)
	}
	if s2 == 0 {
		t.Fatal("expected some skipped cells")
	}
	// Frame 3: one cell changes
	f = pair.begin(20, 10)
	f.put(1, 1, 'A', tcell.StyleDefault.Foreground(tcell.ColorRed))
	f.put(2, 1, 'X', tcell.StyleDefault) // changed
	w3, _ := pair.flushDiff(view)
	if w3 != 1 {
		t.Fatalf("one cell change should write 1, got %d", w3)
	}
}

func TestPaintFrameResizeForcesFull(t *testing.T) {
	view := newMockScreen(10, 5)
	defer view.Fini()
	var pair paintFramePair
	f := pair.begin(10, 5)
	f.put(0, 0, 'Z', tcell.StyleDefault)
	w1, _ := pair.flushDiff(view)
	if w1 == 0 {
		t.Fatal("expected writes on first frame")
	}
	// resize
	view.SetSize(12, 6)
	f = pair.begin(12, 6)
	f.put(0, 0, 'Z', tcell.StyleDefault)
	w2, s2 := pair.flushDiff(view)
	// forceFull after resize => all cells written
	if w2 != 12*6 {
		t.Fatalf("resize should force full paint, written=%d skipped=%d", w2, s2)
	}
}

func TestPaintFrameStampBox(t *testing.T) {
	var f paintFrame
	f.resize(10, 5)
	f.clear(' ', tcell.StyleDefault)
	st := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	box := &boxes.DivBox{
		StartX: 2,
		StartY: 1,
		Width:  2,
		Height: 1,
		RawContents: [][]*boxes.Pixel{
			{{C: 'H', St: st}},
			{{C: 'i', St: st}},
		},
	}
	f.stampBox(box)
	c, ok := f.get(2, 1)
	if !ok || c.c != 'H' {
		t.Fatalf("expected H at 2,1 got %+v ok=%v", c, ok)
	}
	c, _ = f.get(3, 1)
	if c.c != 'i' {
		t.Fatalf("expected i at 3,1 got %q", string(c.c))
	}
}

func TestBeginFromFrontPreservesBody(t *testing.T) {
	view := newMockScreen(8, 4)
	defer view.Fini()
	var pair paintFramePair
	// Full frame with body char
	f := pair.begin(8, 4)
	f.put(0, 0, 'M', tcell.StyleDefault) // menu
	f.put(0, 3, 'P', tcell.StyleDefault) // page body
	pair.flushDiff(view)
	// Menu-only update via beginFromFront
	f = pair.beginFromFront(8, 4)
	f.put(0, 0, 'N', tcell.StyleDefault) // menu changed
	// body 'P' still in buffer from copy
	w, s := pair.flushDiff(view)
	if w != 1 {
		t.Fatalf("menu strip should dirty 1 cell, written=%d skipped=%d", w, s)
	}
	cell, _ := pair.front.get(0, 3)
	if cell.c != 'P' {
		t.Fatalf("body cell should remain P, got %q", string(cell.c))
	}
}

func TestDirtyPercent(t *testing.T) {
	if dirtyPercent(25, 75) != 25 {
		t.Fatalf("got %d", dirtyPercent(25, 75))
	}
	if dirtyPercent(0, 0) != 0 {
		t.Fatal("zero total")
	}
}
