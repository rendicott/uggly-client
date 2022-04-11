package boxes

import (
	"bytes"
	"strings"
	"github.com/gdamore/tcell/v2"
	"github.com/mitchellh/go-wordwrap"
	"github.com/inconshreveable/log15"
)

// Loggo is the global logger
var Loggo log15.Logger

func (bi *DivBox) addTextBlob(tb *TextBlob) {
	Loggo.Debug("entering addTextBlob")
	fillWidth := bi.fillX2 - bi.fillX1
	fillHeight := bi.fillY2 - bi.fillY1
	var charMap map[int][]rune
	if tb.Wrap {
		hardBreaks := false
		charMap = wrap(*tb.Content, fillWidth, hardBreaks)
	} else {
		charMap = noWrap(*tb.Content)
	}
	for i, _ := range charMap {
		Loggo.Debug("charmap row info",
			"row", i,
			"len", len(charMap[i]),
			"function", "addTextBlob",
		)
	}
	// track how much content we were able to actually fit
	invisible := len(charMap) - fillHeight
	if invisible > 0 {
		Loggo.Info("content exceeds divbox height",
			"invisible", invisible,
			"divBox.Name", bi.Name,
		)
		// instantiate hidden contents storage
		bi.HiddenContents = make([][]*Pixel, bi.Width)
		for i := range bi.HiddenContents{
			bi.HiddenContents[i] = make([]*Pixel, invisible)
		}
	}
	Loggo.Debug("fitment stats",
		"divBox.Name", bi.Name,
		"charMap", len(charMap),
		"fillHeight", fillHeight,
		"invisible-lines", invisible,
	)
	// log some info about colors
	fg, _, _ := tb.Style.Decompose()
	Loggo.Debug("have style color",
		"stylefgcolor", fg.TrueColor(),
		"function", "addTextBlob",
	)
	debugSampleRate := 10
	pixelCount := 0
	logPixels := false
	var logPixel bool
	if pixelCount%debugSampleRate == 0 {
		logPixel = true
	}
	// now fill to max height
	Loggo.Debug("populating divbox with text chars", "fillHeight", fillHeight)
	//for i := 0; i < fillHeight; i++ {
	for i := 0; i < len(charMap); i++ {
		for j, char := range charMap[i] {
			p := Pixel{
				C:        char,
				St:       *tb.Style,
				IsBorder: false,
			}
			if logPixels {
				if logPixel {
					Loggo.Debug("setting bi.RawContents",
						"row", i, "col", j,
						"pixelCount", pixelCount,
						"function", "addTextBlob",
					)
				}
			}
			// protect from index out of range if something else failed
			if j > fillWidth-1{
				Loggo.Error("content exceeds available width")
				break
			}
			if i >= fillHeight { // store in hidden
				bi.HiddenContents[bi.fillX1+j][i-fillHeight] = &p
			} else {
				bi.RawContents[bi.fillX1+j][bi.fillY1+i] = &p
			}
		}
		if i >= fillHeight {
			if len(bi.HiddenContents) > 0 {
				Loggo.Debug("hidden stats",
					"i", i, "fillHeight", fillHeight,
					"invisible", invisible,
					"len(bi.HiddenContents)", len(bi.HiddenContents[0]),
				)
			}
		}
	}
	if invisible > 0 {
		// will use this later to implement scrolling
		if len(bi.HiddenContents) > 0 {
			Loggo.Info("stored hidden content", "divBox.Name", bi.Name, "lines", len(bi.HiddenContents[0]))
		}
	}
}

