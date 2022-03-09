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
	logPane  = flag.Bool("log-pane", false, "whether or not to include a client logging pane for debugging")
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
func convertStringCharRune(s string) int32 {
	if len(s) == 0 {
		s = " "
	}
	runes := []rune(s)
	return runes[0]
}


func injectMenu() (boxesWithMenu []*boxes.DivBox, err error) {
	// makes boxes for the uggcli menu top bar
	screenWidth, _ := screen.Size() // returns width, height
	menuHeight := 2
	localPage := buildMenuPage(screenWidth, menuHeight)
	parseLinks(localPage, true) // retain links when injecting Menu
	if err != nil {
		return boxesWithMenu, err
	}
	boxesWithMenu, _ = compileBoxes(localPage)
	// shift all boxes down the height of the menu and add to final slice
	// from the global boxes
	for _, bi := range gBoxes {
		bi.StartY += menuHeight
		boxesWithMenu = append(boxesWithMenu, bi)
	}
	return boxesWithMenu, err
}

func makeboxes() {
	for _, bi := range gBoxes {
		for i := 0; i < bi.Width; i++ {
			for j := 0; j < bi.Height; j++ {
				x := bi.StartX + i
				y := bi.StartY + j
				screen.SetContent(
					x,
					y,
					bi.RawContents[i][j].C,
					nil,
					bi.RawContents[i][j].St,
				)
			}
		}
	}
	screen.Show()
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

func initScreen() (s tcell.Screen, err error) {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	s, err = tcell.NewScreen()
	if err != nil {
		return s, err
	}
	err = s.Init()
	if err != nil {
		return s, err
	}
	s.SetStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorBlack))
	s.Clear()
	return s, err
}

func get(l *link) {
	loggo.Info("getting link", "connString", l.connString, "pageName", l.pageName)
	page, err := sess.directDial(l.server, l.port, l.pageName)
	handle(err)
	err = renderPage(page)
	handle(err)
}

func getLocal(page *pb.PageResponse) {
	err := renderPage(page)
	handle(err)
}

func detectSpecialKey(ev *tcell.EventKey) (isSpecial bool, keyName string) {
	for k, v := range tcell.KeyNames {
		if ev.Key() == k {
			isSpecial = true
			keyName = v
		}
	}
	return isSpecial, keyName
}

func handleLinks(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyRune {
		loggo.Info("detected keypress", "key", string(ev.Rune()))
	} else {
		_, name := detectSpecialKey(ev)
		loggo.Info("detected keypress", "key", name)
	}
	loggo.Info("checking activeLinks for expected keypresses", "numLinks", len(activeLinks))
	for _, l := range activeLinks {
		loggo.Info("checking link", "expectedKey", l.keyStroke)
		// see if we can detect a special
		for k, v := range tcell.KeyNames {
			if v == l.keyStroke {
				if ev.Key() == k {
					get(l)
				}
			}
		}
		// if not special then maybe a rune
		if ev.Key() == tcell.KeyRune {
			if l.keyStroke == string(ev.Rune()) {
				get(l)
			}
		}
	}
}

func pollEvents() {
	for {
		loggo.Info("polling and watching for links", "links", len(activeLinks))
		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyF12:
				close(quit)
				return
			case tcell.KeyCtrlL:
				screen.Sync()
			case tcell.KeyF1:
				getLocal(buildFeedBrowser())
			default:
				handleLinks(ev)
			}
		case *tcell.EventResize:
			screen.Sync()
		case fakeEvent:
			loggo.Info("reloading links", "numLinks", len(activeLinks))
		}
	}
}

type link struct {
	keyStroke  string
	pageName   string
	server     string
	port       string
	connString string
}

var activeLinks []*link

type fakeEvent struct{}

func (f fakeEvent) When() time.Time {
	return time.Now()
}

func parseLinks(page *pb.PageResponse, retain bool) {
	if !retain { // clear all the links
		activeLinks = []*link{}
		loggo.Info("purged links")
	}
	for _, l := range page.Links {
		var tempLink link
		tempLink.keyStroke = l.KeyStroke
		tempLink.pageName = l.PageName
		tempLink.server = l.Server
		tempLink.port = l.Port
		tempLink.connString = fmt.Sprintf("%s:%s", tempLink.server, tempLink.port)
		activeLinks = append(activeLinks, &tempLink)
	}
	screen.PostEvent(fakeEvent{})
	for _, l := range(activeLinks) {
		loggo.Info("added link to activeLinks", "pageName", l.pageName, "connString", l.connString)
	}
}

var sess *session
var screen tcell.Screen
var gBoxes []*boxes.DivBox
var quit chan struct{}

func renderPage(page *pb.PageResponse) (err error) {
	gBoxes, err = compileBoxes(page)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		return err
	}
	parseLinks(page, false)
	screen.Clear()
	// always inject menu
	gBoxes, err = injectMenu()
	if err != nil {
		loggo.Error("error injecting menu", "err", err.Error())
		return err
	}
	// draw right away so there's no delay to user
	makeboxes()
	return err
}

func main() {
	flag.Parse()
	// daemonFlag = false
	setLogger(daemonFlag, logFile, *logLevel)
	boxes.Loggo = loggo
	ugcon.Loggo = loggo
	var err error
	// set up a break channel for monitoring exit keystrokes
	quit = make(chan struct{})
	screen, err = initScreen()
	if err != nil {
		loggo.Error("error initiatiizing screen", "err", err.Error())
		os.Exit(1)
	}
	defer screen.Fini()
	// set up rpc client
	sess = newSession()
	page, err := sess.directDial(*host, *port, "fancy")
	if err != nil {
		loggo.Error("getting page failed", "err", err.Error())
		os.Exit(1)
	}
	defer sess.conn.Close()
	go pollEvents()
	err = renderPage(page)
	if err != nil {
		loggo.Error("error rendering page", "err", err.Error())
		os.Exit(1)
	}
drawloop:
	for {
		select {
		case <-quit:
			break drawloop
		// redraw every 2 seconds for resizing
		case <-time.After(time.Millisecond * 2000):
		}
		makeboxes()
	}
}
