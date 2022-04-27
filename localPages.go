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

func buildBookmarks(width, height int, s *ugglyBrowserSettings) *pb.PageResponse {
	theme := genMenuTheme()
	localPage := &pb.PageResponse{
		Name:     "uggcli-bookmarks",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	divStartX := uggo.Percent(15, width)
	divStartY := uggo.Percent(15, height)
	divWidth := int32(width) - (2 * divStartX)
	divHeight := int32(height) - (2 * divStartY)
	divName := "bookmarks-outer"
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes,
		theme.StylizeDivBox(&pb.DivBox{
			Name:   divName,
			Border: true,
			StartX: divStartX,
			StartY: divStartY,
			Width:  divWidth,
			Height: divHeight,
		}))
	msg := fmt.Sprintf("Bookmarks Browser\n\n")
	for i, bm := range s.Bookmarks {
		if i > len(uggo.StrokeMap) - 1 {
			break
		}
		stroke := uggo.StrokeMap[i]
		msg += fmt.Sprintf("(%s) -- %s: %s\n\n",
			stroke, *bm.ShortName, *bm.Ugri)
		link, err := linkFromString(*bm.Ugri)
		if err != nil {
			loggo.Debug("error generating Link from bookmark",
				"error", err.Error(),
				"Ugri", *bm.Ugri)
		}
		localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
			KeyStroke: stroke,
			Action: &pb.KeyStroke_Link{
				Link: link,
		}})
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs,
		theme.StylizeTextBlob(&pb.TextBlob{
			Content:  msg,
			Wrap:     true,
			DivNames: []string{divName},
		}))
	return localPage
}

