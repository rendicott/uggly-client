package main

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	pb "github.com/rendicott/uggly"
    "github.com/rendicott/uggo"
)

func buildFeedBrowser(width int, keyStrokes []*pb.KeyStroke) *pb.PageResponse {
	height := 36
	localPage := pb.PageResponse{
		Name:     "uggcli-feedbrowser",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	menuBar := pb.DivBox{
		Name:     "uggcli-feedbrowser-list",
		Border:   false,
		FillChar: uggo.ConvertStringCharRune("X"),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height),
		FillSt:   uggo.Style("grey", "black"),
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &menuBar)
	contentString := ""
	for _, k := range keyStrokes {
		switch x := k.Action.(type) {
		case *pb.KeyStroke_Link:
			contentString += fmt.Sprintf(
				"(%s) %s\n", k.KeyStroke, x.Link.PageName)
			localPage.KeyStrokes = append(localPage.KeyStrokes, k)
		}
	}
	feedBrowserContent := pb.TextBlob{
		Content:  contentString,
		Wrap:     true,
		Style:    uggo.Style("white", "black"),
		DivNames: []string{"uggcli-feedbrowser-list"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &feedBrowserContent)
	return &localPage
}

func buildSettings(width, height int, s *ugglyBrowserSettings) *pb.PageResponse {
    theme := genMenuTheme()
	localPage := pb.PageResponse{
		Name:     "settings",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
    divName := "settings-outer"
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes,
            theme.StylizeDivBox(&pb.DivBox{
		Name:     divName,
		StartX:   int32(width-(width/10)),
		StartY:   int32(height-(height/10)),
		Width:    int32(width-(width/10)),
		Height:   int32(height-(height/10)),
	}))
    keyStroke := "j"
    //submitPage := "applySettings"
    //formName := "settings"
    //settingsForm := pb.Form{
    //    Name: formName,
    //    DivName: divName,
    //    SubmitLink: &pb.Link{
    //        PageName: submitPage,
    //    },
    //    TextBoxes: []*pb.TextBox{
    //        theme.StylizeTextBox(&pb.TextBox{
    //            Name: "",
    //            TabOrder: 1,
    //            DefaultValue: "",
    //            Description: "",
    //            PositionX: 10,
    //            PositionY: 10,
    //            Height: 1,
    //            Width: 10,
    //            ShowDescription: true,
    //        }),
    //    },
    //}
    //localPage.Elements.Forms = append(localPage.Elements.Forms, &settingsForm)
	//localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
	//	KeyStroke: keyStroke,
	//	Action: &pb.KeyStroke_FormActivation{
	//		FormActivation: &pb.FormActivation{
	//			FormName: formName,
	//		}}})
	msg := fmt.Sprintf("Settings - Hit (%s) to activate form", keyStroke)
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs,
		theme.StylizeTextBlob(&pb.TextBlob{
			Content:  msg,
			Wrap:     true,
			DivNames: []string{divName},
		}))
	return &localPage
}

func genMenuTheme() (*uggo.Theme) {
    return &uggo.Theme{
		StyleTextBoxDescription: uggo.Style("white", "black"),
		StyleTextBoxCursor:      uggo.Style("black", "white"),
		StyleTextBoxText:        uggo.Style("white", "cyan"),
		StyleTextBoxFill:        uggo.Style("white", "cyan"),
		StyleDivFill:            uggo.Style("white", "black"),
		StyleDivBorder:          uggo.Style("white", "black"),
		StyleTextBlob:           uggo.Style("white", "black"),
		DivBorderWidth:          int32(1),
		DivBorderChar:           uggo.ConvertStringCharRune("="),
		DivFillChar:             uggo.ConvertStringCharRune("."),
    }
}

func buildStatus(message string, width, height int) *pb.PageResponse {
	localPage := pb.PageResponse{
		Name:     "uggcli-status",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-status",
		Border:   false,
		FillChar: uggo.ConvertStringCharRune("."),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height),
		FillSt:   uggo.Style("grey", "black"),
	})
	statusText := pb.TextBlob{
		Content:  message,
		Wrap:     true,
		Style:    uggo.Style("white", "black"),
		DivNames: []string{"uggcli-status"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &statusText)
	return &localPage
}

