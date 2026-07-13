package main

import (
	"fmt"
	"strings"

	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggo"
)

// expandPage normalizes string char fields and expands high-level widgets
// (Table, List, Prompt) and optional root Node trees into legacy DivBoxes /
// TextBlobs / Forms / KeyStrokes so the existing render path can draw them.
func expandPage(page *pb.PageResponse, clientW, clientH int) *pb.PageResponse {
	if page == nil {
		return page
	}
	if page.DivBoxes == nil {
		page.DivBoxes = &pb.DivBoxes{}
	}
	if page.Elements == nil {
		page.Elements = &pb.Elements{}
	}

	// Theme defaults for missing styles (lightweight)
	theme := page.Theme

	for _, box := range page.DivBoxes.Boxes {
		normalizeDivBox(box, theme)
	}

	if page.Root != nil && page.Root.Id != "" {
		expandNode(page, page.Root, 0, 0, clientW, max(3, clientH-4), theme)
		page.Root = nil
	}

	for _, t := range page.Elements.Tables {
		expandTable(page, t, clientW, theme)
	}
	for _, lst := range page.Elements.Lists {
		expandList(page, lst, theme)
	}
	for _, p := range page.Elements.Prompts {
		expandPrompt(page, p, clientW, clientH, theme)
	}
	// clear so re-expand is idempotent
	page.Elements.Tables = nil
	page.Elements.Lists = nil
	page.Elements.Prompts = nil

	// Autogenerate footer hints from keystroke hints if no footer-ish content
	// (optional; skip if page already dense)

	return page
}

func normalizeDivBox(box *pb.DivBox, theme *pb.Theme) {
	if box == nil {
		return
	}
	if box.BorderCharStr != "" {
		box.BorderChar = uggo.ConvertStringCharRune(box.BorderCharStr)
	}
	if box.FillCharStr != "" {
		box.FillChar = uggo.ConvertStringCharRune(box.FillCharStr)
	}
	if box.Border && box.BorderChar == 0 {
		ch := "#"
		if theme != nil && theme.BorderChar != "" {
			ch = theme.BorderChar
		}
		box.BorderChar = uggo.ConvertStringCharRune(ch)
	}
	if box.FillChar == 0 {
		ch := " "
		if theme != nil && theme.FillChar != "" {
			ch = theme.FillChar
		}
		box.FillChar = uggo.ConvertStringCharRune(ch)
	}
	if box.BorderSt == nil && theme != nil && theme.Border != nil {
		box.BorderSt = theme.Border
	}
	if box.FillSt == nil && theme != nil && theme.DefaultText != nil {
		box.FillSt = theme.DefaultText
	}
	// map attrs enum to legacy attr string when present
	if box.BorderSt != nil {
		normalizeStyle(box.BorderSt)
	}
	if box.FillSt != nil {
		normalizeStyle(box.FillSt)
	}
}

func normalizeStyle(st *pb.Style) {
	if st == nil {
		return
	}
	if st.Attr == "" && len(st.Attrs) > 0 {
		for _, a := range st.Attrs {
			if a == pb.Attr_UNDERLINE {
				st.Attr = "4"
				break
			}
		}
	}
}