func buildSettings(width, height int, s *ugglyBrowserSettings, infoMsg string) *pb.PageResponse {
	theme := genMenuTheme()
	uggo.ThemeDefault = theme
	localPage := &pb.PageResponse{
		Name:     "uggcli-settings",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	divStartX := uggo.Percent(5, width)
	divStartY := uggo.Percent(5, height)
	divWidth := int32(width) - (2 * divStartX)
	divHeight := int32(height) - (2 * divStartY)
	divName := "settings-outer"
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes,
		theme.StylizeDivBox(&pb.DivBox{
			Name:   divName,
			Border: true,
			StartX: divStartX,
			StartY: divStartY,
			Width:  divWidth,
			Height: divHeight,
		}))
	keyStroke := "j"
	submitPage := "applySettings"
	formName := "uggcli-settings"
	tbWidth := uggo.Percent(20, int(divWidth))
	tbPosX := int32(30)
	settingsForm := pb.Form{
		Name:    formName,
		DivName: divName,
		SubmitLink: &pb.Link{
			PageName: submitPage,
		},
		TextBoxes: []*pb.TextBox{
			theme.StylizeTextBox(&pb.TextBox{
				Name:            "VaultPassEnvVar",
				TabOrder:        1,
				DefaultValue:    *s.VaultPassEnvVar,
				Description:     "Cookie Vault ENV var",
				PositionX:       tbPosX,
				PositionY:       divStartY + 4,
				Height:          1,
				Width:           tbWidth,
				ShowDescription: true}),

			theme.StylizeTextBox(&pb.TextBox{
				Name:            "VaultFile",
				TabOrder:        2,
				DefaultValue:    *s.VaultFile,
				Description:     "Cookie Vault file path",
				PositionX:       tbPosX,
				PositionY:       divStartY + 6,
				Height:          1,
				Width:           tbWidth,
				ShowDescription: true}),
		}}
	divCenter := divStartX + uggo.Percent(50, int(divWidth))
	bmDivX := divStartX+divCenter
	bmDivY := divStartY+2
	bmDivHeight := divHeight - 4
	bmDivWidth := uggo.Percent(40, int(divWidth)) + 6
	bmDiv := theme.StylizeDivBox(&pb.DivBox{
		Name:   "bookmarks",
		Border: true,
		StartX: bmDivX,
		StartY: bmDivY,
		Width:  bmDivWidth,
		Height: bmDivHeight,
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, bmDiv)
	tabOrder := int32(3)
	bmTbWidthSn := uggo.Percent(20, int(bmDivWidth))
	bmTbWidthUg := uggo.Percent(65, int(bmDivWidth))
	tbPosX1 := divCenter + 2
	tbPosX2 := tbPosX1+bmTbWidthSn+3
	tbPosY2 := divStartY + 2
	colShort := "Short Name"
	colUgri := "UGRI"
	colDel := "del\n\n"
	colShortX := int(tbPosX1 + divStartX)
	colY := int(tbPosY2 + divStartY) + 1
	colUgriX := int(tbPosX2 + divStartX)
	colDelX := int(colUgriX) + int(bmTbWidthUg) + 1
	localPage = uggo.AddTextAt(
		localPage, colShortX, colY, len(colShort), 1, colShort)
	localPage = uggo.AddTextAt(
		localPage, colUgriX, colY, len(colUgri), 1, colUgri)
	for i, bm := range s.Bookmarks {
		if i > len(uggo.StrokeMap) - 1 {
			break // bail if we have more bookmarks than strokes
		}
		tbPosY2+=2
		bmNameUgri := fmt.Sprintf("bookmark_ugri_%d", *bm.uid)
		bmNameShortName := fmt.Sprintf("bookmark_shortname_%d", *bm.uid)
		loggo.Debug("adding bookmark to settings form",
			"bm.Ugri", bm.Ugri,
			"i", i,
			"bmNameUgri", bmNameUgri,
			"bm.uid", bm.uid)
		settingsForm.TextBoxes = append(settingsForm.TextBoxes,
				theme.StylizeTextBox(&pb.TextBox{
			Name:            bmNameShortName,
			TabOrder:        tabOrder,
			DefaultValue:    *bm.ShortName,
			PositionX:       tbPosX1,
			PositionY:       tbPosY2,
			Height:          1,
			Width:           bmTbWidthSn,
			ShowDescription: false}))
		tabOrder++
		settingsForm.TextBoxes = append(settingsForm.TextBoxes,
				theme.StylizeTextBox(&pb.TextBox{
			Name:            bmNameUgri,
			TabOrder:        tabOrder,
			DefaultValue:    *bm.Ugri,
			PositionX:       tbPosX2,
			PositionY:       tbPosY2,
			Height:          1,
			Width:           bmTbWidthUg,
			ShowDescription: false}))
		tabOrder++
		// now add the link and text to the del columb's textblob
		stroke := uggo.StrokeMap[i]
		colDel += fmt.Sprintf("(%s)\n\n", stroke)
		localAuthUuid = uggo.NewUuid()
		delPage := fmt.Sprintf("bookmark_delete_%d_%s", *bm.uid, localAuthUuid)
		delPageLink := pb.Link{ PageName: delPage }
		localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
			KeyStroke: stroke,
			Action: &pb.KeyStroke_Link{
				Link: &delPageLink,
		}})
	}
	localPage = uggo.AddTextAt(
		localPage, colDelX, colY, 4, height, colDel)
	localPage.Elements.Forms = append(localPage.Elements.Forms, &settingsForm)
	localPage.KeyStrokes = append(localPage.KeyStrokes, &pb.KeyStroke{
		KeyStroke: keyStroke,
		Action: &pb.KeyStroke_FormActivation{
			FormActivation: &pb.FormActivation{
				FormName: formName,
			}}})
	msg := fmt.Sprintf("Settings - Hit (%s) to activate form\n" +
		"Then Enter to submit", keyStroke)
	if infoMsg != "" { // maens we got sent back so we need to give the user info
		msg += fmt.Sprintf("\n\n%s", infoMsg)
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs,
		theme.StylizeTextBlob(&pb.TextBlob{
			Content:  msg,
			Wrap:     true,
			DivNames: []string{divName},
		}))
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs,
		theme.StylizeTextBlob(&pb.TextBlob{
			Content:  "Bookmarks:",
			Wrap:     true,
			DivNames: []string{"bookmarks"},
		}))
	bmDiv.FillSt = uggo.Style("black","cornsilk")
	return localPage
}

// things that are expecting to have local pages
// handle sensitive actions can set this so the client
// can verify that they indeed came from a local source
// and not someone trying to forge a link
var localAuthUuid string

func genMenuTheme() *uggo.Theme {
	return &uggo.Theme{
		StyleTextBoxDescription: uggo.Style("black", "navajowhite"),
		StyleTextBoxCursor:      uggo.Style("black", "white"),
		StyleTextBoxText:        uggo.Style("white", "darkblue"),
		StyleTextBoxFill:        uggo.Style("white", "darkblue"),
		StyleDivFill:            uggo.Style("white", "navajowhite"),
		StyleDivBorder:          uggo.Style("white", "black"),
		StyleTextBlob:           uggo.Style("white", "black"),
		DivBorderWidth:          int32(1),
		DivBorderChar:           uggo.ConvertStringCharRune("="),
		DivFillChar:             uggo.ConvertStringCharRune(""),
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
		"uggcli-menu v%s === "+
			"  ColorDemo (F2)"+
			"  Settings (F3)"+
			"  Browse Feed (F4)"+
			"  Refresh (F5)"+
			"  Bookmarks (F6)"+
			"  AddBookmark (F7)"+
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