// buildPageMenu takes some dimensions as input and generates an uggly.PageResponse
// which can then be easily rendered back in the browser just like a server
// response would be.
func buildPageMenu(width, height int, server, port, page, msg string, secure bool) *pb.PageResponse {
	// since we already have functions for converting to divboxes
	// we'll just build a local pageResponse
	localPage := pb.PageResponse{
		Name:     "uggcli-menu",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-menu",
		Border:   false,
		FillChar: uggo.ConvertStringCharRune(" "),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:   uggo.Style("black", "black"),
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-addrbar",
		Border:   false,
		FillChar: uggo.ConvertStringCharRune(" "),
		StartX:   0,
		StartY:   1,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:   uggo.Style("white", "black"),
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-statusbar",
		Border:   false,
		FillChar: uggo.ConvertStringCharRune(" "),
		StartX:   0,
		StartY:   2,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:   uggo.Style("white", "white"),
	})
	menuText := fmt.Sprintf(
		"uggcli-menu v%s === " +
        "  ColorDemo (F2)" +
        "  Settings (F3)" +
        "  Browse Feed (F4)" +
        "  Refresh (F5)" +
        "  Exit (F10)",
		version)
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content:  menuText,
		Wrap:     true,
		Style:    uggo.Style("white", "black"),
		DivNames: []string{"uggcli-menu"},
	})
	addressDescriptionColor := uggo.Style("white", "red")
	addressPrefix := "ugtp://"
	if secure {
		addressPrefix = "ugtps://"
		addressDescriptionColor = uggo.Style("white", "green")
	}
	localPage.Elements.Forms = append(localPage.Elements.Forms, &pb.Form{
		Name:    "address-bar",
		DivName: "uggcli-addrbar",
		TextBoxes: []*pb.TextBox{&pb.TextBox{
			Name:     "connstring",
			TabOrder: int32(0),
			DefaultValue: fmt.Sprintf(
				"%s%s:%s/%s", addressPrefix, server, port, page),
			Description:      "Host: (F1)",
			PositionX:        int32(14),
			PositionY:        int32(0),
			Height:           int32(1),
			Width:            int32(width / 2),
			StyleCursor:      uggo.Style("black", "olive"),
			StyleFill:        uggo.Style("white", "navy"),
			StyleText:        uggo.Style("white", "navy"),
			StyleDescription: addressDescriptionColor,
			ShowDescription:  true,
		}},
		SubmitLink: &pb.Link{}, // will be built by submission handler
	})
	//localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
	//	Content: fmt.Sprintf("Host: %s:%s/%s", server, port, page),
	//	Wrap:    true,
	//	Style: &pb.Style{
	//		Fg:   "white",
	//		Bg:   "green",
	//		Attr: "4",
	//	},
	//	DivNames: []string{"uggcli-addrbar"},
	//})
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content:  msg,
		Wrap:     true,
		Style:    uggo.Style("black", "white"),
		DivNames: []string{"uggcli-statusbar"},
	})
	localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
		KeyStroke: "F4",
		Action: &pb.KeyStroke_Link{
			Link: &pb.Link{
				PageName: "FEEDBROWSER",
				Server:   "MENU",
				Port:     "0",
			},
		},
	})
	localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
		KeyStroke: "F5",
		Action: &pb.KeyStroke_Link{
			Link: &pb.Link{
				PageName: "REFRESH",
				Server:   "MENU",
				Port:     "0",
			},
		},
	})
	localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
		KeyStroke: "F2",
		Action: &pb.KeyStroke_Link{
			Link: &pb.Link{
				PageName: "COLORDEMO",
				Server:   "MENU",
				Port:     "0",
			},
		},
	})
	localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
		KeyStroke: "F1",
		Action: &pb.KeyStroke_FormActivation{
			FormActivation: &pb.FormActivation{
				FormName: "address-bar",
			},
		},
	})
	return &localPage
}

func buildColorDemo(width, height int) *pb.PageResponse {
	cellW := 22
	cellH := 4
	cols := width / cellW
	rows := height / cellH
	loggo.Info("buildColorDemo dimensions",
		"cellW", cellW,
		"cellH", cellH,
		"cols", cols,
		"rows", rows,
		"clientW", width,
		"clientH", height,
	)
	localPage := pb.PageResponse{
		Name:     "uggcli-colordemo",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	// convert colorName map to slice so we can grab by index
	var colors []string
	for colorName, _ := range tcell.ColorNames {
		colors = append(colors, colorName)
	}
	colorIndex := 0
	wroteCols := 0
	wroteRows := 0
	for i := 1; i < rows+1; i++ { // number of colums
		wroteCols++
		for j := 1; j < cols+1; j++ {
			loggo.Debug("colorGrab", "len(colors)", len(colors), "colorIndex", colorIndex)
			if colorIndex >= len(colors) {
				break
			}
			colorName := colors[colorIndex]
			wroteRows++
			divName := fmt.Sprintf("color-%s", colorName)
			localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
				Name:     divName,
				Border:   false,
				FillChar: uggo.ConvertStringCharRune(""),
				StartX:   int32(j*cellW - cellW),
				StartY:   int32(i*cellH - cellH),
				Width:    int32(cellW),
				Height:   int32(cellH),
				FillSt: &pb.Style{
					Fg:   "",
					Bg:   colorName,
					Attr: "4",
				},
			})
			localPage.Elements.TextBlobs = append(
				localPage.Elements.TextBlobs, &pb.TextBlob{
					Content:  fmt.Sprintf("(%d/%d)\n%s", colorIndex+1, len(colors), colorName),
					Wrap:     true,
					Style:    uggo.Style("white", "black"),
					DivNames: []string{divName},
				})
			colorIndex++
		}
	}
	loggo.Info("buildColorDemo", "wroteRows", wroteRows, "wroteCols", wroteCols)
	return &localPage
}
