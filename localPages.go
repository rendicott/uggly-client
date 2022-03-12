package main

import (
	pb "github.com/rendicott/uggly"
	"fmt"
)

func buildFeedBrowser(width int, links []*pb.Link) *pb.PageResponse {
	height := 36
	localPage := pb.PageResponse{
		Name: "uggcli-feedbrowser",
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
		FillSt: &pb.Style{
			Fg:   "grey",
			Bg:   "black",
			Attr: "4",
		},
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &menuBar)
	contentString := ""
	for _, l := range(links) {
		contentString += fmt.Sprintf("(%s) %s\n", l.KeyStroke, l.PageName)
		// need to build 
		localPage.Links = append(localPage.Links, l)
	}
	feedBrowserContent := pb.TextBlob{
		Content: contentString,
		Wrap:    true,
		Style: &pb.Style{
			Fg:   "white",
			Bg:   "black",
			Attr: "4",
		},
		DivNames: []string{"uggcli-feedbrowser-list"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &feedBrowserContent)
	return &localPage
}

func buildStatus(message string, width, height int) *pb.PageResponse {
	localPage := pb.PageResponse{
		Name: "uggcli-status",
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
		FillSt: &pb.Style{
			Fg:   "grey",
			Bg:   "black",
			Attr: "4",
		},
	})
	statusText := pb.TextBlob{
		Content: message,
		Wrap:    true,
		Style: &pb.Style{
			Fg:   "white",
			Bg:   "black",
			Attr: "4",
		},
		DivNames: []string{"uggcli-status"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &statusText)
	return &localPage
}


// buildPageMenu takes some dimensions as input and generates an uggly.PageResponse
// which can then be easily rendered back in the browser just like a server
// response would be.
func buildPageMenu(width, height int, server, port, page string) *pb.PageResponse {
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
		Height:   int32(height)/3,
		FillSt: &pb.Style{
			Fg:   "black",
			Bg:   "black",
			Attr: "4",
		},
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-addrbar",
		Border:   false,
		FillChar: convertStringCharRune(" "),
		StartX:   2,
		StartY:   1,
		Width:    int32(width),
		Height:   int32(height)/3,
		FillSt: &pb.Style{
			Fg:   "white",
			Bg:   "black",
			Attr: "4",
		},
	})
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &pb.DivBox{
		Name:     "uggcli-statusbar",
		Border:   false,
		FillChar: convertStringCharRune(" "),
		StartX:   2,
		StartY:   2,
		Width:    int32(width),
		Height:   int32(height)/3,
		FillSt: &pb.Style{
			Fg:   "white",
			Bg:   "black",
			Attr: "4",
		},
	})
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content: "uggcli-menu ===  Browse Feed (F1)   Exit (F12)",
		Wrap:    true,
		Style: &pb.Style{
			Fg:   "white",
			Bg:   "black",
			Attr: "4",
		},
		DivNames: []string{"uggcli-menu"},
	})
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content: fmt.Sprintf("Host: %s:%s/%s", server, port, page),
		Wrap:    true,
		Style: &pb.Style{
			Fg:   "white",
			Bg:   "green",
			Attr: "4",
		},
		DivNames: []string{"uggcli-addrbar"},
	})
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &pb.TextBlob{
		Content: "hi",
		Wrap:    true,
		Style: &pb.Style{
			Fg:   "white",
			Bg:   "green",
			Attr: "4",
		},
		DivNames: []string{"uggcli-statusbar"},
	})
	linkBrowseFeed := &pb.Link{
		KeyStroke: "F1",
		PageName:  "FEEDBROWSER",
		Server:    "",
		Port:      "0",
	}
	localPage.Links = append(localPage.Links, linkBrowseFeed)
	return &localPage
}
