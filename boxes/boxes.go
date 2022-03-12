package boxes

import (
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
)

// Loggo is the global logger
var Loggo log15.Logger

func (bi *DivBox) addTextBlob(tb *TextBlob) {
	Loggo.Info("entering addTextBlob")
	fillWidth := bi.fillX2 - bi.fillX1
	fillHeight := bi.fillY2 - bi.fillY1
	var charMap map[int][]rune
	if tb.Wrap {
		hardBreaks := false
		charMap = wrap(*tb.Content, fillWidth, hardBreaks)
	}
	for i, _ := range charMap {
		Loggo.Debug("charmap row info",
			"row", i,
			"len", len(charMap[i]),
			"function", "addTextBlob",
		)
	}
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
	for i := 0; i < fillHeight; i++ {
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
			bi.RawContents[bi.fillX1+j][bi.fillY1+i] = &p
		}
	}
}

// Init establishes Borders, padding and instantiates
// Pixelmap with usable space
func (bi *DivBox) Init() {
	Loggo.Info("initializing box", "BorderChar", bi.BorderChar)
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