func expandTable(page *pb.PageResponse, t *pb.Table, clientW int, theme *pb.Theme) {
	if t == nil {
		return
	}
	headers := t.Headers
	var rows [][]string
	for _, r := range t.Rows {
		rows = append(rows, r.Cells)
	}
	nCols := len(headers)
	if nCols == 0 && len(rows) > 0 {
		nCols = len(rows[0])
		headers = make([]string, nCols)
		for i := 0; i < nCols; i++ {
			headers[i] = fmt.Sprintf("c%d", i)
		}
	}
	colW := make([]int, nCols)
	for i := 0; i < nCols; i++ {
		w := 3
		if i < len(headers) && len(headers[i]) > w {
			w = len(headers[i])
		}
		for _, row := range rows {
			if i < len(row) && len(row[i]) > w {
				w = len(row[i])
			}
		}
		if w > 40 {
			w = 40
		}
		colW[i] = w
	}
	total := 0
	for _, w := range colW {
		total += w
	}
	if total+2 > clientW && total > 0 {
		scale := float64(clientW-2) / float64(total)
		for i := range colW {
			colW[i] = max(3, int(float64(colW[i])*scale))
		}
	}

	startY := int(t.StartY)
	if startY == 0 {
		startY = 1
	}
	startX := 1
	name := t.Name
	if name == "" {
		name = "table"
	}

	// One DivBox per row (not per cell) — O(rows) boxes instead of O(rows×cols).
	// Columns stay aligned via fixed-width padded fields; zebra/header styles apply per row.
	formatRow := func(cells []string) string {
		var b strings.Builder
		for i := 0; i < nCols; i++ {
			w := 8
			if i < len(colW) {
				w = colW[i]
			}
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			if len(cell) > w {
				cell = cell[:w]
			} else if len(cell) < w {
				cell = cell + strings.Repeat(" ", w-len(cell))
			}
			b.WriteString(cell)
		}
		return b.String()
	}

	rowStyle := func(rowIdx int, header bool) (fg, bg string) {
		fg, bg = "black", "dimgrey"
		if header {
			fg, bg = "black", "yellow"
			if t.HeaderStyle != nil {
				if t.HeaderStyle.Bg != "" {
					bg = t.HeaderStyle.Bg
				}
				if t.HeaderStyle.Fg != "" {
					fg = t.HeaderStyle.Fg
				}
			}
			return fg, bg
		}
		if t.Zebra && rowIdx%2 == 0 {
			fg, bg = "black", "lightslategrey"
			if t.AltCellStyle != nil {
				if t.AltCellStyle.Bg != "" {
					bg = t.AltCellStyle.Bg
				}
				if t.AltCellStyle.Fg != "" {
					fg = t.AltCellStyle.Fg
				}
			}
			return fg, bg
		}
		if t.CellStyle != nil {
			if t.CellStyle.Bg != "" {
				bg = t.CellStyle.Bg
			}
			if t.CellStyle.Fg != "" {
				fg = t.CellStyle.Fg
			}
		}
		return fg, bg
	}

	paintRow := func(cells []string, y, rowIdx int, header bool) {
		line := formatRow(cells)
		rowW := len(line)
		if rowW < 1 {
			rowW = 1
		}
		fg, bg := rowStyle(rowIdx, header)
		bname := fmt.Sprintf("%s_r%d", name, rowIdx)
		page.DivBoxes.Boxes = append(page.DivBoxes.Boxes, &pb.DivBox{
			Name:        bname,
			Border:      false,
			StartX:      int32(startX),
			StartY:      int32(y),
			Width:       int32(rowW),
			Height:      1,
			FillCharStr: " ",
			FillChar:    uggo.ConvertStringCharRune(" "),
			FillSt:      uggo.Style(fg, bg),
		})
		page.Elements.TextBlobs = append(page.Elements.TextBlobs, &pb.TextBlob{
			Content:  line,
			Wrap:     false,
			Style:    uggo.Style(fg, bg),
			DivNames: []string{bname},
		})
	}

	y := startY
	if len(headers) > 0 {
		paintRow(headers, y, 0, true)
		y++
	}
	for ri, row := range rows {
		cells := make([]string, nCols)
		copy(cells, row)
		paintRow(cells, y+ri, ri+1, false)
	}
}

