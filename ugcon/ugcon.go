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
		u.AddTextBox(&ugform.AddTextBoxInput{
			Name: tb.Name,
			TabOrder: int(tb.TabOrder),
			DefaultValue: tb.DefaultValue,
			Description: tb.Description,
			PositionX: int(tb.PositionX),
			PositionY: int(tb.PositionY),
			Height: int(tb.Height),
			Width: int(tb.Width),
			StyleCursor: *setStyle(tb.StyleCursor.Fg, tb.StyleCursor.Bg),
			StyleFill: *setStyle(tb.StyleFill.Fg, tb.StyleFill.Bg),
			StyleText: *setStyle(tb.StyleText.Fg, tb.StyleText.Bg),
			StyleDescription: *setStyle(tb.StyleDescription.Fg, tb.StyleDescription.Bg),
			ShowDescription: tb.ShowDescription,
			Password: tb.Password,
		})
	}
	return u, err
}
