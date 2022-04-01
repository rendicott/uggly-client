package main

import (
	"flag"
	"fmt"
	"context"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"github.com/rendicott/ugform"
	"os"
	"net/url"
	"strings"
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

// splitLink takes a connection string (e.g., from the address bar textbox
// and splits it into its component parts so it can be used to build a link struct
// for example
func splitLink(connString string) (host, port, page string, err error) {
	// first add junk http so we can cheat and use net/url parse package
        h := fmt.Sprintf("http://%s", connString)
        u, err := url.Parse(h)
        if err != nil {
		return host, port, page, err
	}
        host = u.Hostname()
        port = u.Port()
        page = strings.TrimPrefix(u.Path, "/")
	return host, port, page, err
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

func (b *ugglyBrowser) processPageForms(page *pb.PageResponse) {
	b.forms = make([]*ugform.Form, 0) // purge existing forms
	for _, form := range(page.Elements.Forms) {
		f, err := ugcon.ConvertFormLocalForm(form, b.view)
		if err != nil {
			loggo.Error("error processing form", "err", err.Error())
			continue
		}
		b.forms = append(b.forms, f)
	}
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
	b.processPageForms(localPage)
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


func (b *ugglyBrowser) colorDemo() {
	thisfunc := "colorDemo"
	b.currentPage = buildColorDemo(b.vW, b.vH)
	b.currentPageLocal = b.currentPage
	go b.sendMessage("locally generated color demo to show tcell color capabilities on this TTY", thisfunc)
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

func (b *ugglyBrowser) refresh(ctx context.Context) {
	if b.currentPageLocal == nil {
		startLink := link{
			server:   b.sess.server,
			port:     b.sess.port,
			pageName: b.sess.currPage,
		}
		loggo.Info("refreshing page from server")
		b.get(ctx, &startLink)
	} else if b.currentPageLocal != nil {
		if b.currentPageLocal.Name == "uggcli-colordemo" {
			b.colorDemo()
		}
		if b.currentPageLocal.Name == "uggcli-feedbrowser" {
			b.getFeed(ctx)
		}
	}
}

func (b *ugglyBrowser) getFeed(ctx context.Context) {
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

func (b *ugglyBrowser) get(ctx context.Context, l *link) {
	dest := fmt.Sprintf("%s:%s", l.server, l.port)
	loggo.Info("getting link", "connString", dest, "pageName", l.pageName)
	var err error
	// set dimensions so it can be sent by session
	b.sess.clientWidth = int32(b.vW)
	b.sess.clientHeight = int32(b.vH)
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

// processAddresBar takes the address bar's form collection data and tries
// to make it into a valid link to pass to the get() function. This is user
// typed data so must handle many possible inputs.
func (b *ugglyBrowser) processAddressBarInput(formContents map[string]string) (l *link, err error) {
	l = &link{
		class:     "page",
		keyStroke:  "",
		pageName:   "",
		server:     "",
		port:       "",
		connString: "",
		formName:   "",
	}
	l.server, l.port, l.pageName, err = splitLink(formContents["connstring"])
	if err != nil {
		// TODO: try harder, it's possible we could accept
		// all sorts of random values like "<page>" only 
		// and assume current server:port. For now we'll
		// just pass the burden onto the user to do better
		return l, err
	}
	return l, err
}

func (b *ugglyBrowser) processFormSubmission(ctx context.Context, name string) {
	for _, f := range b.forms {
		if f.Name == name {
			if f.Name == "address-bar" {
				// take contents of textbox
				// and build link to get page
				l, err := b.processAddressBarInput(f.Collect())
				if err != nil {
					return
				} else {
					loggo.Info("dialing form submitted server",
						"server", l.server,
						"port", l.port,
						"page", l.pageName,
					)
					b.get(ctx, l)
				}
			} else {
				// find form in current Page and
				// discover submit link, collect
				// contents from form and craft
				// PageRequest with FormData
				return
			}
		}
	}
}

func (b *ugglyBrowser) formWatcher(ctx context.Context, interrupt chan struct{}, submit chan string) {
	for {
		select {
			case <-ctx.Done():
				return
			case <-interrupt:
				return
			case formName := <-submit:
				b.processFormSubmission(ctx, formName)
				close(submit)
				return
		}
	}
}

// passForm takes a desired form name and then passes control over
// to the form. This is a blocking function as it waits for the 
// passed form to close the interrupt channel. 
func (b *ugglyBrowser) passForm (ctx context.Context, name string) {
	for _, f := range(b.forms) {
		loggo.Info("checking all forms for desired form", "currForm", f.Name, "desiredName", name)
		if f.Name == name {
			interrupt := make(chan struct{})
			submit := make(chan string)
			// ctx cancel() can be called to unblock
			go b.formWatcher(ctx, interrupt, submit)
			go f.Poll(ctx, interrupt, submit)
			<-interrupt
		}
	}
}

// linkRouter determines link type (e.g., page or form) and calls the
// appropriate method
func (b *ugglyBrowser) linkRouter(ctx context.Context, l *link) {
	switch l.class {
		case "page":
			b.get(ctx, l)
		case "form":
			// warning, potentially blocking function
			// but ctx cancel() will regain control
			loggo.Info("detected form link, passing to passForm")
			b.passForm(ctx, l.formName)
	}
}

func (b *ugglyBrowser) handleLinks(ctx context.Context, ev *tcell.EventKey) {
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
					loggo.Info("sending expected key to link router")
					b.linkRouter(ctx, l)
				}
			}
		}
		// if not special then maybe a rune
		if ev.Key() == tcell.KeyRune {
			if l.keyStroke == string(ev.Rune()) {
				b.linkRouter(ctx, l)
			}
		}
	}
}

func (b *ugglyBrowser) pollEvents(ctx context.Context) {
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
			case tcell.KeyF4:
				b.getFeed(ctx)
			case tcell.KeyF2:
				b.colorDemo()
			case tcell.KeyF5:
				b.refresh(ctx)
			default:
				loggo.Info("sending to handleLinks", "numLinks", len(b.activeLinks))
				b.handleLinks(ctx, ev)
				// not async, poll could be blocked in handleLinks
			}
		case *tcell.EventResize:
			b.view.Sync()
			if !b.resizing {
				go b.resizeHandler(ctx)
				b.resizeBuffer <- int(0)
			}
		case fakeEvent:
			loggo.Info("reloaded links", "numLinks", len(b.activeLinks))
		}
	}
}

