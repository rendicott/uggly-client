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
			loggo.Debug("divbox rawcontents first pixel",
				"pixel", bi.RawContents[0][0].C)
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

func (b *ugglyBrowser) processPageForms(page *pb.PageResponse, isMenu bool) {
	b.forms = make([]*ugform.Form, 0) // purge existing forms
	if page.Elements != nil {
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
	b.parseKeyStrokes(localPage, true) // retain keyStrokes when injecting Menu
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
		partial := pb.Link{
			Server:   b.sess.server,
			Port:     b.sess.port,
			PageName: b.sess.currPage,
		}
		startLink, _ := b.linkFiller(&partial)
		loggo.Info("refreshing page from server")
		b.get2(ctx, linkRequest(startLink))
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
	keyStrokes, err := b.sess.feedKeyStrokes()
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
		b.currentPage = buildFeedBrowser(b.vW, keyStrokes)
		b.currentPageLocal = b.currentPage
		loggo.Debug("feed build complete", "len(page.KeyStrokes)", len(b.currentPage.KeyStrokes))
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
// to make it into a valid Link to pass to the get() function. This is user
// typed data so must handle many possible inputs.
func (b *ugglyBrowser) processAddressBarInput(formContents map[string]string) (*pb.Link, error) {
	loggo.Info("got address bar submission", "submission", formContents["connstring"])
	link, err := linkFromString(formContents["connstring"])
	loggo.Info("built link from address bar submission",
		"server", link.Server,
		"port", link.Port,
		"pageName", link.PageName,
		"secure", link.Secure,
	)
	return link, err
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
						"server", l.Server,
						"port", l.Port,
						"page", l.PageName,
						"secure", l.Secure,
					)
					b.get2(ctx, linkRequest(l))
				}
			} else {
				// find form in current Page and
				// discover submit link, collect
				// contents from form and craft
				// PageRequest with FormData
				li := f.SubmitAction
				loggo.Debug("got mainbody form submission link")
				if li, ok := li.(*pb.Link); ok {
					l, _ := b.linkFiller(li)
					loggo.Debug("type assertion succeeded, getting link",
						"pageName", l.PageName,
						"server", l.Server,
						"port", l.Port,
					)
					// convert link to PageRequest
					pr := linkRequest(l)
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

// keyStrokeRouter determines action type (e.g., page, form, div) and calls the
// appropriate method
func (b *ugglyBrowser) keyStrokeRouter(ctx context.Context, ks *pb.KeyStroke) {
	switch x := ks.Action.(type) {
		case *pb.KeyStroke_Link:
			loggo.Debug("keyStrokeRouter sending get2")
			b.get2(ctx, linkRequest(x.Link))
		case *pb.KeyStroke_FormActivation:
			// warning, potentially blocking function
			// but ctx cancel() will regain control
			loggo.Info("detected form activation action, passing to passForm")
			b.passForm(ctx, x.FormActivation.FormName)
	}
}

func (b *ugglyBrowser) handleKeyStrokes(ctx context.Context, ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyRune {
		loggo.Debug("detected keypress", "key", string(ev.Rune()))
	} else {
		_, name := detectSpecialKey(ev)
		loggo.Debug("detected keypress", "key", name)
	}
	loggo.Debug("checking activeKeyStrokes for expected keypresses", "numLinks", len(b.activeKeyStrokes))
	for _, ks := range b.activeKeyStrokes {
		loggo.Debug("checking key", "expectedKey", ks.KeyStroke)
		// see if we can detect a special
		for k, v := range tcell.KeyNames {
			if v == ks.KeyStroke {
				if ev.Key() == k {
					loggo.Info("sending expected key to keyStroke router")
					b.keyStrokeRouter(ctx, ks)
				}
			}
		}
		// if not special then maybe a rune
		if ev.Key() == tcell.KeyRune {
			if ks.KeyStroke == string(ev.Rune()) {
				b.keyStrokeRouter(ctx, ks)
			}
		}
	}
}

func (b *ugglyBrowser) pollEvents(ctx context.Context) {
	for {
		loggo.Debug("polling and watching for keyStrokes", "keyStrokes", len(b.activeKeyStrokes))
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
				loggo.Debug("sending to handleKeyStrokes", "numLinks", len(b.activeKeyStrokes))
				b.handleKeyStrokes(ctx, ev)
				// not async, poll could be blocked in handleKeyStrokes
			}
		case *tcell.EventResize:
			b.view.Sync()
			if !b.resizing {
				go b.resizeHandler(ctx)
				b.resizeBuffer <- int(0)
			}
		case fakeEvent:
			loggo.Debug("reloaded keyStrokes", "numKeyStrokes", len(b.activeKeyStrokes))
		}
	}
}

func linkRequest(in *pb.Link) *pb.PageRequest {
	return &pb.PageRequest{
		Name:   in.PageName,
		Server: in.Server,
		Port:   in.Port,
		Secure: in.Secure,
	}
}

