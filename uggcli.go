package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	"github.com/rendicott/ugform"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"github.com/rendicott/uggo"
	"github.com/rendicott/uggsec"
)

var version string

var (
	logFile  = "uggcli.log.json"
	logLevel = flag.String("loglevel", "info", "log level 'info' or 'debug'")
	breaks   = flag.Bool("breaks", false, "when set the program will stop at various"+
		" points so the log can be read easier")
	ugri = flag.String("UGRI", "", "The uggly resource identifier, "+
		"e.g., ugtps://myserver.domain.net:8443/home")
	genPass = flag.Bool("vault-pass-gen", false, "On systems that do not have an OS "+
		"keyring the vault encryption password must be stored in an ENV "+
		"variable instead. This flag causes the browser to generate an uggsec "+
		"vault encryption password and dump to STDOUT. Useful when used in "+
		"conjunction with commands like `export UGGSECP=$(ugglyc -vault-pass-gen)`")
	vaultEnvVar = flag.String("vault-password-env-var", "UGGSECP", "The ENV var that "+
		"is used to store the vault encryption password on systems that do no support "+
		"an OS keyring. See `vault-pass-gen` flag for generating password")
	vaultFile = flag.String("vault-file", "cookies.json.encrypted", "filename where "+
		"encrypted cookies are stored. Encryption key will try to be stored in OS "+
		"keyring if available otherwise you'll have to manually generate a password "+
		"and set an ENV var. See `vault-password-env-var` and `vault-pass-gen` "+
		"for more details.")
	configFile = flag.String("config", "config.yml", "filename where browser settings "+
		"are stored. Command parameters will always override settings loaded from file.")
	headless = flag.Bool("headless", false, "Run without terminal (uses tcell.SimulationScreen)")
	autoExit = flag.Bool("auto-exit", false, "Exit immediately when synthetic key queue is exhausted")
	timeoutF = flag.Float64("timeout", 10, "Seconds to wait after synthetic queue drains before forcing exit")
	output   = flag.String("output", "", "File path for headless screen capture (default: stdout)")
)

// loggo is the global logger
var loggo log15.Logger

