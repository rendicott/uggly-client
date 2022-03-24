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
	"sync"
	"time"
)

var (
	daemonFlag = true
	logFile    = "uggcli.log.json"
	//logLevel   = "info"
	logLevel = flag.String("loglevel", "info", "log level 'info' or 'debug'")
	host     = flag.String("host", "localhost", "the host to connect to")
	port     = flag.String("port", "443", "the port to connect to")
	page     = flag.String("page", "home", "the page to connect to, if page is unavailable then client will browse feed instead")
	logPane  = flag.Bool("log-pane", false, "whether or not to include a client logging pane for debugging")
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

// convertPageBoxes converts an uggly.PageResponse into a boxes.DivBox format
// which can then be set as content to be drawn later
func convertPageBoxes(page *pb.PageResponse) (myBoxes []*boxes.DivBox, err error) {
	if page == nil {
		return myBoxes, err
	}
	if page.DivBoxes == nil {
		return myBoxes, err
	}
	if page.DivBoxes.Boxes == nil {
		return myBoxes, err
	}
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
		loggo.Debug("build boxes.TextBlob", "tb.Content", tb.Content)
		fgcolor, _, _ := tb.Style.Decompose()
		tcolor := fgcolor.TrueColor()
		loggo.Debug("style after conversion",
			"fgcolor", tcolor, "page-name", page.Name,
		)
		if page.Name == "uggcli-menu" {
			loggo.Debug("got menu textblob", "content", ele.Content)
		}
		tb.MateBoxes(myBoxes)
	}
	for _, bi := range myBoxes {
		loggo.Debug("calling divbox.Init()")
		bi.Init()
		if len(bi.RawContents) > 0 {
			loggo.Debug("divbox rawcontents first pixel", "pixel", bi.RawContents[0][0].C)
		}
	}
	return myBoxes, err
}

// handle is a lazy way of handling errors until they can be handled with
// more sophisticated methods
func handle(err error) {
	if err != nil {
		loggo.Error("generic error", "error", err.Error())
		os.Exit(1)
	}
}