// linkFiller takes a potentially partial Link and 
// tries to fill in all of the properties using context
// from the current server session
func (b *ugglyBrowser) linkFiller(partial *pb.Link) (*pb.Link, error) {
	var err error
	var full pb.Link
	full.PageName = partial.PageName
	// if server didn't specify new host:port
	// we'll assume it's the current server
	if partial.Server == "" {
		full.Server = b.sess.server
	} else {
		full.Server = partial.Server
	}
	if partial.Port == "" {
		full.Port = b.sess.port
	} else {
		full.Port = partial.Port
	}
	if partial.Secure {
		full.Secure = true
	}
	if full.Server == b.sess.server && full.Port == b.sess.port {
		full.Secure = b.sess.secure
	}
	return &full, err
}

// linkFromString takes a UGLI connection string (e.g., from 
// the address bar) and tries to parse it into a Link object.
func linkFromString(junk string) (*pb.Link, error) {
	var full pb.Link
	if strings.Contains(junk, "ugtps://") {
		full.Secure = true
	} else {
		full.Secure = false
	}
	// first cheat with junk http so we can cheat and use net/url parse package
	h := strings.Replace(junk, "ugtp", "http", 1)
	u, err := url.Parse(h)
	if err != nil {
		// try guessing some stuff
		if !strings.Contains(junk, "ugtp") {
			// maybe user forgot protocol
			chunks := strings.Split(junk, ":")
			if len(chunks) > 1 {
				full.Server = chunks[0]
				postPort := chunks[1]
				pageChunks := strings.Split(postPort, "/")
				if len(pageChunks) > 0 {
					full.Port = strings.TrimPrefix(pageChunks[0], ":")
					full.PageName = pageChunks[1]
				}
			}
			err = nil // an attempt was made
		}
		// TODO: try harder, it's possible we could accept
		// all sorts of random values like "<page>" only
		// and assume current server:port. For now we'll
		// just pass the burden onto the user to do better
	}
	full.Server = u.Hostname()
	full.Port = u.Port()
	full.PageName = strings.TrimPrefix(u.Path, "/")
	if full.Server == "" {
		err = errors.New("error parsing url")
	}
	return &full, err
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

func (b *ugglyBrowser) finalizeKeyStrokes() {
	// always add menu keystrokes to list
	for _, k := range b.menuKeyStrokes {
		b.activeKeyStrokes = append(b.activeKeyStrokes, k)
	}
	b.view.PostEvent(fakeEvent{})
	for _, k := range b.activeKeyStrokes {
		loggo.Debug("added keystroke to activeKeyStrokes", "keyStroke", k.KeyStroke)
	}
}

func (b *ugglyBrowser) parseKeyStrokes(page *pb.PageResponse, menu bool) {
	if menu { // clear menu keyStrokes if we're rebuilding menu
		loggo.Debug("detected menu flag, purging menuKeyStrokes")
		b.menuKeyStrokes = []*pb.KeyStroke{}
	}
	b.activeKeyStrokes = []*pb.KeyStroke{} // purge all keyStrokes always
	if page == nil {
		return
	}
	if page.KeyStrokes == nil {
		b.finalizeKeyStrokes() // always finalize to add menuKeyStrokes, etc
		return
	}
	for _, k := range page.KeyStrokes{
		// first we need to know what type of keystroke we have
		switch x := k.Action.(type) {
			case *pb.KeyStroke_Link:
				loggo.Debug("found link action on page")
				// fill in keyStroke properties sent over wire so we know more about them
				x.Link, _ = b.linkFiller(x.Link)
			case *pb.KeyStroke_FormActivation:
				loggo.Debug("found formactivation action on page")
			case *pb.KeyStroke_DivScroll:
				loggo.Debug("found divscroll action on page")
		}
		if menu {
			b.menuKeyStrokes = append(b.menuKeyStrokes, k)
		} else {
			b.activeKeyStrokes = append(b.activeKeyStrokes, k)
		}
	}
	b.finalizeKeyStrokes()
	loggo.Debug("parseKeyStrokes complete", "len(b.activeKeyStrokes)",len(b.activeKeyStrokes))
}

// buildDraw takes all of the currently set content in the browser
// and renders it then triggers a draw action
func (b *ugglyBrowser) buildDraw(label string) (err error) {
	b.contentExt, err = convertPageBoxes(b.currentPage)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		return err
	}
	if b.currentPage != nil {
		b.processPageForms(b.currentPage, false)
		b.parseKeyStrokes(b.currentPage, false)
	}
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
	// to prevent race conditions with many things creating content
	// should replace with mutexes later
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
	activeKeyStrokes       []*pb.KeyStroke
	menuKeyStrokes         []*pb.KeyStroke
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
	b.activeKeyStrokes = make([]*pb.KeyStroke, 0)
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
		startLink, _ := linkFromString(ugri)
		b.sess.server = startLink.Server
		b.sess.port = startLink.Port
		b.sess.secure = startLink.Secure
		b.sess.currPage = startLink.PageName
		// try to get initial link from a server
		loggo.Info("getting page from server")
		b.get2(ctx, linkRequest(startLink))
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
