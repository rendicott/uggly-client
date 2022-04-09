package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	"github.com/rendicott/ugform"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"net/url"
	"os"
	"strings"
	"time"
)

var version string

var (
	logFile = "uggcli.log.json"
	//logLevel   = "info"
	logLevel = flag.String("loglevel", "info", "log level 'info' or 'debug'")
	ugri     = flag.String("UGRI", "", "The uggly resource identifier, e.g., ugtps://myserver.domain.net:8443/home")

//	logPane  = flag.Bool("log-pane", false, "whether or not to include a client logging pane for debugging")
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
	//h := fmt.Sprintf("http://%s", connString)
	var prefix string
	if strings.Contains(connString, "ugtps://") {
		prefix = "ugtps://"
	} else {
		prefix = "ugtp://"
	}
	h := strings.Replace(connString, "ugtp", "http", 1)
	u, err := url.Parse(h)
	if err != nil {
		return host, port, page, err
	}
	host = fmt.Sprintf("%s%s", prefix, u.Hostname())
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

func (b *ugglyBrowser) processPageForms(page *pb.PageResponse, isMenu bool) {
	b.forms = make([]*ugform.Form, 0) // purge existing forms
	for _, form := range page.Elements.Forms {
		f, err := ugcon.ConvertFormLocalForm(form, b.view)
		if err != nil {
			loggo.Error("error processing form", "err", err.Error())
			continue
		}
		b.forms = append(b.forms, f)
		if isMenu {
			b.menuForms = make([]*ugform.Form, 0) // purge existing forms
			b.menuForms = append(b.menuForms, f)
		}
	}
	// always add back the menu forms
	for _, mf := range b.menuForms {
		b.forms = append(b.forms, mf)
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
		b.vW, b.menuHeight, b.sess.server, b.sess.port, b.sess.currPage, msg, b.sess.secure)
	b.parseLinks(localPage, true) // retain links when injecting Menu
	b.processPageForms(localPage, true)
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
		b.drawContent("menu")
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
		startLink.construct()
		startLink.deconstruct()
		loggo.Info("refreshing page from server")
		b.get2(ctx, startLink.genReq())
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

func (b *ugglyBrowser) get2(ctx context.Context, pq *pb.PageRequest) {
	var err error
	ctx, _ = context.WithTimeout(context.Background(), 5*time.Second)
	pq.ClientWidth = int32(b.vW)
	pq.ClientHeight = int32(b.vH)
	dest := fmt.Sprintf("%s:%s", pq.Server, pq.Port)
	b.sendMessage(fmt.Sprintf("dialing server '%s'...", dest), "get2-preDial")
	b.currentPage, err = b.sess.get2(ctx, pq)
	if err != nil {
		if err.Error() == "context deadline exceeded" {
			b.sendMessage(
				fmt.Sprintf("connection timeout to '%s'", dest), "get2-timeout")
			return
		}
		if err.Error() == "error getting page from server" {
			msg := fmt.Sprintf("error getting page '%s' from server", pq.Name)
			b.sendMessage(msg, "get2-notfound")
			loggo.Error(msg)
		} else {
			b.handle(err)
		}
	} else {
		b.sendMessage("connected!", "get2-success")
		b.currentPageLocal = nil // so refresh knows to get external
		b.handle(b.buildDraw("get2"))
	}
}

// processAddresBar takes the address bar's form collection data and tries
// to make it into a valid link to pass to the get() function. This is user
// typed data so must handle many possible inputs.
func (b *ugglyBrowser) processAddressBarInput(formContents map[string]string) (*link, error) {
	tempLink := link{
		class:      "page",
		keyStroke:  "",
		pageName:   "",
		server:     "",
		port:       "",
		connString: "",
		formName:   "",
	}
	loggo.Info("got address bar submission", "submission", formContents["connstring"])
	err := tempLink.build(formContents["connstring"])
	loggo.Info("built ugri from address bar submission", "ugri", tempLink.connString)
	return &tempLink, err
}

func (b *ugglyBrowser) processFormSubmission(ctx context.Context, name string) {
	for _, f := range b.forms {
		if f.Name == name {
			if f.Name == "address-bar" {
				// take contents of textbox
				// and build link to get page
				l, err := b.processAddressBarInput(f.Collect())
				if err != nil {
					go b.sendMessage("error parsing UGRI", "process-form")
					return
				} else {
					loggo.Info("dialing form submitted server",
						"server", l.server,
						"port", l.port,
						"page", l.pageName,
					)
					b.get2(ctx, l.genReq())
				}
			} else {
				// find form in current Page and
				// discover submit link, collect
				// contents from form and craft
				// PageRequest with FormData
				li := f.SubmitAction
				loggo.Debug("got mainbody form submission link")
				if li, ok := li.(*pb.Link); ok {
					l := b.convertLink(li)
					loggo.Debug("type assertion succeeded, getting link",
						"pageName", l.pageName,
						"server", l.server,
						"port", l.port,
					)
					// convert link to PageRequest
					pr := l.genReq()
					// gather data from form and build request
					data := f.Collect()
					pr.FormData = []*pb.FormData{}
					fd := &pb.FormData{
						Name:        f.Name,
						TextBoxData: []*pb.TextBoxData{},
					}
					for k, v := range data {
						td := pb.TextBoxData{
							Name:     k,
							Contents: v,
						}
						fd.TextBoxData = append(
							fd.TextBoxData, &td)
					}
					pr.FormData = append(pr.FormData, fd)
					b.get2(ctx, pr)
				}
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
func (b *ugglyBrowser) passForm(ctx context.Context, name string) {
	found := false
	for _, f := range b.forms {
		loggo.Info("checking all forms for desired form", "currForm", f.Name, "desiredName", name)
		if f.Name == name && !found {
			found = true
			interrupt := make(chan struct{})
			submit := make(chan string)
			// ctx cancel() can be called to unblock
			go b.formWatcher(ctx, interrupt, submit)
			go f.Poll(ctx, interrupt, submit)
			<-interrupt
			// TODO: we were blocking during form Poll so
			// we may need to update screen if it resized
			// during form, trying to send to resizeBuffer
			// made a weird bug
			loggo.Debug("polling passed back to main")
		}
	}
}

// linkRouter determines link type (e.g., page or form) and calls the
// appropriate method
func (b *ugglyBrowser) linkRouter(ctx context.Context, l *link) {
	switch l.class {
	case "page":
		loggo.Debug("linkrouter sending get2")
		b.get2(ctx, l.genReq())
	case "form":
		// warning, potentially blocking function
		// but ctx cancel() will regain control
		loggo.Info("detected form link, passing to passForm")
		b.passForm(ctx, l.formName)
	}
}

func (b *ugglyBrowser) handleLinks(ctx context.Context, ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyRune {
		loggo.Debug("detected keypress", "key", string(ev.Rune()))
	} else {
		_, name := detectSpecialKey(ev)
		loggo.Debug("detected keypress", "key", name)
	}
	loggo.Debug("checking activeLinks for expected keypresses", "numLinks", len(b.activeLinks))
	for _, l := range b.activeLinks {
		loggo.Debug("checking link", "expectedKey", l.keyStroke)
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
		loggo.Debug("polling and watching for links", "links", len(b.activeLinks))
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
				loggo.Debug("sending to handleLinks", "numLinks", len(b.activeLinks))
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
			loggo.Debug("reloaded links", "numLinks", len(b.activeLinks))
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
	secure     bool
}

func (l *link) construct() {
	prefix := "ugtp://"
	if l.secure {
		prefix = "ugtps://"
	}
	l.connString = fmt.Sprintf("%s%s:%s/%s", prefix, l.server, l.port, l.pageName)
}

func (l *link) deconstruct() (err error) {
	if strings.Contains(l.connString, "ugtps://") {
		l.secure = true
	} else {
		l.secure = false
	}
	// first cheat with junk http so we can cheat and use net/url parse package
	h := strings.Replace(l.connString, "ugtp", "http", 1)
	u, err := url.Parse(h)
	if err != nil {
		return err
	}
	l.server = u.Hostname()
	l.port = u.Port()
	l.pageName = strings.TrimPrefix(u.Path, "/")
	return err
}

func (l *link) build(junk string) (err error) {
	l.connString = junk
	err = l.deconstruct()
	if err != nil {
		// try guessing some stuff
		if !strings.Contains(l.connString, "ugtp") {
			// maybe user forgot protocol
			chunks := strings.Split(l.connString, ":")
			if len(chunks) > 1 {
				l.server = chunks[0]
				postPort := chunks[1]
				pageChunks := strings.Split(postPort, "/")
				if len(pageChunks) > 0 {
					l.port = strings.TrimPrefix(pageChunks[0], ":")
					l.pageName = pageChunks[1]
				}
			}
			l.construct()
			err = nil
		}
		// TODO: try harder, it's possible we could accept
		// all sorts of random values like "<page>" only
		// and assume current server:port. For now we'll
		// just pass the burden onto the user to do better
	}
	if l.server == "" {
		err = errors.New("error parsing url")
	}
	return err
}

func (l *link) genReq() *pb.PageRequest {
	return &pb.PageRequest{
		Name:   l.pageName,
		Server: l.server,
		Port:   l.port,
		Secure: l.secure,
	}
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
		loggo.Debug("added link to activeLinks", "keyStroke", l.keyStroke, "class", l.class)
	}
}

func (b *ugglyBrowser) convertLink(l *pb.Link) *link {
	var tempLink link
	tempLink.keyStroke = l.KeyStroke
	if l.FormName == "" {
		tempLink.class = "page"
		// if it has a connstring then that trumps everything else
		if l.ConnString != "" {
			tempLink.connString = l.ConnString
			tempLink.deconstruct() // set others
		} else {
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
			if l.Secure {
				tempLink.secure = true
			}
			if tempLink.server == b.sess.server && tempLink.port == b.sess.port {
				tempLink.secure = b.sess.secure
			}
			tempLink.construct()
			tempLink.deconstruct()
			loggo.Debug("convertLink built link", "connstring", tempLink.connString)
		}
	} else {
		tempLink.class = "form"
		tempLink.formName = l.FormName

	}
	return &tempLink
}

func (b *ugglyBrowser) parseLinks(page *pb.PageResponse, menu bool) {
	if menu { // clear menu links if we're rebuilding menu
		loggo.Debug("detected menu flag, purging menuLinks")
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
		tempLink := b.convertLink(l)
		if menu {
			b.menuLinks = append(b.menuLinks, tempLink)
		} else {
			b.activeLinks = append(b.activeLinks, tempLink)
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
	b.processPageForms(b.currentPage, false)
	b.parseLinks(b.currentPage, false)
	b.view.Clear()
	msg := fmt.Sprintf("buildDraw-%s", label)
	b.drawContent(msg)
	return err
}

// drawContent concats the contents of contentMenu and contentExt
// then draws to screen
func (b *ugglyBrowser) drawContent(label string) {
	if b.exitFlag {
		return
	}
	loggo.Debug("drawing content", "label", label)
	time.Sleep(5 * time.Millisecond)
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
		loggo.Debug("starting form", "formName", f.Name)
		f.Start()
	}
	b.view.Show()
}

type ugglyBrowser struct {
	view                   tcell.Screen
	contentMenu            []*boxes.DivBox
	forms                  []*ugform.Form  // stores forms known at this time
	menuForms              []*ugform.Form  // stores menuforms known at this time
	contentExt             []*boxes.DivBox // e.g., non-menu content
	currentPage            *pb.PageResponse
	currentPageLocal       *pb.PageResponse // so we don't get from external
	interrupt              chan struct{}
	sess                   *session    // gRPC stuff buried in session.go
	messages               []*string   // messages accessed from here
	messageBuffer          chan string // buffer mostly used as trigger/stack
	resizeBuffer           chan int    // buffers resize events
	resizing               bool        // locks out other resize attempts
	resizeDelay            time.Duration
	activeLinks, menuLinks []*link
	menuHeight             int
	exitFlag               bool
	vH                     int // view height (updates on resize event)
	vW                     int // view width (updates on resize event)
}

// newBrowser initializes all of the browser's properties
// and takes special care to instantiate lists of pointers
// because everyone hates a nil pointer panic
func newBrowser() *ugglyBrowser {
	b := ugglyBrowser{}
	b.menuHeight = 3
	// how long of a buffer between resize events
	// to solve resizeEvent jitter type issues
	b.resizeDelay = 500 * time.Millisecond
	b.interrupt = make(chan struct{})
	b.resizeBuffer = make(chan int)
	b.messageBuffer = make(chan string)
	b.contentMenu = make([]*boxes.DivBox, 0)
	b.contentExt = make([]*boxes.DivBox, 0)
	b.currentPage = &pb.PageResponse{}
	b.activeLinks = make([]*link, 0)
	return &b
}

// start initializes
func (b *ugglyBrowser) start(ugri string) (err error) {
	ctx, _ := context.WithCancel(context.Background())
	b.view, err = initScreen()
	if err != nil {
		return err
	}
	b.vW, b.vH = b.view.Size()
	go b.startupRefreshDelay()
	// start main event poller for keyboard activity
	go b.pollEvents(ctx)
	// start menu watcher which looks for messages to be
	// displayed in menu status bar
	go b.menuWatch()
	// draw a blank page with menu to start
	loggo.Info("building menu content")
	if ugri != "" {
		// build a local link as a bootstrap since
		// no server can send us any links yet
		startLink := link{}
		startLink.build(ugri)
		b.sess.server = startLink.server
		b.sess.port = startLink.port
		b.sess.secure = startLink.secure
		b.sess.currPage = startLink.pageName
		// try to get initial link from a server
		loggo.Info("getting page from server")
		b.get2(ctx, startLink.genReq())
	} else {
		loggo.Info("no start link, starting blank")
		go b.sendMessage("enter an address with F1", "start-blank")
	}
	//b.buildContentMenu("init")
	// start something that watches for exit
	// but keeps this start() method running
	// so main doesn't die
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

func (b *ugglyBrowser) startupRefreshDelay() {
	loggo.Info("ignoring startup resize event for 5 seconds")
	b.resizing = true
	time.Sleep(5 * time.Second)
	b.resizing = false
}

func main() {
	flag.Parse()
	// for log package daemon should always be true
	// i.e., don't log to stdout since tcell screen has
	// control over screen and when stdout is accessed at
	// the same time, weird things happen
	daemonFlag := true
	setLogger(daemonFlag, logFile, *logLevel)
	if version == "" {
		version = "0.0.0"
	}
	loggo.Info("uggly-client started", "version", version)
	// link this logger to sub-packages that support
	// log15 logger and export their global logger for
	// modification
	boxes.Loggo = loggo
	ugform.Loggo = loggo
	ugcon.Loggo = loggo
	brow = newBrowser()
	brow.sess = newSession()
	// start the monostruct
	err := brow.start(*ugri)
	defer brow.view.Fini()
	// clean up screen so we don't butcher the user's terminal
	if err != nil {
		loggo.Error("error starting browser", "err", err.Error())
		os.Exit(1)
	}
}