// handle is a lazy way of handling generic errors within the browser
// context. Can help make more graceful exits by closing up screens,
// connections, etc.
func (b *ugglyBrowser) handle(err error) {
	if err != nil {
		loggo.Error("generic browser error", "error", err.Error())
		b.exit(1)
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
	return s, err
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

func (b *ugglyBrowser) buildContentMenu(label string) {
	// makes boxes for the uggcli menu top bar
	var msg string
	if len(b.messages) > 0 {
		msg = *b.messages[len(b.messages)-1]
	} else {
		msg = ""
	}
	localPage := buildPageMenu(
		b.vW, b.menuHeight, b.sess.server, b.sess.port, b.sess.currPage, msg)
	b.parseLinks(localPage, true) // retain links when injecting Menu
	var err error
	b.contentMenu, err = convertPageBoxes(localPage)
	if err != nil {
		loggo.Error("buildContentMenu convertPageBoxes error", "err", err.Error())
		return
	}
	loggo.Debug("sending viewTrigger")
	select {
	case <-b.interrupt:
		return
	default:
		b.drawContent()
	}
}

// menuWatch always watches the message buffer for messages
// and redraws the menu when it gets user facing messages
func (b *ugglyBrowser) menuWatch() {
	for {
		select {
		case <-b.interrupt:
			return
		default:
			msg := <-b.messageBuffer
			b.messages = append(b.messages, &msg)
			b.buildContentMenu("messageBuffer")
		}
	}
}

// sendMessage can be used to add a message to the buffer and
// can be called a goroutine for lazy message sending
func (b *ugglyBrowser) sendMessage(msg, label string) {
	b.messageBuffer <- msg
}

func (b *ugglyBrowser) get(l *link) {
	dest := fmt.Sprintf("%s:%s", l.server, l.port)
	loggo.Info("getting link", "connString", dest, "pageName", l.pageName)
	var err error
	// set dimensions so it can be sent by session
	b.sess.clientWidth = int64(b.vW)
	b.sess.clientHeight = int64(b.vH)
	b.sendMessage(fmt.Sprintf("dialing server '%s'...", dest), "getLink-preDial")
	b.currentPage, err = b.sess.directDial(l.server, l.port, l.pageName)
	if err != nil {
		if err.Error() == "context deadline exceeded" {
			b.sendMessage(
				fmt.Sprintf("connection timeout to '%s'", dest), "getLink-timeout")
			return
		}
		if err.Error() == "error getting page from server" {
			msg := fmt.Sprintf("error getting page '%s' from server", l.pageName)
			b.sendMessage(msg, "getLink-notfound")
			loggo.Error(msg)
		} else {
			b.handle(err)
		}
	} else {
		b.sendMessage("connected!", "getLink-success")
		b.currentPageLocal = nil // so refresh knows to get external
		b.handle(b.buildDraw("getLink"))
	}
}

func (b *ugglyBrowser) handleLinks(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyRune {
		loggo.Info("detected keypress", "key", string(ev.Rune()))
	} else {
		_, name := detectSpecialKey(ev)
		loggo.Info("detected keypress", "key", name)
	}
	loggo.Info("checking activeLinks for expected keypresses", "numLinks", len(b.activeLinks))
	for _, l := range b.activeLinks {
		loggo.Info("checking link", "expectedKey", l.keyStroke)
		// see if we can detect a special
		for k, v := range tcell.KeyNames {
			if v == l.keyStroke {
				if ev.Key() == k {
					b.get(l)
				}
			}
		}
		// if not special then maybe a rune
		if ev.Key() == tcell.KeyRune {
			if l.keyStroke == string(ev.Rune()) {
				b.get(l)
			}
		}
	}
}

func (b *ugglyBrowser) colorDemo() {
	thisfunc := "colorDemo"
	b.currentPage = buildColorDemo(b.vW, b.vH)
	b.currentPageLocal = b.currentPage
	go b.sendMessage("locally generated color demo to show tcell color capabilities on this TTY", thisfunc)
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) getFeed() {
	thisfunc := "geedFeed"
	feedErrMsg := "no server connection"
	feedErrMsgNoFeed := "server provides no feed"
	loggo.Info("getting feed")
	links, err := b.sess.feedLinks()
	if err != nil {
		if err.Error() == feedErrMsg {
			msg := "unable to connect to server"
			b.sendMessage(msg, thisfunc)
		} else if err.Error() == feedErrMsgNoFeed {
			b.sendMessage(feedErrMsgNoFeed, thisfunc)
		} else {
			b.handle(err)
		}
	} else {
		loggo.Info("building feed")
		b.currentPage = buildFeedBrowser(b.vW, links)
		b.currentPageLocal = b.currentPage
	}
	// regardless, redraw
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) exit(code int) {
	loggo.Info("caught exit interrupt", "code", code)
	b.exitFlag = true // in case other go routines are watching
	close(b.interrupt)
	close(b.messageBuffer)
	b.view.Fini()
	os.Exit(code)
}

func (b *ugglyBrowser) refresh() {
	if b.currentPageLocal == nil {
		startLink := link{
			server:   b.sess.server,
			port:     b.sess.port,
			pageName: b.sess.currPage,
		}
		loggo.Info("refreshing page from server")
		b.get(&startLink)
	} else if b.currentPageLocal != nil {
		if b.currentPageLocal.Name == "uggcli-colordemo" {
			b.colorDemo()
		}
		if b.currentPageLocal.Name == "uggcli-feedbrowser" {
			b.getFeed()
		}
	}
}

func (b *ugglyBrowser) pollEvents() {
	for {
		loggo.Info("polling and watching for links", "links", len(b.activeLinks))
		ev := b.view.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyF12:
				b.exit(0)
				return
			case tcell.KeyCtrlL:
				b.view.Sync()
			case tcell.KeyF1:
				b.getFeed()
			case tcell.KeyF2:
				b.colorDemo()
			case tcell.KeyF5:
				b.refresh()
			default:
				b.handleLinks(ev)
			}
		case *tcell.EventResize:
			b.view.Sync()
			if !b.resizing {
				go b.resizeHandler()
				b.resizeBuffer <- int(0)
			}
		case fakeEvent:
			loggo.Info("reloading links", "numLinks", len(b.activeLinks))
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

type fakeEvent struct{}

func (f fakeEvent) When() time.Time {
	return time.Now()
}

func (b *ugglyBrowser) updateAll() {
	label := "updateAll"
	b.buildContentMenu(label)
	b.handle(b.buildDraw(label))
}

func (b *ugglyBrowser) resizeHandler() {
	b.resizing = true
	<-b.resizeBuffer
	time.Sleep(b.resizeDelay)
	w, h := b.view.Size()
	b.sess.clientWidth = int64(w)
	b.sess.clientHeight = int64(h)
	b.vW = w
	b.vH = h
	b.refresh()
	//b.updateAll()
	b.resizing = false
}

func (b *ugglyBrowser) parseLinks(page *pb.PageResponse, retain bool) {
	if !retain { // clear all the links
		b.activeLinks = []*link{}
		loggo.Info("purged links")
	}
	if page == nil {
		return
	}
	if page.Links == nil {
		return
	}
	for _, l := range page.Links {
		var tempLink link
		tempLink.keyStroke = l.KeyStroke
		tempLink.pageName = l.PageName
		// if server didn't specify new host:port
		// we'll assume it's the current server
		if l.Server == "" {
			tempLink.server = b.sess.server
		} else {
			tempLink.server = l.Server
		}
		if l.Port == "" {
			tempLink.port = b.sess.port
		} else {
			tempLink.port = l.Port
		}
		tempLink.connString = fmt.Sprintf("%s:%s", tempLink.server, tempLink.port)
		b.activeLinks = append(b.activeLinks, &tempLink)
	}
	b.view.PostEvent(fakeEvent{})
	for _, l := range b.activeLinks {
		loggo.Info("added link to activeLinks", "pageName", l.pageName, "connString", l.connString)
	}
}

// buildDraw takes all of the currently set content in the browser
// and renders it then triggers a draw action
func (b *ugglyBrowser) buildDraw(label string) (err error) {
	b.contentExt, err = convertPageBoxes(b.currentPage)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		return err
	}
	b.parseLinks(b.currentPage, false)
	b.view.Clear()
	b.drawContent()
	return err
}

// drawContent concats the contents of contentMenu and contentExt
// then draws to screen
func (b *ugglyBrowser) drawContent() {
	if b.exitFlag {
		return
	}
	loggo.Debug("drawing content")
	content := make([]*boxes.DivBox, 0) // work with a local copy
	loggo.Debug("drawing menu content", "len", len(b.contentMenu))
	for _, mb := range b.contentMenu {
		content = append(content, mb)
	}
	// add external content to total content shifting it
	// down the height of the menu
	loggo.Debug("drawing ext content", "len", len(b.contentExt))
	for _, bi := range b.contentExt {
		// since we're modifying positioning lets make a local copy
		// so as not to modify the source content (4hr bug hunt!)
		var bj boxes.DivBox
		bj = *bi
		if bj.StartY < b.menuHeight { // make sure can't cover menu
			bj.StartY = 0
		}
		bj.StartY += b.menuHeight
		content = append(content, &bj)
	}
	loggo.Debug("drawing all content", "len", len(content))
	// now actually draw
	for _, bi := range content {
		for i := 0; i < bi.Width; i++ {
			for j := 0; j < bi.Height; j++ {
				x := bi.StartX + i
				y := bi.StartY + j
				b.view.SetContent(
					x,
					y,
					bi.RawContents[i][j].C,
					nil,
					bi.RawContents[i][j].St,
				)
			}
		}
	}
	b.view.Show()
}

type ugglyBrowser struct {
	view             tcell.Screen
	contentMenu      []*boxes.DivBox
	screenLock       sync.Mutex
	contentExt       []*boxes.DivBox
	currentPage      *pb.PageResponse
	currentPageLocal *pb.PageResponse // so we don't get from external
	interrupt        chan struct{}
	sess             *session
	messages         []*string
	messageBuffer    chan string
	resizeBuffer     chan int
	resizing         bool
	resizeDelay      time.Duration
	activeLinks      []*link
	menuHeight       int
	exitFlag         bool
	vH               int // view height (updates on resize event)
	vW               int // view width (updates on resize event)
}

func newBrowser() *ugglyBrowser {
	b := ugglyBrowser{}
	b.menuHeight = 3
	b.resizeDelay = 1500 * time.Millisecond
	b.interrupt = make(chan struct{})
	b.resizeBuffer = make(chan int)
	b.messageBuffer = make(chan string)
	b.contentMenu = make([]*boxes.DivBox, 0)
	b.contentExt = make([]*boxes.DivBox, 0)
	b.currentPage = &pb.PageResponse{}
	b.activeLinks = make([]*link, 0)
	return &b
}

func (b *ugglyBrowser) start() (err error) {
	b.view, err = initScreen()
	if err != nil {
		return err
	}
	b.vW, b.vH = b.view.Size()
	// draw a blank page with menu to start
	go b.pollEvents()
	//go b.drawContent()
	go b.menuWatch()
	loggo.Info("building menu content")
	b.buildContentMenu("init")
	startLink := link{
		server:   b.sess.server,
		port:     b.sess.port,
		pageName: b.sess.currPage,
	}
	loggo.Info("getting page from server")
	b.get(&startLink)
	loggo.Info("starting interrupt loop")
browloop:
	for {
		select {
		case <-b.interrupt:
			loggo.Info("breaking interrupt loop")
			break browloop
		}
	}
	return err
}

var brow *ugglyBrowser

func main() {
	flag.Parse()
	// daemonFlag = false
	setLogger(daemonFlag, logFile, *logLevel)
	boxes.Loggo = loggo
	ugcon.Loggo = loggo
	// set up a break channel for monitoring exit keystrokes
	brow = newBrowser()
	brow.sess = newSession()
	brow.sess.server = *host
	brow.sess.port = *port
	brow.sess.currPage = *page
	brow.start()
	defer brow.view.Fini()
}