func expandList(page *pb.PageResponse, lst *pb.ItemList, theme *pb.Theme) {
	if lst == nil {
		return
	}
	var lines []string
	for i, item := range lst.Items {
		key := item.Key
		if key == "" && i < len(uggo.StrokeMap) {
			key = uggo.StrokeMap[i]
		}
		prefix := ""
		if key != "" {
			prefix = fmt.Sprintf("(%s) ", key)
		}
		lines = append(lines, prefix+item.Label)
		ks := &pb.KeyStroke{KeyStroke: key}
		if item.Event != nil && item.Event.Name != "" {
			ks.Action = &pb.KeyStroke_Event{Event: item.Event}
		} else if item.Link != nil {
			if item.Link.KeyStroke == "" {
				item.Link.KeyStroke = key
			}
			ks.Action = &pb.KeyStroke_Link{Link: item.Link}
		} else {
			continue
		}
		page.KeyStrokes = append(page.KeyStrokes, ks)
	}
	content := strings.Join(lines, "\n")
	div := lst.DivName
	if div == "" {
		div = lst.Name
	}
	if div == "" {
		div = "list"
	}
	if !hasBox(page, div) {
		st := uggo.Style("white", "black")
		if lst.Style != nil {
			st = lst.Style
		} else if theme != nil && theme.DefaultText != nil {
			st = theme.DefaultText
		}
		page.DivBoxes.Boxes = append(page.DivBoxes.Boxes, &pb.DivBox{
			Name:        div,
			Border:      true,
			BorderW:     1,
			StartX:      1,
			StartY:      1,
			Width:       60,
			Height:      int32(max(3, len(lines)+2)),
			BorderChar:  uggo.ConvertStringCharRune("="),
			FillChar:    uggo.ConvertStringCharRune(" "),
			BorderSt:    uggo.Style("cyan", "black"),
			FillSt:      st,
		})
	}
	st := uggo.Style("white", "black")
	if lst.Style != nil {
		st = lst.Style
	}
	page.Elements.TextBlobs = append(page.Elements.TextBlobs, &pb.TextBlob{
		Content:  content,
		Wrap:     false,
		Style:    st,
		DivNames: []string{div},
	})
}

func expandPrompt(page *pb.PageResponse, p *pb.Prompt, clientW, clientH int, theme *pb.Theme) {
	if p == nil {
		return
	}
	div := p.DivName
	if div == "" {
		div = p.Name + "_div"
	}
	field := p.FieldName
	if field == "" {
		field = "input"
	}
	w := int(p.Width)
	if w <= 0 {
		w = min(50, clientW-10)
	}
	y := max(0, clientH-8)
	if !hasBox(page, div) {
		page.DivBoxes.Boxes = append(page.DivBoxes.Boxes, &pb.DivBox{
			Name:       div,
			Border:     true,
			BorderW:    1,
			StartX:     2,
			StartY:     int32(y),
			Width:      int32(w + 12),
			Height:     5,
			BorderChar: uggo.ConvertStringCharRune("-"),
			FillChar:   uggo.ConvertStringCharRune(" "),
			BorderSt:   uggo.Style("cyan", "black"),
			FillSt:     uggo.Style("white", "black"),
		})
	}
	posX := int32(2)
	if p.Label != "" {
		posX = int32(len(p.Label) + 2)
	}
	form := &pb.Form{
		Name:       p.Name,
		DivName:    div,
		Autofocus:  true,
		SubmitLink: p.SubmitLink,
		TextBoxes: []*pb.TextBox{
			{
				Name:            field,
				TabOrder:        1,
				DefaultValue:    p.Placeholder,
				Description:     p.Label,
				ShowDescription: p.Label != "",
				PositionX:       posX,
				PositionY:       2,
				Height:          1,
				Width:           int32(w),
				Password:        p.Password,
				StyleCursor:     uggo.Style("black", "white"),
				StyleFill:       uggo.Style("white", "blue"),
				StyleText:       uggo.Style("white", "blue"),
				StyleDescription: uggo.Style("white", "black"),
			},
		},
	}
	// Honor server autofocus flag; always register (i) for activation.
	form.Autofocus = p.Autofocus
	page.Elements.Forms = append(page.Elements.Forms, form)
	// Avoid duplicate (i) if server already sent FormActivation for this form
	hasI := false
	for _, ks := range page.KeyStrokes {
		if ks.KeyStroke == "i" {
			hasI = true
			break
		}
	}
	if !hasI {
		page.KeyStrokes = append(page.KeyStrokes, &pb.KeyStroke{
			KeyStroke: "i",
			Hint:      "(i) Input",
			Action: &pb.KeyStroke_FormActivation{
				FormActivation: &pb.FormActivation{FormName: p.Name},
			},
		})
	}
}

