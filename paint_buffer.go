package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rendicott/uggly-client/boxes"
)

// paintCell is one terminal cell in the compositor.
type paintCell struct {
	c  rune
	st tcell.Style
}

// paintFrame is a full-screen cell buffer used for dirty-region painting.
// We build the desired frame each draw, then only SetContent cells that differ
// from the last flushed frame. This is the main win for streaming pages where
// most of the chrome and unchanged body cells stay the same between frames.
type paintFrame struct {
	w, h int
	cells []paintCell
}

func (f *paintFrame) resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if f.w == w && f.h == h && len(f.cells) == w*h {
		return
	}
	f.w, f.h = w, h
	f.cells = make([]paintCell, w*h)
}

func (f *paintFrame) clear(fill rune, st tcell.Style) {
	if fill == 0 {
		fill = ' '
	}
	for i := range f.cells {
		f.cells[i] = paintCell{c: fill, st: st}
	}
}

func (f *paintFrame) put(x, y int, c rune, st tcell.Style) {
	if x < 0 || y < 0 || x >= f.w || y >= f.h {
		return
	}
	f.cells[y*f.w+x] = paintCell{c: c, st: st}
}

func (f *paintFrame) get(x, y int) (paintCell, bool) {
	if x < 0 || y < 0 || x >= f.w || y >= f.h {
		return paintCell{}, false
	}
	return f.cells[y*f.w+x], true
}

func (f *paintFrame) equalAt(x, y int, c rune, st tcell.Style) bool {
	cell, ok := f.get(x, y)
	if !ok {
		return false
	}
	return cell.c == c && cell.st == st
}

// stampBox writes a DivBox's RawContents into the frame at the box position.
func (f *paintFrame) stampBox(bi *boxes.DivBox) {
	if bi == nil || bi.RawContents == nil {
		return
	}
	for i := 0; i < bi.Width; i++ {
		if i >= len(bi.RawContents) {
			break
		}
		col := bi.RawContents[i]
		for j := 0; j < bi.Height; j++ {
			if j >= len(col) || col[j] == nil {
				continue
			}
			f.put(bi.StartX+i, bi.StartY+j, col[j].C, col[j].St)
		}
	}
}

// paintFramePair holds the desired (back) and last-flushed (front) buffers.
type paintFramePair struct {
	front paintFrame // what is currently on the tcell screen
	back  paintFrame // what we want to show next
	// forceFull makes the next flush rewrite every cell (resize / first paint)
	forceFull bool
	// stats for debugging / tests
	lastWritten int
	lastSkipped int
}

func (p *paintFramePair) invalidate() {
	p.forceFull = true
	p.front.w, p.front.h = 0, 0
	p.front.cells = nil
}

// begin prepares the back buffer: same size as the screen, filled with blanks.
func (p *paintFramePair) begin(w, h int) *paintFrame {
	if w != p.front.w || h != p.front.h {
		p.forceFull = true
		p.front.resize(w, h)
	}
	p.back.resize(w, h)
	p.back.clear(' ', tcell.StyleDefault)
	return &p.back
}

// flushDiff writes only changed cells to the screen and swaps buffers.
// Returns (written, skipped) cell counts.
func (p *paintFramePair) flushDiff(view tcell.Screen) (written, skipped int) {
	if view == nil {
		return 0, 0
	}
	w, h := p.back.w, p.back.h
	if w == 0 || h == 0 {
		return 0, 0
	}
	if len(p.front.cells) != w*h {
		p.front.resize(w, h)
		p.forceFull = true
	}
	force := p.forceFull
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			i := row + x
			want := p.back.cells[i]
			if !force {
				have := p.front.cells[i]
				if have.c == want.c && have.st == want.st {
					skipped++
					continue
				}
			}
			view.SetContent(x, y, want.c, nil, want.st)
			written++
		}
	}
	// Swap: front becomes what we just painted
	p.front, p.back = p.back, p.front
	p.forceFull = false
	p.lastWritten = written
	p.lastSkipped = skipped
	return written, skipped
}

// flushRegionDiff is like flushDiff but only considers cells inside the rectangle
// [x0,x1) × [y0,y1). Cells outside the region are left unchanged in front
// (and must not be stamped into back for this pass — caller should only stamp
// the region of interest, then we merge).
//
// Simpler approach used by menu strip: stamp menu into a full back buffer that
// starts as a copy of front, then only menu rows change.
func (p *paintFramePair) beginFromFront(w, h int) *paintFrame {
	if w != p.front.w || h != p.front.h {
		return p.begin(w, h)
	}
	p.back.resize(w, h)
	copy(p.back.cells, p.front.cells)
	return &p.back
}