// headlessCaptureScreen captures the current screen and writes output to --output file.
func (b *ugglyBrowser) headlessCaptureScreen() {
	if !b.headless {
		return
	}
	text, err := b.ScreenOutput()
	if err != nil {
		loggo.Warn("failed to capture screen", "error", err.Error())
		return
	}
	if text == "" {
		return
	}
	if *output != "" {
		if err := os.WriteFile(*output, []byte(text), 0644); err != nil {
			loggo.Error("failed to write output file", "error", err.Error())
		} else {
			loggo.Info("screen capture written", "file", *output)
		}
	} else {
		fmt.Print(text)
	}
}

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
	debugTags := []string{"transform", "draw"}
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
		loggo.Debug("build boxes.TextBlob",
			"tb.Content", tb.Content,
			"tags", debugTags)
		fgcolor, _, _ := tb.Style.Decompose()
		tcolor := fgcolor.TrueColor()
		loggo.Debug("style after conversion",
			"fgcolor", tcolor,
			"page-name", page.Name,
			"tags", debugTags)
		if page.Name == "uggcli-menu" {
			loggo.Debug("got menu textblob",
				"content", ele.Content,
				"tags", debugTags)
		}
		tb.MateBoxes(myBoxes)
	}
	for _, bi := range myBoxes {
		loggo.Debug("calling divbox.Init()",
			"tags", debugTags)
		bi.Init()
		if len(bi.RawContents) > 0 && bi.Height != 0 && bi.Width != 0 {
			loggo.Debug("divbox rawcontents first pixel",
				"pixel", bi.RawContents[0][0].C,
				"tags", debugTags)
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

func initScreen(headless bool) (s tcell.Screen, err error) {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	var tcellErr error
	if headless {
		s = tcell.NewSimulationScreen("UTF-8")
	} else {
		s, tcellErr = tcell.NewScreen()
		if tcellErr != nil {
			return nil, tcellErr
		}
	}
	if tcellErr = s.Init(); tcellErr != nil {
		return nil, tcellErr
	}
	s.SetStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.ColorBlack))
	return s, nil
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

func (b *ugglyBrowser) processPageForms(page *pb.PageResponse, isMenu bool, label string) {
	debugTags := []string{"form", "menu"}
	loggo.Debug("starting processPageForms, purging all forms",
		"label", label, "startingForms", len(b.forms),
		"tags", debugTags)
	b.forms = make([]*ugform.Form, 0) // purge existing forms
	if isMenu {
		loggo.Debug("job flagged as isMenu so purging menu forms",
			"tags", debugTags)
		b.menuForms = make([]*ugform.Form, 0) // purge existing forms
	}
	if page.Elements != nil {
		for _, form := range page.Elements.Forms {
			loggo.Debug("convering page form to ugform",
				"tags", debugTags,
				"formName", form.Name,
				"formDivName", form.DivName,
			)
			f, err := ugcon.ConvertFormLocalForm(form, b.view)
			if err != nil {
				loggo.Error("error processing form", "err", err.Error(), "label", label)
				continue
			}
			// now we have it in ugform.Form format so
			// we have the form.ShiftXY method available so we can
			// shove the form into DivBox like the docs say we do
			// this prevents them from covering the menu too
			// if people tell them to start at positionY = 0
			loggo.Debug("shifting forms to be relative to DivBox",
				"tags", debugTags)
			foundDiv := false
			for _, div := range page.DivBoxes.Boxes {
				if form.DivName == div.Name {
					foundDiv = true
					loggo.Debug("shifting form to start in DivBox",
						"formName", form.Name,
						"divName", div.Name,
						"label", label,
						"tags", debugTags)
					sX := int(div.StartX)
					sY := int(div.StartY + div.BorderW)
					if !isMenu {
						sY += b.menuHeight
					}
					f.ShiftXY(sX, sY)
				}
			}
			if isMenu {
				b.menuForms = append(b.menuForms, f)
			} else {
				loggo.Debug("adding page form to b.forms",
					"beforeAdd", len(b.forms),
					"tags", debugTags)
				if foundDiv {
					b.forms = append(b.forms, f)
				}
			}
		}
	}
	// always add back the menu forms
	loggo.Debug("before adding back menu forms",
		"beforeAddMenuForms", len(b.forms), "numMenuForms", len(b.menuForms),
		"tags", debugTags)
	for _, mf := range b.menuForms {
		b.forms = append(b.forms, mf)
	}
	loggo.Debug("have final forms",
		"forms", len(b.forms),
		"menuForms", len(b.menuForms),
		"label", label,
		"tags", debugTags)
}

func (b *ugglyBrowser) buildContentMenu(label string) {
	// makes boxes for the uggcli menu top bar
	debugTags := []string{"menu", "form"}
	label = fmt.Sprintf("%s-buildContentMenu", label)
	var msg string
	if len(b.messages) > 0 {
		msg = *b.messages[len(b.messages)-1]
	} else {
		msg = ""
	}
	localPage := buildPageMenu(
		b.vW, b.menuHeight, b.sess.server, b.sess.port, b.sess.currPage, msg, b.sess.secure)
	b.parseKeyStrokes(localPage, true) // retain keyStrokes when injecting Menu
	loggo.Debug("after menu build have forms",
		"pageForms", len(localPage.Elements.Forms),
		"tags", debugTags)
	b.processPageForms(localPage, true, label)
	loggo.Debug("after processPageForms have browser forms",
		"forms", len(b.forms), "menuForms", len(b.menuForms),
		"tags", debugTags)
	for _, form := range b.forms {
		loggo.Debug("form details",
			"formName", form.Name,
			"tags", debugTags)
	}
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
		if b.currentPage != nil {
			loggo.Debug("menu build but current page not nil so processing forms and keystrokes with menu=false",
				"label", label, "b.currentPage", b.currentPage.Name,
				"tags", debugTags)
			b.processPageForms(b.currentPage, false, label)
			loggo.Debug("after menu build with current page non-nil have forms", "forms", len(b.forms))
			b.parseKeyStrokes(b.currentPage, false)
		}
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
	loggo.Info("processing sendMessage",
		"msg", msg,
		"label", label,
	)
	b.messageBuffer <- msg
}

func (b *ugglyBrowser) settingsPage(infoMsg string) {
	thisfunc := "settingsPage"
	loggo.Info("building settings page")
	b.currentPage = buildSettings(b.vW, b.vH, b.settings, infoMsg)
	b.currentPageLocal = b.currentPage
	go b.sendMessage("Local Settings", thisfunc)
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) bookmarksPage() {
	thisfunc := "bookmarksPage"
	loggo.Info("building bookmarks page")
	b.currentPage = buildBookmarks(b.vW, b.vH, b.settings)
	b.currentPageLocal = b.currentPage
	go b.sendMessage("Bookmarks Browser", thisfunc)
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) bookmarkAdd() {
	thisfunc := "bookmarkAdd"
	ugri := b.sess.genUgri()
	b.settings.addBookmark("", *ugri)
	loggo.Info("adding bookmark")
	message := fmt.Sprintf("added bookmark: '%s'", *ugri)
	go b.sendMessage(message, thisfunc)
	err := b.settingsSave()
	if err != nil {
		loggo.Error("error adding bookmark", "err", err.Error())
		message = "error adding bookmark, check log"
		go b.sendMessage(message, thisfunc)
	}
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
	err := b.storeCookies()
	if err != nil {
		loggo.Error("error storing cookies on close", "error", err.Error())
		if strings.Contains(err.Error(), "no password found") {
			b.exitMessages = append(
				b.exitMessages,
				"Warning: Cookie storage failed on close due to absence of keyring"+
					" or missing encryption password. Cookies will be ephemeral until "+
					" this is fixed. To fix this, run the browser with the "+
					"`--help` parameter and generate a new password and store it"+
					" in the desired ENV var")
		}
	}
	close(b.interrupt)
	close(b.messageBuffer)
	if b.headless {
		b.headlessCaptureScreen()
	}
	b.view.Fini()
	for _, message := range b.exitMessages {
		fmt.Println(message)
	}
	os.Exit(code)
}

// startHeadlessTimer waits for the synthetic key queue to drain,
// then injects a synthetic F10 to interrupt the blocking PollEvent()
// and trigger headless capture and shutdown.
func (b *ugglyBrowser) startHeadlessTimer() {
	for {
		time.Sleep(100 * time.Millisecond)
		if b.synthStepIndex >= len(b.syntheticSteps) {
			// Queue drained. Start countdown or exit immediately.
			if b.autoExit {
				time.Sleep(500 * time.Millisecond) // give draw loop one cycle
				b.postF10()
				return
			}
			if b.exitDelay > 0 {
				time.Sleep(time.Duration(b.exitDelay) * time.Second)
			} else {
				// No timeout means we exit immediately after drain
				time.Sleep(1 * time.Second)
			}
			b.postF10()
			return
		}
	}
}

// postF10 injects a synthetic F10 event into the screen's event queue.
// This unblocks any blocking PollEvent() call and triggers shutdown.
func (b *ugglyBrowser) postF10() {
	loggo.Info("headless timeout reached, injecting F10 to exit")
	b.view.PostEvent(tcell.NewEventKey(tcell.KeyF10, 0, 0))
}

func (b *ugglyBrowser) refresh(ctx context.Context) {
	if b.currentPageLocal == nil {
		partial := pb.Link{
			Server:   b.sess.server,
			Port:     b.sess.port,
			PageName: b.sess.currPage,
			Stream:   b.sess.stream,
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
		if b.currentPageLocal.Name == "uggcli-settings" {
			b.settings = b.settingsLoad()
			b.settingsPage("")
		}
		if b.currentPageLocal.Name == "uggcli-bookmarks" {
			b.bookmarksPage()
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

func (b *ugglyBrowser) streamHandler(stream chan *pb.PageResponse) {
	var ok bool
	for {
		select {
		case b.currentPage, ok = <-stream:
			if !ok {
				stream = nil
			} else {
				loggo.Info("got page from stream, drawing...")
				b.setCookies(b.currentPage)
				b.handle(b.buildDraw("get2"))
				//time.Sleep(1*time.Millisecond)
				if b.currentPage.StreamDelayMs == 0 {
					time.Sleep(500 * time.Millisecond)
				} else {
					s := time.Duration(b.currentPage.StreamDelayMs)
					time.Sleep(s * time.Millisecond)
				}
			}
		}
		if stream == nil {
			//b.sendMessage("stream ended", "streamWatcher")
			return
		}
	}
}

func (b *ugglyBrowser) cexVendor() {
	ctx, cancel := context.WithCancel(context.Background())
	for {
		select {
		case msg := <-b.cexCancel:
			loggo.Info("caught cancel", "cancel-msg", msg)
			loggo.Info("calling cancel in watcher cexVendor")
			//go b.sendMessage("cancelling connection", "cexVendor-cancel")
			cancel()
			// reset context
			ctx, cancel = context.WithCancel(context.Background())
		case job := <-b.cexJobs:
			loggo.Info("got request for new context")
			switch job {
			case "page":
				ctx, cancel = context.WithTimeout(
					context.Background(), 5*time.Second)
				b.cexOut <- ctx
				loggo.Info("sent timeout ctx to requestor")
			case "stream":
				ctx, cancel = context.WithCancel(context.Background())
				loggo.Debug("sending cancel ctx to requestor channel")
				b.cexOut <- ctx
				loggo.Info("sent cancel ctx to requestor")
			case "form":
				ctx, cancel = context.WithCancel(context.Background())
				b.cexOut <- ctx
				loggo.Info("sent blank ctx to requestor")
			default:
				loggo.Info("sending current ctx to requestor")
				b.cexOut <- ctx
			}
		}
	}
}

func (b *ugglyBrowser) get2(ctx context.Context, pq *pb.PageRequest) {
	var err error
	pq.ClientWidth = int32(b.vW)
	pq.ClientHeight = int32(b.vH)
	dest := fmt.Sprintf("%s:%s", pq.Server, pq.Port)
	b.sendMessage(fmt.Sprintf("dialing server '%s'...", dest), "get2-preDial")
	b.breaks("PRE-GET")
	if pq.Stream {
		loggo.Info("connecting to stream")
		stream := make(chan *pb.PageResponse)
		go b.streamHandler(stream)
		loggo.Info("requesting cancellable context from cexVendor")
		b.cexJobs <- "stream"
		ctxc, pqc := b.addCookies(<-b.cexOut, pq)
		go b.sendMessage("connected to stream!", "get2-stream-success")
		b.currentPageLocal = nil // so refresh knows to get external
		err = b.sess.getStream(ctxc, pqc, stream)
		b.breaks("AFTER STREAM GET")
		if err != nil {
			loggo.Error("error getting stream", "error", err.Error())
			b.sendMessage("error getting stream", "get2-stream-fail")
		}
	} else {
		loggo.Info("requesting timeout context from cexVendor")
		b.cexJobs <- "page"
		ctxc, pqc := b.addCookies(<-b.cexOut, pq)
		b.currentPage, err = b.sess.get2(ctxc, pqc)
	}
	if err != nil {
		if err.Error() == "context deadline exceeded" {
			b.sendMessage(
				fmt.Sprintf("connection timeout to '%s'", dest), "get2-timeout")
			return
		} else if err.Error() == "error getting page from server" {
			msg := fmt.Sprintf("error getting page '%s' from server", pq.Name)
			go b.sendMessage(msg, "get2-notfound")
			loggo.Error(msg)
		} else if strings.Contains(err.Error(), "connection refused") {
			msg := fmt.Sprintf("connection refused")
			go b.sendMessage(msg, "get2-refused")
			loggo.Error(msg)
		} else if strings.Contains(err.Error(), "context cancel") { // wow, spelling
			msg := fmt.Sprintf("connection cancelled")
			go b.sendMessage(msg, "get2-cancelled")
			loggo.Error(msg)
		} else {
			b.handle(err)
		}
	} else if !pq.Stream {
		go b.sendMessage("connected!", "get2-success")
		b.currentPageLocal = nil // so refresh knows to get external
		// process cookies
		for _, setCookie := range b.currentPage.SetCookies {
			loggo.Debug("Got cookie from server", "key", setCookie.Key)
		}
		b.setCookies(b.currentPage)
		b.handle(b.buildDraw("get2"))
	}
}

// processAddresBar takes the address bar's form collection data and tries
// to make it into a valid Link to pass to the get2() function. This is user
// typed data so must handle many possible inputs.
func (b *ugglyBrowser) processAddressBarInput(formContents map[string]string) (*pb.Link, error) {
	loggo.Info("got address bar submission", "submission", formContents["connstring"])
	link, err := linkFromString(formContents["connstring"])
	//if strings.Contains(link.PageName, "->") {
	//	loggo.Info("detected '->' in form submitted link, converting to stream")
	//	link.Stream = true
	//}
	loggo.Info("built link from address bar submission",
		"server", link.Server,
		"port", link.Port,
		"pageName", link.PageName,
		"secure", link.Secure,
	)
	return link, err
}

func (b *ugglyBrowser) processFormSubmission(ctx context.Context, name string) {
	settingsSubmissionAuthorizedName := fmt.Sprintf("uggcli-settings-%s", localAuthUuid)
	for _, f := range b.forms {
		loggo.Debug("checking for form matches",
			"f.Name", f.Name,
			"name", name)
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
			} else if f.Name == settingsSubmissionAuthorizedName {
				// so we can be sure the submission is not some
				// nefarious server re-using our sacred "uggcli-settings" form
				loggo.Debug("detected settings submission")
				b.settingsProcess(f.Collect())
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

func (b *ugglyBrowser) isLocal(link *pb.Link) bool {
	if strings.Contains(link.PageName, localAuthUuid) && len(localAuthUuid) > 1 {
		loggo.Debug("isLocal verified page request is local",
			"link.PageName", link.PageName,
			"contains", localAuthUuid)
		return true
	}
	return false
}

func (b *ugglyBrowser) localLinkRouter(link *pb.Link) {
	if b.isLocal(link) { //double check
		loggo.Info("processing local link")
		if strings.Contains(link.PageName, "bookmark_delete") {
			chunks := strings.Split(link.PageName, "_")
			var bmUidString string
			if len(chunks) > 2 {
				bmUid, err := strconv.Atoi(chunks[2])
				if err != nil {
					loggo.Debug("error deleting bookmark, could not convert s to int",
						"err", err.Error(),
						"bmUidString", bmUidString)
					b.sendMessage("error deleting bookmark", "bookmark_delete")
				} else {
					ok := b.settings.deleteBookmark(bmUid)
					if ok {
						infoMsg := "bookmark deleted"
						err = b.settingsSave()
						if err != nil {
							infoMsg += ", error saving settings to disk"
						}
						b.sendMessage(infoMsg, "settings-process")
						b.settingsPage(infoMsg)
					} else {
						b.sendMessage("bookmark not deleted, could not find",
							"bookmark_delete")
					}
				}
			}
		}
	}
}

// keyStrokeRouter determines action type (e.g., page, form, div) and calls the
// appropriate method
func (b *ugglyBrowser) keyStrokeRouter(ctx context.Context, ks *pb.KeyStroke) {
	switch x := ks.Action.(type) {
	case *pb.KeyStroke_Link:
		if b.isLocal(x.Link) {
			go b.localLinkRouter(x.Link)
		} else {
			loggo.Debug("keyStrokeRouter sending get2")
			link, _ := b.linkFiller(x.Link)
			go b.get2(ctx, linkRequest(link))
			localAuthUuid = uggo.NewUuid()
		}
	case *pb.KeyStroke_FormActivation:
		// warning, potentially blocking function
		// but ctx cancel() will regain control
		loggo.Info("detected form activation action, passing to passForm")
		loggo.Info("getting new context without cancel or timeout")
		b.cexJobs <- "form"
		ctx = <-b.cexOut
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

func (b *ugglyBrowser) breaks(message string) {
	if b.debugBreaks {
		for {
			loggo.Debug("stopping at break point", "message", message)
			fmt.Printf("\n\n\nBREAK\n%s", message)
			ev := b.view.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyEnter:
					return
				}
			}
		}
	}
}

func (b *ugglyBrowser) pollEvents(ctx context.Context) {
	for {
		loggo.Debug("polling and watching for keyStrokes", "keyStrokes", len(b.activeKeyStrokes))
		if b.synthStepIndex < len(b.syntheticSteps) {
			step := b.syntheticSteps[b.synthStepIndex]
			b.synthStepIndex++

			// If step has an event, route it through existing logic
			if step.ev != nil {
				loggo.Debug("using synthetic event step",
					"index", b.synthStepIndex,
					"total", len(b.syntheticSteps))

				var ev tcell.Event = step.ev
				switch ev := ev.(type) {
				case *tcell.EventKey:
					switch ev.Key() {
					case tcell.KeyF10:
						b.cexCancel <- "user-cancel"
						b.exit(0)
						return
					case tcell.KeyCtrlL:
						loggo.Info("kill context")
						b.cexCancel <- "user-cancel"
					case tcell.KeyF4:
						b.cexCancel <- "user-cancel"
						b.getFeed(ctx)
					case tcell.KeyF2:
						b.cexCancel <- "user-cancel"
						b.colorDemo()
					case tcell.KeyF3:
						b.cexCancel <- "user-cancel"
						b.settingsPage("")
					case tcell.KeyF5:
						b.cexCancel <- "user-cancel"
						b.refresh(ctx)
					case tcell.KeyF6:
						b.cexCancel <- "user-cancel"
						b.bookmarksPage()
					case tcell.KeyF7:
						b.cexCancel <- "user-cancel"
						b.bookmarkAdd()
					default:
						loggo.Debug("sending to handleKeyStrokes",
							"numLinks", len(b.activeKeyStrokes))
						b.cexCancel <- "user-cancel"
						b.handleKeyStrokes(ctx, ev)
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

			// If step has a delay, sleep now
			if step.wait > 0 {
				loggo.Debug("synthetic delay", "duration", step.wait)
				time.Sleep(step.wait)
			}
		} else {
			// No synthetics left — fall through to live PollEvent()
			ev := b.view.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyF10:
					b.cexCancel <- "user-cancel"
					b.exit(0)
					return
				case tcell.KeyCtrlL:
					loggo.Info("kill context")
					b.cexCancel <- "user-cancel"
				case tcell.KeyF4:
					b.cexCancel <- "user-cancel"
					b.getFeed(ctx)
				case tcell.KeyF2:
					b.cexCancel <- "user-cancel"
					b.colorDemo()
				case tcell.KeyF3:
					b.cexCancel <- "user-cancel"
					b.settingsPage("")
				case tcell.KeyF5:
					b.cexCancel <- "user-cancel"
					b.refresh(ctx)
				case tcell.KeyF6:
					b.cexCancel <- "user-cancel"
					b.bookmarksPage()
				case tcell.KeyF7:
					b.cexCancel <- "user-cancel"
					b.bookmarkAdd()
				default:
					loggo.Debug("sending to handleKeyStrokes",
						"numLinks", len(b.activeKeyStrokes))
					b.cexCancel <- "user-cancel"
					b.handleKeyStrokes(ctx, ev)
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
}

func linkRequest(in *pb.Link) *pb.PageRequest {
	return &pb.PageRequest{
		Name:   in.PageName,
		Server: in.Server,
		Port:   in.Port,
		Secure: in.Secure,
		Stream: in.Stream,
	}
}

// linkFiller takes a potentially partial Link and
// tries to fill in all of the properties using context
// from the current server session
func (b *ugglyBrowser) linkFiller(partial *pb.Link) (*pb.Link, error) {
	var err error
	var full pb.Link
	full.PageName = partial.PageName
	if strings.Contains(partial.PageName, "->") {
		partial.Stream = true
	}
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
	full.Stream = partial.Stream
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
	if pageIsStream(full.PageName) {
		full.Stream = true
	}
	return &full, err
}

func pageIsStream(pageName string) bool {
	if strings.Contains(pageName, "->") {
		return true
	}
	return false
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
	for _, k := range page.KeyStrokes {
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
	loggo.Debug("parseKeyStrokes complete", "len(b.activeKeyStrokes)", len(b.activeKeyStrokes))
}

// buildDraw takes all of the currently set content in the browser
// and renders it then triggers a draw action
func (b *ugglyBrowser) buildDraw(label string) (err error) {
	label = fmt.Sprintf("%s-buildDraw", label)
	b.contentExt, err = convertPageBoxes(b.currentPage)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		return err
	}
	// check for empty content to give a user a more helpful explanation
	totalBoxes := len(b.contentExt)
	emptyBoxes := 0
	for _, box := range b.contentExt {
		if len(box.RawContents) == 0 {
			emptyBoxes += 1
		}
	}
	if totalBoxes == emptyBoxes {
		go b.sendMessage("all content empty...", "notifyEmptyBoxes")
	}
	// make sure we process forms and keystrokes even if we got here
	// during a menu build
	if b.currentPage != nil {
		b.processPageForms(b.currentPage, false, label)
		b.parseKeyStrokes(b.currentPage, false)
	}
	loggo.Debug("clearing screen contents")
	b.view.Clear()
	b.drawContent(label)
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
	// collect some stats
	dsForms := len(b.forms)
	dsMenuBoxes := len(b.contentMenu)
	dsExtBoxes := len(b.contentExt)
	dsTotalBoxes := len(content)
	loggo.Info("---------Draw Stats------------",
		"forms", dsForms, "menuBoxes", dsMenuBoxes,
		"extBoxes", dsExtBoxes, "totalBoxes", dsTotalBoxes)
}

type ugglyBrowser struct {
	view             tcell.Screen
	contentMenu      []*boxes.DivBox
	forms            []*ugform.Form  // stores forms known at this time
	menuForms        []*ugform.Form  // stores menuforms known at this time
	contentExt       []*boxes.DivBox // e.g., non-menu content
	currentPage      *pb.PageResponse
	currentPageLocal *pb.PageResponse // so we don't get from external
	interrupt        chan struct{}
	sess             *session    // gRPC stuff buried in session.go
	messages         []*string   // messages accessed from here
	messageBuffer    chan string // buffer mostly used as trigger/stack
	resizeBuffer     chan int    // buffers resize events
	resizing         bool        // locks out other resize attempts
	resizeDelay      time.Duration
	activeKeyStrokes []*pb.KeyStroke
	menuKeyStrokes   []*pb.KeyStroke
	cookies          map[string][]*pb.Cookie // all cookies stored for each server string
	menuHeight       int
	exitFlag         bool
	vH               int      // view height (updates on resize event)
	vW               int      // view width (updates on resize event)
	exitMessages     []string // messages to print on exit since stdout no worky during
	settings         *ugglyBrowserSettings
	settingsFile     string
	vaultPassEnvVar  string
	// define channels for context vendor
	cexCancel, cexJobs chan string
	cexOut             chan context.Context
	debugBreaks        bool
	syntheticSteps     []synthStep // interleaved events and delays from -key/-delay flags
	synthStepIndex     int         // drain cursor for synthetic steps
	// headless mode fields
	headless        bool    // use SimulationScreen instead of real terminal
	autoExit        bool    // exit immediately when synthetic queue drains
	exitDelay       float64 // timeout seconds after synthetic queue drains (0 = unlimited)
	headlessCapture string  // output file path for headless capture
}

// synthStep combines a synthetic keystroke event with an optional delay.
// ev is nil for delay-steps, wait is zero for event-steps.
type synthStep struct {
	ev   tcell.Event
	wait time.Duration
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
	b.cookies = make(map[string][]*pb.Cookie, 0)
	b.exitMessages = make([]string, 0)
	b.cexJobs = make(chan string)
	b.cexCancel = make(chan string)
	b.cexOut = make(chan context.Context)
	b.syntheticSteps = make([]synthStep, 0)
	return &b
}

// start initializes
func (b *ugglyBrowser) start(ugri string, headless bool) (err error) {
	localAuthUuid = uggo.NewUuid() // set this so it's not blank
	b.view, err = initScreen(headless)
	if err != nil {
		return err
	}
	err = b.loadCookies()
	if err != nil {
		loggo.Error("error loading cookies from file", "error", err.Error())
		// not fatal so we'll continue
		err = nil
	}
	b.vW, b.vH = b.view.Size()
	b.headless = headless
	b.autoExit = *autoExit
	b.exitDelay = *timeoutF
	b.headlessCapture = *output
	if headless {
		go b.startHeadlessTimer()
	}
	go b.startupRefreshDelay()
	loggo.Info("starting context vendor goroutine")
	go b.cexVendor()
	b.cexJobs <- "page"
	ctx := <-b.cexOut
	// start main event poller for keyboard activity
	go b.pollEvents(ctx)
	// start menu watcher which looks for messages to be
	// displayed in menu status bar
	go b.menuWatch()
	b.breaks("START")
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
		b.sess.stream = startLink.Stream
		// try to get initial link from a server
		loggo.Info("getting page from server")
		startLink, _ = b.linkFiller(startLink)
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
	// Extract -key and -delay values and remove them from os.Args before
	// flag.Parse() since flag.Parse() doesn't know about these flags.
	// All values are collected into one interleaved queue.
	var stepStrings []string
	var keptArgs = []string{os.Args[0]}
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		if (a == "-key" || a == "-delay") && i+1 < len(os.Args) {
			stepStrings = append(stepStrings, os.Args[i+1])
			i++ // skip the value
		} else {
			keptArgs = append(keptArgs, a)
		}
	}
	os.Args = keptArgs
	flag.Parse()
	// Build interleaved step queue from combined -key/-delay values
	brow = newBrowser()
	if len(stepStrings) > 0 {
		brow.syntheticSteps = make([]synthStep, len(stepStrings))
		for i, s := range stepStrings {
			brow.syntheticSteps[i] = makeStep(s)
		}
	}
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
	uggsec.Loggo = loggo

	if *genPass {
		fmt.Println(uggsec.NewVaultPassword())
		os.Exit(0)
	}
	brow.debugBreaks = *breaks
	var err error
	brow.settingsFile = *configFile
	brow.settings = brow.settingsLoad()
	// check to see if we need to override loaded config with any params
	if *vaultEnvVar != "UGGSECP" {
		brow.settings.VaultPassEnvVar = vaultEnvVar
	}
	if *vaultFile != "cookies.json.encrypted" {
		brow.settings.VaultFile = vaultFile
	}
	brow.sess = newSession()
	// start the monostruct
	err = brow.start(*ugri, *headless)
	defer brow.view.Fini()
	// clean up screen so we don't butcher the user's terminal
	if err != nil {
		loggo.Error("error starting browser", "err", err.Error())
		os.Exit(1)
	}
}

// synthesizeEvent creates a tcell.Event from a raw key string.
// Supported formats: single rune (e.g. "a"), named key (e.g. "Enter", "F5", "Tab"),
// Ctrl combination (e.g. "Ctrl-A", "Ctrl-X"), Shift+key (e.g. "Shift-Enter").
func synthesizeEvent(raw string) (tcell.Event, error) {
	// Check for Ctrl- prefix
	if strings.HasPrefix(raw, "Ctrl-") {
		suffix := strings.TrimPrefix(raw, "Ctrl-")
		ch := rune(0)
		if len(suffix) > 0 {
			ch = rune(suffix[0])
		}
		return tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModCtrl), nil
	}
	// Check for Shift- prefix
	if strings.HasPrefix(raw, "Shift-") {
		suffix := strings.TrimPrefix(raw, "Shift-")
		ch := rune(0)
		if len(suffix) > 0 {
			ch = rune(suffix[0])
		}
		return tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModShift), nil
	}
	// Check for Ctrl+ key
	if strings.HasPrefix(raw, "Ctrl+") {
		suffix := strings.TrimPrefix(raw, "Ctrl+")
		ch := rune(0)
		if len(suffix) > 0 {
			ch = rune(suffix[0])
		}
		return tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModCtrl), nil
	}
	// Check for Shift+ key
	if strings.HasPrefix(raw, "Shift+") {
		suffix := strings.TrimPrefix(raw, "Shift+")
		ch := rune(0)
		if len(suffix) > 0 {
			ch = rune(suffix[0])
		}
		return tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModShift), nil
	}
	// Check for named keys via tcell.KeyNames
	for key, name := range tcell.KeyNames {
		if strings.EqualFold(name, raw) {
			return tcell.NewEventKey(key, 0, 0), nil
		}
	}
	// Try as a single rune
	if len(raw) == 1 {
		return tcell.NewEventKey(tcell.KeyRune, rune(raw[0]), 0), nil
	}
	// Try as UTF-8 runes
	runes := []rune(raw)
	if len(runes) == 1 {
		return tcell.NewEventKey(tcell.KeyRune, runes[0], 0), nil
	}
	return nil, fmt.Errorf("cannot synthesize key: %s", raw)
}

// makeStep converts a raw CLI value into a synthStep. The value is tried as
// a duration (e.g. "2s", "500ms"), then as a bare float (seconds), then as a
// key name (via synthesizeEvent). On failure a warning is logged and a zero
// step is returned (discarded by the caller).
func makeStep(raw string) synthStep {
	// 1. Try as a Go duration string
	if d, err := time.ParseDuration(raw); err == nil {
		return synthStep{wait: d}
	}
	// 2. Try as a bare number (float) → seconds
	if n, err := strconv.ParseFloat(raw, 64); err == nil && n >= 0 {
		return synthStep{wait: time.Duration(n * float64(time.Second))}
	}
	// 3. Must be a key → synthesize event
	if ev, err := synthesizeEvent(raw); err == nil {
		return synthStep{ev: ev}
	}
	// 4. Can't parse → warn and skip
	loggo.Warn("unrecognized step value",
		"value", raw,
		"hint", "use a duration e.g. '2s'/'0.5s' or a key e.g. 'Enter'/'Tab'/'a'")
	return synthStep{}
}

// ScreenOutput captures the current SimulationScreen and returns it as
// ANSI 256-colored ASCII with Unicode box-drawing characters preserved.
func (b *ugglyBrowser) ScreenOutput() (string, error) {
	sim, ok := b.view.(tcell.SimulationScreen)
	if !ok {
		return "", fmt.Errorf("view is not a SimulationScreen")
	}
	cells, w, h := sim.GetContents()
	if w == 0 || h == 0 {
		return "", nil
	}

	var buf strings.Builder
	var lastFg, lastBg uint8
	var lastAttrs tcell.AttrMask

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			var c tcell.SimCell
			if idx < len(cells) {
				c = cells[idx]
			}
			// Ensure rune is never empty
			if len(c.Runes) == 0 {
				c.Runes = []rune{' '}
			}
			r := c.Runes[0]

			fg, bg, attrs := c.Style.Decompose()
			fg8 := uint8(fg)
			bg8 := uint8(bg)

			// Emit ANSI escape when style changes
			if fg8 != lastFg || bg8 != lastBg || attrs != lastAttrs {
				// Reset previously active attributes
				buf.WriteString("\033[0m")
				fgIdx := rgbTo256(fg)
				bgIdx := rgbTo256(bg)
				buf.WriteString(fmt.Sprintf("\033[38;5;%dm", fgIdx))
				buf.WriteString(fmt.Sprintf("\033[48;5;%dm", bgIdx))
				if attrs&tcell.AttrBold != 0 {
					buf.WriteString("\033[1m")
				}
				if attrs&tcell.AttrDim != 0 {
					buf.WriteString("\033[2m")
				}
				if attrs&tcell.AttrItalic != 0 {
					buf.WriteString("\033[3m")
				}
				if attrs&tcell.AttrUnderline != 0 {
					buf.WriteString("\033[4m")
				}
				if attrs&tcell.AttrBlink != 0 {
					buf.WriteString("\033[5m")
				}
				if attrs&tcell.AttrReverse != 0 {
					buf.WriteString("\033[7m")
				}
				if attrs&tcell.AttrStrikeThrough != 0 {
					buf.WriteString("\033[9m")
				}
				lastFg, lastBg, lastAttrs = fg8, bg8, attrs
			}
			buf.WriteRune(r)
		}
		if y < h-1 {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString("\033[0m\n")
	return buf.String(), nil
}

// rgbTo256 converts a tcell.Color to the nearest ANSI 256 palette index.
func rgbTo256(c tcell.Color) int {
	red, green, blue := c.RGB()
	r, g, b := int(red), int(green), int(blue)

	// Check standard 16 colors
	for i, entry := range stdColorTable {
		if entry[0] == r && entry[1] == g && entry[2] == b {
			return i
		}
	}

	// Check grayscale ramp (232-255)
	for i := 0; i < 24; i++ {
		v := 8 + 10*i
		if abs(r-v) <= 5 && abs(g-v) <= 5 && abs(b-v) <= 5 {
			return 232 + i
		}
	}

	// 6x6x6 cube (16-231)
	// Each component ∈ {0, 51, 102, 153, 204, 255}
	cube := []int{0, 95, 135, 175, 215, 255}
	cr := findNearest(cube, r)
	cg := findNearest(cube, g)
	cb := findNearest(cube, b)
	return 16 + cr*36 + cg*6 + cb
}

func findNearest(vals []int, target int) int {
	best := 0
	bestDist := abs(target - vals[0])
	for i := 1; i < len(vals); i++ {
		d := abs(target - vals[i])
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// stdColorTable defines the ANSI 256 color entries with RGB values.
// Indices 0-15: standard + bright colors
// Indices 16+: 6x6x6 cube and grayscale ramp computed by rgbTo256
var stdColorTable = [][]int{
	{0, 0, 0},       // 0: black
	{128, 0, 0},     // 1: maroon (standard red)
	{0, 128, 0},     // 2: green
	{128, 128, 0},   // 3: olive (yellow)
	{0, 0, 128},     // 4: blue
	{128, 0, 128},   // 5: purple (magenta)
	{0, 128, 128},   // 6: teal (cyan)
	{192, 192, 192}, // 7: silver (white/light gray)
	{128, 128, 128}, // 8: gray (dark gray)
	{255, 0, 0},     // 9: bright red
	{0, 255, 0},     // 10: bright green
	{255, 255, 0},   // 11: bright yellow
	{0, 0, 255},     // 12: bright blue
	{255, 0, 255},   // 13: bright magenta
	{0, 255, 255},   // 14: bright cyan
	{255, 255, 255}, // 15: bright white
}