type link struct {
	class      string // one of 'page' or 'form'
	keyStroke  string
	pageName   string
	server     string
	port       string
	connString string
	formName   string
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

func (b *ugglyBrowser) resizeHandler(ctx context.Context) {
	b.resizing = true
	<-b.resizeBuffer
	time.Sleep(b.resizeDelay)
	w, h := b.view.Size()
	b.sess.clientWidth = int32(w)
	b.sess.clientHeight = int32(h)
	b.vW = w
	b.vH = h
	b.refresh(ctx)
	//b.updateAll()
	b.resizing = false
}

func (b *ugglyBrowser) finalizeLinks() {
	// always add menu links to list
	for _, l := range b.menuLinks {
		b.activeLinks = append(b.activeLinks, l)
	}
	b.view.PostEvent(fakeEvent{})
	for _, l := range b.activeLinks {
		loggo.Info("added link to activeLinks", "keyStroke", l.keyStroke, "class", l.class)
	}
}


func (b *ugglyBrowser) parseLinks(page *pb.PageResponse, menu bool) {
	if menu { // clear menu links if we're rebuilding menu
		loggo.Info("detected menu flag, purging menuLinks")
		b.menuLinks = []*link{}
	}
	b.activeLinks = []*link{} // purge all links always
	if page == nil {
		return
	}
	if page.Links == nil {
		b.finalizeLinks() // always finalize to add menuLinks, etc
		return
	}
	for _, l := range page.Links {
		var tempLink link
		tempLink.keyStroke = l.KeyStroke
		if l.FormName == "" {
			tempLink.class = "page"
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
		} else {
			tempLink.class = "form"
			tempLink.formName = l.FormName

		}
		if menu {
			b.menuLinks = append(b.menuLinks, &tempLink)
		} else {
			b.activeLinks = append(b.activeLinks, &tempLink)
		}
	}
	b.finalizeLinks()
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
	// draw forms on top of canvas
	for _, f := range b.forms {
		loggo.Info("starting form", "formName", f.Name)
		f.Start()
	}
	b.view.Show()
}

type ugglyBrowser struct {
	view             tcell.Screen
	contentMenu      []*boxes.DivBox
	forms            []*ugform.Form
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
	menuLinks        []*link
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
	ctx, _ := context.WithCancel(context.Background())
	b.view, err = initScreen()
	if err != nil {
		return err
	}
	b.vW, b.vH = b.view.Size()
	// draw a blank page with menu to start
	go b.pollEvents(ctx)
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
	b.get(ctx, &startLink)
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
	ugform.Loggo = loggo
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
