package boxes

import(
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
)

// Loggo is the global logger
var Loggo log15.Logger

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
    bi.fillX2 = bi.Width-bi.BorderW
    bi.fillY1 = bi.BorderW
    bi.fillY2 = bi.Height-bi.BorderW
	// initialize Pixelmap
    bi.RawContents = make([][]*Pixel, bi.Width)
    for i := range(bi.RawContents) {
        bi.RawContents[i] = make([]*Pixel, bi.Height)
    }
    // fill with Borderchar or blanks
	bi.BorderSt = tcell.StyleDefault
	for i := 0; i < bi.Width; i++ {
		for j := 0; j < bi.Height; j++ {
			var p Pixel
			// fill the whole thing with Border for now
			if bi.Border {
				p = Pixel{
					C:  bi.BorderChar,
					St: bi.BorderSt,
                    IsBorder: true,
				}
			} else {
				p = Pixel{
					C:  bi.FillChar,
					St: tcell.StyleDefault,
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
                C:  bi.FillChar,
                St: tcell.StyleDefault,
                IsBorder: false,
            }
            bi.RawContents[i][j] = &p
        }
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
	Border      bool
	BorderW     int
	BorderChar  rune
	BorderSt    tcell.Style
	FillChar    rune
	StartX      int
	StartY      int
	Width       int
	Height      int
	RawContents [][]*Pixel
    // unexported fields
    // usable fill space minus Border
    fillX1       int
    fillX2       int
    fillY1       int
    fillY2       int
    fillWidth    int
    fillHeight   int
}