func expandNode(page *pb.PageResponse, node *pb.Node, x, y, w, h int, theme *pb.Theme) {
	if node == nil {
		return
	}
	lay := node.Layout
	if lay != nil && lay.Kind == pb.Layout_ABSOLUTE {
		if lay.X != 0 {
			x = int(lay.X)
		}
		if lay.Y != 0 {
			y = int(lay.Y)
		}
		if lay.W != 0 {
			w = int(lay.W)
		}
		if lay.H != 0 {
			h = int(lay.H)
		}
	}

	switch body := node.Body.(type) {
	case *pb.Node_Box:
		box := body.Box
		if box.Name == "" {
			box.Name = node.Id
		}
		box.StartX = int32(x)
		box.StartY = int32(y)
		if box.Width <= 0 {
			box.Width = int32(w)
		}
		if box.Height <= 0 {
			box.Height = int32(h)
		}
		normalizeDivBox(box, theme)
		page.DivBoxes.Boxes = append(page.DivBoxes.Boxes, box)
	case *pb.Node_Text:
		div := node.Id
		if div == "" {
			div = "text"
		}
		if !hasBox(page, div) {
			page.DivBoxes.Boxes = append(page.DivBoxes.Boxes, &pb.DivBox{
				Name:       div,
				StartX:     int32(x),
				StartY:     int32(y),
				Width:      int32(w),
				Height:     int32(h),
				FillChar:   uggo.ConvertStringCharRune(" "),
				FillSt:     uggo.Style("white", "black"),
			})
		}
		tb := body.Text
		found := false
		for _, n := range tb.DivNames {
			if n == div {
				found = true
				break
			}
		}
		if !found {
			tb.DivNames = append(tb.DivNames, div)
		}
		if tb.Style != nil {
			normalizeStyle(tb.Style)
		}
		page.Elements.TextBlobs = append(page.Elements.TextBlobs, tb)
	case *pb.Node_Table:
		body.Table.StartY = int32(y)
		expandTable(page, body.Table, w, theme)
	case *pb.Node_ItemList:
		expandList(page, body.ItemList, theme)
	case *pb.Node_Prompt:
		expandPrompt(page, body.Prompt, w, h+y, theme)
	case *pb.Node_Form:
		page.Elements.Forms = append(page.Elements.Forms, body.Form)
	}

	children := node.Children
	if len(children) == 0 {
		return
	}
	gap, pad := 0, 0
	kind := pb.Layout_STACK_V
	if lay != nil {
		gap = int(lay.Gap)
		pad = int(lay.Pad)
		kind = lay.Kind
	}
	innerX, innerY := x+pad, y+pad
	innerW, innerH := max(1, w-2*pad), max(1, h-2*pad)
	n := len(children)

	if kind == pb.Layout_STACK_H {
		cellW := max(1, (innerW-gap*max(0, n-1))/n)
		cx := innerX
		for _, ch := range children {
			cw := cellW
			if ch.Layout != nil && ch.Layout.W > 0 {
				cw = int(ch.Layout.W)
			}
			expandNode(page, ch, cx, innerY, cw, innerH, theme)
			cx += cw + gap
		}
		return
	}
	// STACK_V / FILL / default
	cellH := max(1, (innerH-gap*max(0, n-1))/max(n, 1))
	cy := innerY
	for _, ch := range children {
		chH := cellH
		if ch.Layout != nil && ch.Layout.H > 0 {
			chH = int(ch.Layout.H)
		}
		expandNode(page, ch, innerX, cy, innerW, chH, theme)
		cy += chH + gap
	}
}

func hasBox(page *pb.PageResponse, name string) bool {
	for _, b := range page.DivBoxes.Boxes {
		if b.Name == name {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
