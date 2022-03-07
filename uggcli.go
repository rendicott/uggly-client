package main

import (
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"os"
	"errors"
	"time"
)

// loggo is the global logger
var loggo log15.Logger

// setLogger sets up logging globally for the packages involved
// in the gossamer runtime.
func setLogger(daemonFlag bool, logFileS, loglevel string) {
	loggo = log15.New()
	if daemonFlag && loglevel == "debug" {
		loggo.SetHandler(
			log15.LvlFilterHandler(
				log15.LvlDebug,
				log15.Must.FileHandler(logFileS, log15.JsonFormat())))
	} else if daemonFlag && loglevel == "info" {
		loggo.SetHandler(
			log15.LvlFilterHandler(
				log15.LvlInfo,
				log15.Must.FileHandler(logFileS, log15.JsonFormat())))
	} else if loglevel == "debug" && !daemonFlag {
		// log to stdout and file
		loggo.SetHandler(log15.MultiHandler(
			log15.StreamHandler(os.Stdout, log15.LogfmtFormat()),
			log15.LvlFilterHandler(
				log15.LvlDebug,
				log15.Must.FileHandler(logFileS, log15.JsonFormat()))))
	} else {
		// log to stdout and file
		loggo.SetHandler(log15.MultiHandler(
			log15.LvlFilterHandler(
				log15.LvlInfo,
				log15.StreamHandler(os.Stdout, log15.LogfmtFormat())),
			log15.LvlFilterHandler(
				log15.LvlInfo,
				log15.Must.FileHandler(logFileS, log15.JsonFormat()))))
	}
}

var (
	daemonFlag = true
	logFile    = "uggcli.log.json"
	//logLevel   = "info"
	logLevel = flag.String("loglevel", "info", "log level 'info' or 'debug'")
	host     = flag.String("host", "localhost", "the host to connect to")
	port     = flag.String("port", "443", "the port to connect to")
	logPane = flag.Bool("log-pane", false, "whether or not to include a client logging pane for debugging")
)

func handle(err error) {
	if err != nil {
		loggo.Error("generic error", "error", err.Error())
		os.Exit(1)
	}
}

func sleep() {
	time.Sleep(10 * time.Millisecond)
}


// convertStringCharRune takes a string and converts it to a rune slice
// then grabs the rune at index 0 in the slice so that it can return 
// an int32 to satisfy the Uggly protobuf struct for border and fill chars
// and such. If the input string is less than zero length then it will just 
// rune out a space char and return that int32. 
func convertStringCharRune(s string) (int32) {
	if len(s) == 0 {
		s = " "
	}
	runes := []rune(s)
	return runes[0]
}

func buildMenuPage(width, height int) (*pb.PageResponse) {
	// since we already have functions for converting to divboxes
	// we'll just build a local pageResponse 
	localPage := pb.PageResponse{
		Name: "uggcli-menu",
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{},
	}
	menuBar := pb.DivBox{
		Name: "uggcli-menu",
		Border: false,
		FillChar: convertStringCharRune(" "),
		StartX: 0,
		StartY: 0,
		Width: int32(width),
		Height: int32(height),
		FillSt: &pb.Style {
			Fg: "black",
			Bg: "black",
			Attr: "4",
		},
	}
	localPage.DivBoxes.Boxes = append(localPage.DivBoxes.Boxes, &menuBar)
	menuContent := pb.TextBlob{
		Content: "uggcli-menu ===  Browse (F1)   Exit (F12)",
		Wrap: true,
		Style: &pb.Style{
			Fg: "white",
			Bg: "black",
			Attr: "4",
		},
		DivNames: []string{"uggcli-menu"},
	}
	localPage.Elements.TextBlobs = append(localPage.Elements.TextBlobs, &menuContent)
	linkBrowse := &pb.Link{
		KeyStroke: "F1",
		PageName: "basic",
		Server: "",
		Port: int32(10000),
	}
	localPage.Links = append(localPage.Links, linkBrowse)
	return &localPage
}

func injectMenu(screen tcell.Screen, bis []*boxes.DivBox) (boxesWithMenu []*boxes.DivBox, links []*link, err error) {
	// makes boxes for the uggcli menu top bar
	screenWidth, _ := screen.Size() // returns width, height
	menuHeight := 1
	localPage := buildMenuPage(screenWidth, menuHeight)
	links, err = parseLinks(localPage)
	if err != nil {
		return boxesWithMenu, links, err
	}
	boxesWithMenu, _ = compileBoxes(localPage)
	// shift all boxes down the height of the menu and add to final slice
	for _, bi := range(bis) {
		bi.StartY += menuHeight
		boxesWithMenu = append(boxesWithMenu, bi)
	}
	return boxesWithMenu, links, err
}

func makeboxes(s tcell.Screen, bis []*boxes.DivBox, quit chan struct{}) {
	for _, bi := range bis {
		for i := 0; i < bi.Width; i++ {
			for j := 0; j < bi.Height; j++ {
				x := bi.StartX + i
				y := bi.StartY + j
				s.SetContent(
					x,
					y,
					bi.RawContents[i][j].C,
					nil,
					bi.RawContents[i][j].St,
				)
			}
		}
	}
	s.Show()
}