// Init establishes Borders, padding and instantiates
// Pixelmap with usable space
func (bi *DivBox) Init() {
	Loggo.Debug("initializing box", "BorderChar", bi.BorderChar)
	// set BorderW to 0 if Border is false
	if !bi.Border {
		bi.BorderW = 0
	}
	// set up usable fill space
	bi.fillX1 = bi.BorderW
	bi.fillX2 = bi.Width - bi.BorderW
	bi.fillY1 = bi.BorderW
	bi.fillY2 = bi.Height - bi.BorderW
	// initialize Pixelmap
	bi.RawContents = make([][]*Pixel, bi.Width)
	for i := range bi.RawContents {
		bi.RawContents[i] = make([]*Pixel, bi.Height)
	}
	// fill with Borderchar or blanks
	for i := 0; i < bi.Width; i++ {
		for j := 0; j < bi.Height; j++ {
			var p Pixel
			// fill the whole thing with Border for now
			if bi.Border {
				p = Pixel{
					C:        bi.BorderChar,
					St:       *bi.BorderSt,
					IsBorder: true,
				}
			} else {
				p = Pixel{
					C:        bi.FillChar,
					St:       tcell.StyleDefault,
					IsBorder: false,
				}
			}
			bi.RawContents[i][j] = &p
		}
	}
	// fill non-Border
	for i := bi.fillX1; i < bi.fillX2; i++ {
		for j := bi.fillY1; j < bi.fillY2; j++ {
			p := Pixel{
				C:        bi.FillChar,
				St:       *bi.FillSt,
				IsBorder: false,
			}
			bi.RawContents[i][j] = &p
		}
	}
	// now process other stuff like textBlobs
	for _, tb := range bi.textBlobs {
		bi.addTextBlob(tb)
	}
}

type Pixel struct {
	C        rune
	St       tcell.Style
	IsBorder bool
}

// DivBox holds properties and
// methods for making boxes
type DivBox struct {
	Name        string
	Border      bool
	BorderW     int
	BorderChar  rune
	BorderSt    *tcell.Style
	FillSt      *tcell.Style
	FillChar    rune
	StartX      int
	StartY      int
	Width       int
	Height      int
	RawContents [][]*Pixel
	HiddenContents [][]*Pixel
	// unexported fields
	// usable fill space minus Border
	fillX1     int
	fillX2     int
	fillY1     int
	fillY2     int
	fillWidth  int
	fillHeight int
	textBlobs  []*TextBlob
}

type TextBlob struct {
	Content  *string
	Wrap     bool
	Style    *tcell.Style
	DivNames []string
}

// MateBoxes takes a slice of DivBoxes and attaches
// itself to the DivBox's textBlobs property
func (tb *TextBlob) MateBoxes(bxs []*DivBox) {
	for _, bx := range bxs {
		for _, name := range tb.DivNames {
			if bx.Name == name {
				Loggo.Debug("appending textBlob to div", "name", name)
				bx.textBlobs = append(bx.textBlobs, tb)
			}
		}
	}
}

// thanks to https://stackoverflow.com/users/639133/mozey
func splitSubN(s string, n int) []string {
	sub := ""
	subs := []string{}

	runes := bytes.Runes([]byte(s))
	l := len(runes)
	for i, r := range runes {
		sub = sub + string(r)
		if (i+1)%n == 0 {
			subs = append(subs, sub)
			sub = ""
		} else if (i + 1) == l {
			subs = append(subs, sub)
		}
	}

	return subs
}

func wrap(s string, fillWidth int, hardBreaks bool) map[int][]rune {
	charMap := make(map[int][]rune)
	var splitS []string
	if hardBreaks {
		splitS = splitSubN(s, fillWidth)
	} else {
		sn := wordwrap.WrapString(s, uint(fillWidth))
		splitS = strings.Split(sn, "\n")
		for i, line := range splitS {
			if len(line) > fillWidth {
				// do a hard break anyway and insert
				newSlice := splitSubN(line, fillWidth)
				toAppend := splitS[i+1:]
				splitS = append(splitS[:i], newSlice...)
				splitS = append(splitS, toAppend...)
			}
		}
	}
	for i, line := range splitS {
		charMap[i] = make([]rune, len(line))
		for j, char := range line {
			charMap[i][j] = char
		}
	}
	return charMap
}

func noWrap(s string) map[int][]rune {
	charMap := make(map[int][]rune, 0)
	charMap[0] = make([]rune, len(s))
	for j, char := range s {
		charMap[0][j] = char
	}
	return charMap
}
