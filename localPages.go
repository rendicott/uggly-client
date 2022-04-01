package main

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	pb "github.com/rendicott/uggly"
)

func shelp(fg, bg string) *pb.Style {
	return &pb.Style{
		Fg:   fg,
		Bg:   bg,
		Attr: "4",
	}
}

func buildFeedBrowser(width int, links []*pb.Link) *pb.PageResponse {
	height := 36
	localPage := pb.PageResponse{
		Name:     "uggcli-feedbrowser",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	menuBar := pb.DivBox{
		Name:     "uggcli-feedbrowser-list",
		Border:   false,
		FillChar: convertStringCharRune("X"),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height),
		FillSt:   shelp("grey", "black"),
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &menuBar)
	contentString := ""
	for _, l := range links {
		contentString += fmt.Sprintf("(%s) %s\n", l.KeyStroke, l.PageName)
		// need to build
		localPage.Links = append(localPage.Links, l)
	}
	feedBrowserContent := pb.TextBlob{
		Content:  contentString,
		Wrap:     true,
		Style:    shelp("white", "black"),
		DivNames: []string{"uggcli-feedbrowser-list"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &feedBrowserContent)
	return &localPage
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
		FillChar: convertStringCharRune("."),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height),
		FillSt:    shelp("grey", "black"),
	})
	statusText := pb.TextBlob{
		Content:  message,
		Wrap:     true,
		Style:    shelp("white", "black"),
		DivNames: []string{"uggcli-status"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &statusText)
	return &localPage
}

// buildPageMenu takes some dimensions as input and generates an uggly.PageResponse
// which can then be easily rendered back in the browser just like a server
// response would be.
func buildPageMenu(width, height int, server, port, page, msg string) *pb.PageResponse {
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
		FillChar: convertStringCharRune(" "),
		StartX:   0,
		StartY:   0,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:    shelp("black", "black"),
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-addrbar",
		Border:   false,
		FillChar: convertStringCharRune(" "),
		StartX:   0,
		StartY:   1,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:    shelp("white", "black"),
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-statusbar",
		Border:   false,
		FillChar: convertStringCharRune(" "),
		StartX:   0,
		StartY:   2,
		Width:    int32(width),
		Height:   int32(height) / 3,
		FillSt:    shelp("white", "white"),
	})
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content:  "uggcli-menu ===  Browse Feed (F4)  ColorDemo (F2)   Refresh (F5)    Exit (F12)",
		Wrap:     true,
		Style:    shelp("white", "black"),
		DivNames: []string{"uggcli-menu"},
	})
	localPage.Elements.Forms = append(localPage.Elements.Forms, &pb.Form{
		Name:    "address-bar",
		DivName: "uggcli-addrbar",
		TextBoxes: []*pb.TextBox{&pb.TextBox{
			Name:             "connstring",
			TabOrder:         int32(0),
			DefaultValue:     fmt.Sprintf("%s:%s/%s", server, port, page),
			Description:      "Host: (F1)",
			PositionX:        int32(14),
			PositionY:        int32(1),
			Height:           int32(1),
			Width:            int32(width/2),
			StyleCursor:      shelp("black", "olive"),
			StyleFill:        shelp("white", "navy"),
			StyleText:        shelp("white", "navy"),
			StyleDescription: shelp("white", "black"),
			ShowDescription:  true,
		}},
		SubmitLink: &pb.Link{ // will be built by submission handler
			KeyStroke: "F5",
			PageName:  "REFRESH",
			Server:    "",
			Port:      "0",
		},
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
		Style:    shelp("black", "white"),
		DivNames: []string{"uggcli-statusbar"},
	})
	localPage.Links = append(localPage.Links, &pb.Link{
		KeyStroke: "F4",
		PageName:  "FEEDBROWSER",
		Server:    "",
		Port:      "0",
	})
	localPage.Links = append(localPage.Links, &pb.Link{
		KeyStroke: "F5",
		PageName:  "REFRESH",
		Server:    "",
		Port:      "0",
	})
	localPage.Links = append(localPage.Links, &pb.Link{
		KeyStroke: "F2",
		PageName:  "COLORDEMO",
		Server:    "",
		Port:      "0",
	})
	localPage.Links = append(localPage.Links, &pb.Link{
		KeyStroke: "F1",
		FormName: "address-bar",
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
				FillChar: convertStringCharRune(""),
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
					Style:    shelp("white", "black"),
					DivNames: []string{divName},
				})
			colorIndex++
		}
	}
	loggo.Info("buildColorDemo", "wroteRows", wroteRows, "wroteCols", wroteCols)
	return &localPage
}