func compileBoxes(page *pb.PageResponse) ([]*boxes.DivBox, error) {
	var myBoxes []*boxes.DivBox
	var err error
	for _, div := range page.DivBoxes.Boxes {
		// convert divboxes to local format
		b, err := ugcon.ConvertDivBoxLocalBoxes(div)
		if err != nil {
			return myBoxes, err
		}
		myBoxes = append(myBoxes, b)
	}
	// collect elements from page 
	for _, ele := range page.Elements.TextBlobs {
		// convert and mate textBlobs to boxes
		tb, err := ugcon.ConvertTextBlobLocalBoxes(ele)
		if err != nil {
			return myBoxes, err
		}
		fgcolor, _, _ := tb.Style.Decompose()
		tcolor := fgcolor.TrueColor()
		loggo.Debug("style after converstion",
			"fgcolor", tcolor, "page-name", page.Name,
		)
		if page.Name == "uggcli-menu" {
			loggo.Debug("got menu textblob", "content", ele.Content)
		}
		tb.MateBoxes(myBoxes)
	}
	for _, bi := range myBoxes {
		bi.Init()
	}
	return myBoxes, err
}

func initScreen() (tcell.Screen, error) {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	screen, err := tcell.NewScreen()
	if err != nil {
		return screen, err
	}
	err = screen.Init()
	if err != nil {
		return screen, err
	}
	screen.SetStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorBlack))
	screen.Clear()
	return screen, err
}


func pollEvents(screen tcell.Screen, links []*link, quitChan chan struct{}) {
	loggo.Info("got link list", "len", len(links))
	for {
		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyF12:
				close(quitChan)
				return
			case tcell.KeyCtrlL:
				screen.Sync()
			}
		case *tcell.EventResize:
			screen.Sync()
		}
	}
}

type link struct{
	keyStroke tcell.Key
	pageName string
	server string
	port int
	connString string
}

func parseLinks(page *pb.PageResponse) (links []*link, err error) {
	for _, l := range(page.Links) {
		var tempLink link
		tempLink.pageName = l.PageName
		tempLink.server = l.Server
		tempLink.port = int(l.Port)
		tempLink.connString = fmt.Sprintf("%s:%d", tempLink.server, tempLink.port)
		foundKey := false
		for key, name := range(tcell.KeyNames) {
			if l.KeyStroke == name {
				foundKey = true
				tempLink.keyStroke = key
			}
		}
		if !foundKey {
			msg := fmt.Sprintf("no keystroke could be mapped for keystring '%s'", l.KeyStroke)
			err = errors.New(msg)
			return links, err
		}
		loggo.Info("detected key for link", "string", l.KeyStroke)
		links = append(links, &tempLink)
	}
	return links, err
}

func main() {
	flag.Parse()
	// daemonFlag = false
	setLogger(daemonFlag, logFile, *logLevel)
	boxes.Loggo = loggo
	ugcon.Loggo = loggo
	screen, err := initScreen()
	if err != nil {
		loggo.Error("error intitializing screen", "err", err.Error())
		os.Exit(1)
	}
	defer screen.Fini()
	// set up rpc client
	s := newSession()
	// s.setServer(*host, *port)
	// err := s.getConnection()
	// //defer conn.Close() moved this off to struct
	// if err != nil {
	// 	loggo.Error("dialing server failed", "server", s.connString, "err", err.Error())
	// 	os.Exit(1)
	// }
	// err = s.browseFeed()
	// if err != nil {
	// 	loggo.Error("selecting feed failed", "err", err.Error())
	// 	os.Exit(1)
	// }
	// page, err := getPage(s)
	page, err := s.directDial(*host, *port, "fancy")
	if err != nil {
		loggo.Error("getting page failed", "err", err.Error())
		os.Exit(1)
	}
	defer s.conn.Close()
	// now convert server divs to boxes
	myBoxes, err := compileBoxes(page)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		os.Exit(1)
	}
	links, err := parseLinks(page)
	if err != nil {
		loggo.Error("error parsing links", "err", err.Error())
		os.Exit(1)
	}
	// always inject menu
	myBoxes, links, err = injectMenu(screen, myBoxes)
	if err != nil {
		loggo.Error("error injecting menu", "err", err.Error())
		os.Exit(1)
	}
	for _, ml := range(links) {
		links = append(links, ml)
	}
	// set up a break channel for monitoring exit keystrokes
	quit := make(chan struct{})
	go pollEvents(screen, links, quit)
	// draw right away so there's no delay to user
	makeboxes(screen, myBoxes, quit)
drawloop:
	for {
		select {
		case <-quit:
			break drawloop
		// redraw every 2 seconds for resizing
		case <-time.After(time.Millisecond * 2000):
		}
		makeboxes(screen, myBoxes, quit)
	}
}
