// ugcon converts the uggly protocol objects that
/// come over the wire into client side objects
package ugcon

import (
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	"github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/ugform"
)

var Loggo log15.Logger

// setStyle takes a foreground and background color string and 
// converts it to a tcell Style struct
func setStyle(fgcolor, bgcolor string) (style *tcell.Style) {
	var st tcell.Style
	if fgcolor != "" {
		Loggo.Debug("lookup color", "uggcolor", fgcolor)
		colorFg := tcell.GetColor(fgcolor)
		Loggo.Debug("got fg color", "tcellcolor", colorFg)
		st = st.Foreground(colorFg)
	} else {
		st.Foreground(tcell.ColorReset)
	}
	if bgcolor != "" {
		colorBg := tcell.GetColor(bgcolor)
		st = st.Background(colorBg)
	} else {
		st.Background(tcell.ColorReset)
	}
	style = &st
	return style
}

// ConvertTextBlobLocalBoxes converts an uggly
// formatted TextBlob into a Boxes package version
func ConvertTextBlobLocalBoxes(
	utb *uggly.TextBlob) (*boxes.TextBlob, error) {
	var err error
	tb := boxes.TextBlob{
		Content:  &utb.Content,
		Wrap:     utb.Wrap,
		DivNames: utb.DivNames,
		// Style:    *utb.Style, // have to convert this
	}
	if utb.Style != nil {
		tb.Style = setStyle(utb.Style.Fg, utb.Style.Bg)
	} else {
		tb.Style = &tcell.StyleDefault
	}
	return &tb, err
}
// ConvertDivBoxLocalBoxes converts an uggly // formatted DivBox into a Boxes package version
func ConvertDivBoxLocalBoxes(
	udb *uggly.DivBox) (*boxes.DivBox, error) {
	var err error
	b := boxes.DivBox{
		Name:       udb.Name,
		Border:     udb.Border,
		BorderW:    int(udb.BorderW),
		BorderChar: rune(udb.BorderChar),
		FillChar:   rune(udb.FillChar),
		StartX:     int(udb.StartX),
		StartY:     int(udb.StartY),
		Width:      int(udb.Width),
		Height:     int(udb.Height),
		// BorderSt:    *tcell.Style
		// FillSt:      *tcell.Style
	}
	if udb.BorderSt != nil {
		//colorFg := tcell.GetColor(udb.BorderSt.Fg)
		b.BorderSt = setStyle(udb.BorderSt.Fg, udb.BorderSt.Bg)
	} else {
		b.BorderSt = &tcell.StyleDefault
	}
	if udb.FillSt != nil {
		b.FillSt = setStyle(udb.FillSt.Fg, udb.FillSt.Bg)
	} else {
		b.FillSt = &tcell.StyleDefault
	}
	return &b, err
}

func ConvertFormLocalForm (uf *uggly.Form, s tcell.Screen) (*ugform.Form, error) {
	var err error
	u := ugform.NewForm(s)
	u.Name = uf.Name
	u.SubmitAction = uf.SubmitLink
	for _, tb := range uf.TextBoxes {
		fgCursor, bgCursor := "black", "white"
		fgFill, bgFill := "white", "blue"
		fgText, bgText := "white", "blue"
		fgDesc, bgDesc := "white", "black"
		if tb.StyleCursor != nil {
			if tb.StyleCursor.Fg != "" {
				fgCursor = tb.StyleCursor.Fg
			}
			if tb.StyleCursor.Bg != "" {
				bgCursor = tb.StyleCursor.Bg
			}
		}
		if tb.StyleFill != nil {
			if tb.StyleFill.Fg != "" {
				fgFill = tb.StyleFill.Fg
			}
			if tb.StyleFill.Bg != "" {
				bgFill = tb.StyleFill.Bg
			}
		}
		if tb.StyleText != nil {
			if tb.StyleText.Fg != "" {
				fgText = tb.StyleText.Fg
			}
			if tb.StyleText.Bg != "" {
				bgText = tb.StyleText.Bg
			}
		}
		if tb.StyleDescription != nil {
			if tb.StyleDescription.Fg != "" {
				fgDesc = tb.StyleDescription.Fg
			}
			if tb.StyleDescription.Bg != "" {
				bgDesc = tb.StyleDescription.Bg
			}
		}
		u.AddTextBox(&ugform.AddTextBoxInput{
			Name: tb.Name,
			TabOrder: int(tb.TabOrder),
			DefaultValue: tb.DefaultValue,
			Description: tb.Description,
			PositionX: int(tb.PositionX),
			PositionY: int(tb.PositionY),
			Height: int(tb.Height),
			Width: int(tb.Width),
			StyleCursor: *setStyle(fgCursor, bgCursor),
			StyleFill: *setStyle(fgFill, bgFill),
			StyleText: *setStyle(fgText, bgText),
			StyleDescription: *setStyle(fgDesc, bgDesc),
			ShowDescription: tb.ShowDescription,
			Password: tb.Password,
		})
	}
	return u, err
}
