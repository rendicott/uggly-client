package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	"github.com/rendicott/ugform"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"github.com/rendicott/uggo"
	"github.com/rendicott/uggsec"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	pageTimeout = flag.Float64("page-timeout", 180, "Seconds to wait for a GetPage RPC (raise for slow LLM servers)")
	output   = flag.String("output", "", "File path for headless screen capture (default: stdout)")
	emitKeystrokes = flag.Bool("emit-keystrokes", false, "Emit available keystrokes as JSON to stderr (headless mode only)")
	keystrokesOutput = flag.String("keystrokes-output", "", "File path for keystroke JSON output (works in headless or interactive mode)")
	controlSocket  = flag.String("control-socket", "", "Unix socket path for interactive control (e.g., /tmp/uggly.sock)")
	controlPort    = flag.Int("control-port", 0, "TCP port for interactive control (0 = random free port)")
	noAutoExit     = flag.Bool("no-auto-exit", false, "Do not auto-exit after timeout (useful with control socket)")
	screenWidth  = flag.Int("width", 80, "Screen width in characters (headless mode)")
	screenHeight = flag.Int("height", 24, "Screen height in rows (headless mode)")
)

// loggo is the global logger
var loggo log15.Logger

// headlessCaptureScreen captures the current screen and writes output to --output file.
// Syncs the screen buffer first to ensure render is complete, then captures.
func (b *ugglyBrowser) headlessCaptureScreen() {
	loggo.Info("headlessCaptureScreen entered")
	if !b.headless {
		loggo.Info("not headless, returning early")
		return
	}
	// Force a render cycle before capture
	b.view.Show()
	// Wait for the render to propagate to the simulation screen's front buffer
	time.Sleep(200 * time.Millisecond)
	text, err := b.ScreenOutput()
	if err != nil {
		loggo.Warn("failed to capture screen", "error", err.Error())
		return
	}
	loggo.Info("screen capture result", "textLen", len(text), "outputFlag", *output)
	if text == "" {
		loggo.Warn("screen capture returned empty text")
		return
	}
	if *output != "" {
		loggo.Info("writing capture to file", "file", *output)
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
	if page.Elements != nil {
		if page.Elements.TextBlobs != nil {
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
		}
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

// isDeadline returns true for context deadlines and gRPC DeadlineExceeded.
func isDeadline(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.DeadlineExceeded {
		return true
	}
	// legacy string match after session wrapping
	return strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "DeadlineExceeded")
}

func isCancelled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
		return true
	}
	return strings.Contains(err.Error(), "context cancel")
}

func initScreen(headless bool) (s tcell.Screen, err error) {
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	var tcellErr error
	if headless {
		s = tcell.NewSimulationScreen("UTF-8")
		if tcellErr = s.Init(); tcellErr != nil {
			return nil, tcellErr
		}
		// Set screen size for headless simulation after Init
		if simScreen, ok := s.(tcell.SimulationScreen); ok {
			simScreen.SetSize(*screenWidth, *screenHeight)
		}
	} else {
		s, tcellErr = tcell.NewScreen()
		if tcellErr != nil {
			return nil, tcellErr
		}
		if tcellErr = s.Init(); tcellErr != nil {
			return nil, tcellErr
		}
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
	loggo.Debug("starting processPageForms",
		"label", label, "isMenu", isMenu,
		"pageForms", len(b.pageForms), "menuForms", len(b.menuForms),
		"tags", debugTags)

	// Keep page forms and menu forms in separate buckets so a menu redraw
	// (status message, etc.) cannot wipe the page's chat_prompt / login form.
	if isMenu {
		// While address-bar (or any menu form) is actively being polled, keep
		// the existing menuForms so we do not replace the live text buffer.
		if b.formActive != "" && b.activeForm != nil {
			loggo.Debug("skipping menu form rebuild while form active",
				"formActive", b.formActive, "label", label)
			b.recomposeForms()
			return
		}
		b.menuForms = make([]*ugform.Form, 0)
	} else {
		// Do not wipe page forms while one of them is active either
		if b.formActive != "" && b.activeForm != nil {
			// keep page forms; only recompose
			b.recomposeForms()
			return
		}
		b.pageForms = make([]*ugform.Form, 0)
	}

	if page != nil && page.Elements != nil {
		for _, form := range page.Elements.Forms {
			loggo.Debug("converting page form to ugform",
				"tags", debugTags,
				"formName", form.Name,
				"formDivName", form.DivName,
			)
			f, err := ugcon.ConvertFormLocalForm(form, b.view)
			if err != nil {
				loggo.Error("error processing form", "err", err.Error(), "label", label)
				continue
			}
			foundDiv := false
			if page.DivBoxes != nil {
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
						break
					}
				}
			}
			if !foundDiv {
				// Still register the form so activation keys work; shift by menu
				// height so it is not under the chrome.
				loggo.Warn("form div not found; registering form without div shift",
					"formName", form.Name, "divName", form.DivName, "label", label)
				if !isMenu {
					f.ShiftXY(0, b.menuHeight)
				}
			}
			if isMenu {
				b.menuForms = append(b.menuForms, f)
			} else {
				b.pageForms = append(b.pageForms, f)
			}
		}
	}
	b.recomposeForms()
	loggo.Debug("have final forms",
		"forms", len(b.forms),
		"pageForms", len(b.pageForms),
		"menuForms", len(b.menuForms),
		"label", label,
		"tags", debugTags)
}

// recomposeForms rebuilds b.forms from page + menu form buckets.
func (b *ugglyBrowser) recomposeForms() {
	b.forms = make([]*ugform.Form, 0, len(b.pageForms)+len(b.menuForms))
	b.forms = append(b.forms, b.pageForms...)
	b.forms = append(b.forms, b.menuForms...)
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
			// Re-bind page keystrokes, but do NOT rebuild page forms while a
			// form is active (would drop the live form object). If forms are
			// missing, ensurePageForms rebuilds them.
			loggo.Debug("menu build: refresh keystrokes; keep page forms if present",
				"label", label, "b.currentPage", b.currentPage.Name,
				"pageForms", len(b.pageForms), "formActive", b.formActive,
				"tags", debugTags)
			if b.formActive == "" {
				if len(b.pageForms) == 0 {
					b.ensurePageForms()
				} else {
					// recompose only (menu forms already updated above)
					b.recomposeForms()
				}
			} else {
				b.recomposeForms()
			}
			b.parseKeyStrokes(b.currentPage, false)
			loggo.Debug("after menu build forms", "forms", len(b.forms), "pageForms", len(b.pageForms))
		}
		b.drawContent("menu")
	}
}

// menuWatch drains the status message buffer as a fallback. Primary path is
// statusOnlyEvent handled on the pollEvents (UI) thread.
func (b *ugglyBrowser) menuWatch() {
	for {
		select {
		case <-b.interrupt:
			return
		case msg, ok := <-b.messageBuffer:
			if !ok || b.exiting() {
				return
			}
			b.mu.Lock()
			b.messages = append(b.messages, &msg)
			if len(b.messages) > 50 {
				b.messages = b.messages[len(b.messages)-50:]
			}
			formBusy := b.formActive != ""
			b.mu.Unlock()
			// statusOnlyEvent usually already updated UI; skip heavy rebuild here
			// unless no screen yet. Avoid lock nesting with buildContentMenu.
			if !formBusy && b.view == nil {
				b.buildContentMenu("messageBuffer")
			}
		}
	}
}

// sendMessage queues a status line for the menu bar. Non-blocking; safe after exit.
func (b *ugglyBrowser) sendMessage(msg, label string) {
	b.safeSendMessage(msg, label)
}

func (b *ugglyBrowser) settingsPage(infoMsg string) {
	thisfunc := "settingsPage"
	loggo.Info("building settings page")
	b.setCurrentPage(buildSettings(b.vW, b.vH, b.settings, infoMsg), true)
	go b.sendMessage("Local Settings", thisfunc)
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) bookmarksPage() {
	thisfunc := "bookmarksPage"
	loggo.Info("building bookmarks page")
	b.setCurrentPage(buildBookmarks(b.vW, b.vH, b.settings), true)
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
	b.setCurrentPage(buildColorDemo(b.vW, b.vH), true)
	go b.sendMessage("locally generated color demo to show tcell color capabilities on this TTY", thisfunc)
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) exit(code int) {
	b.exitOnce.Do(func() {
		loggo.Info("caught exit interrupt", "code", code)
		loggo.Info("exit called with headless", "headless", b.headless)
		b.setExiting()
		b.exitFlag = true

		// Cancel in-flight network
		b.fetchMu.Lock()
		if b.fetchCancel != nil {
			b.fetchCancel()
		}
		b.fetchMu.Unlock()

		// In headless mode, capture screen BEFORE closing channels
		if b.headless {
			loggo.Info("headless mode, calling capture BEFORE closing channels")
			b.headlessCaptureScreen()
		}

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

		if b.controlCancel != nil {
			b.controlCancel()
		}
		if b.controlServer != nil {
			b.controlServer.Close()
		}
		if *controlSocket != "" {
			os.Remove(*controlSocket)
		}

		// Close interrupt to wake waiters; do not close messageBuffer (senders use select).
		select {
		case <-b.interrupt:
		default:
			close(b.interrupt)
		}
		if b.view != nil {
			b.view.Fini()
		}
		for _, message := range b.exitMessages {
			fmt.Println(message)
		}
		os.Exit(code)
	})
}

// startHeadlessTimer waits for the synthetic key queue to drain,
// then injects a synthetic F10 to interrupt the blocking PollEvent()
// and trigger headless capture and shutdown.
func (b *ugglyBrowser) startHeadlessTimer() {
	// Wait until page has drawn at least once (or timeout budget starts)
	deadline := time.Now().Add(time.Duration(b.exitDelay)*time.Second + 5*time.Second)
	for !b.isPageReady() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	// Autofocus form if the page requested it (headless can type immediately after)
	if b.autofocusForm != "" {
		loggo.Info("headless autofocus form", "form", b.autofocusForm)
		// form activation needs a live context; post a synthetic via event loop is hard,
		// so we rely on servers that also send a form activation key for headless typing,
		// OR user injects the activation key. Still record readiness.
	}
	for {
		time.Sleep(100 * time.Millisecond)
		if b.synthStepIndex >= len(b.syntheticSteps) {
			// Queue drained. Ensure page had a chance to render.
			for !b.isPageReady() && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if b.autoExit {
				time.Sleep(500 * time.Millisecond) // give draw loop one cycle
				// Force a final Show so SimulationScreen front buffer is current
				if b.view != nil {
					b.view.Show()
					time.Sleep(100 * time.Millisecond)
				}
				b.postF10()
				return
			}
			if b.exitDelay > 0 {
				time.Sleep(time.Duration(b.exitDelay) * time.Second)
			} else {
				// No timeout means we exit immediately after drain
				time.Sleep(1 * time.Second)
			}
			if b.view != nil {
				b.view.Show()
				time.Sleep(100 * time.Millisecond)
			}
			b.postF10()
			return
		}
	}
}

// postF10 injects synthetic F10 events into the screen's event queue.
// This unblocks any blocking PollEvent() call and triggers shutdown.
// Posted twice so an autofocused form can exit on the first F10 and the
// main browser loop can exit on the second.
func (b *ugglyBrowser) postF10() {
	loggo.Info("headless timeout reached, injecting F10 to exit")
	_ = b.view.PostEvent(tcell.NewEventKey(tcell.KeyF10, 0, 0))
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = b.view.PostEvent(tcell.NewEventKey(tcell.KeyF10, 0, 0))
	}()
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
	thisfunc := "getFeed"
	feedErrMsg := "no server connection"
	feedErrMsgNoFeed := "server provides no feed"
	loggo.Info("getting feed")
	feed, err := b.sess.getFeed()
	if err != nil {
		if err.Error() == feedErrMsg {
			b.sendMessage("unable to connect to server", thisfunc)
		} else if err.Error() == feedErrMsgNoFeed {
			b.sendMessage(feedErrMsgNoFeed, thisfunc)
		} else {
			b.handle(err)
		}
	} else {
		b.feedListings = feed.GetPages()
		b.feedOffset = 0
		b.showFeedPage()
		loggo.Debug("feed loaded", "listings", len(b.feedListings))
	}
	b.handle(b.buildDraw(thisfunc))
}

func (b *ugglyBrowser) showFeedPage() {
	h := b.vH - b.menuHeight
	if h < 12 {
		h = 20
	}
	page := buildFeedBrowser(b.vW, h, b.feedListings, b.feedOffset, b.sess.server, b.sess.port)
	b.setCurrentPage(page, true)
}

func (b *ugglyBrowser) feedNext() {
	if b.feedOffset+feedPageSize < len(b.feedListings) {
		b.feedOffset += feedPageSize
		b.showFeedPage()
		b.handle(b.buildDraw("feed-next"))
	} else {
		b.sendMessage("end of feed", "feed-next")
	}
}

func (b *ugglyBrowser) feedPrev() {
	b.feedOffset -= feedPageSize
	if b.feedOffset < 0 {
		b.feedOffset = 0
	}
	b.showFeedPage()
	b.handle(b.buildDraw("feed-prev"))
}

func (b *ugglyBrowser) streamHandler(stream chan *pb.PageResponse, gen uint64) {
	// Coalesce: if the UI is behind, keep only the latest frame intent by posting
	// every frame but applying latest-only in applyFetchResult via gen check.
	// Pacing: streamDelayMs == 0 means server-paced only (no client sleep).
	// Positive delay is used only when the producer is faster than display and
	// the server did not already set 0 for realtime surfaces.
	for page := range stream {
		if b.exiting() || !b.isFetchCurrent(gen) {
			continue
		}
		loggo.Debug("got page from stream, drawing...")
		b.postFetchDone(&fetchDoneEvent{
			gen:  gen,
			page: page,
			err:  nil,
			dest: "stream",
			pq:   &pb.PageRequest{Stream: true},
		})
		// Realtime streams (games, live surface): streamDelayMs == 0 → no sleep.
		// Legacy producers omit the field (0) but historically expected ~500ms;
		// treat missing/zero as no sleep so server-paced games stay snappy.
		// Only sleep when server explicitly requests a positive delay.
		if page != nil && page.StreamDelayMs > 0 {
			time.Sleep(time.Duration(page.StreamDelayMs) * time.Millisecond)
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
				// Long timeout so LLM-backed servers can finish (was 5s — too short).
				to := time.Duration(*pageTimeout) * time.Second
				if to <= 0 {
					to = 180 * time.Second
				}
				ctx, cancel = context.WithTimeout(context.Background(), to)
				b.cexOut <- ctx
				loggo.Info("sent timeout ctx to requestor", "pageTimeout", to.String())
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

// startFetchPage kicks off an async page fetch. Results are applied on the UI
// thread via fetchDoneEvent so paint stays single-threaded. In-flight fetches
// are cancelled when a newer navigation starts.
func (b *ugglyBrowser) startFetchPage(pq *pb.PageRequest) {
	if pq == nil || b.exiting() {
		return
	}
	// Snapshot size under lock
	b.mu.Lock()
	pq.ClientWidth = int32(b.vW)
	pq.ClientHeight = int32(b.vH)
	b.mu.Unlock()

	gen, ctx := b.beginFetch(pq.Stream)
	dest := fmt.Sprintf("%s:%s/%s", pq.Server, pq.Port, pq.Name)
	b.sendMessage(fmt.Sprintf("dialing server '%s'...", dest), "get2-preDial")
	// Show loading chrome while waiting (skip for pure stream follow-up frames)
	if !pq.Stream {
		w, h := b.vW, b.vH
		if w == 0 {
			w = 80
		}
		if h == 0 {
			h = 24
		}
		b.mu.Lock()
		// Don't clobber an in-progress form page with loading chrome
		if b.formActive == "" {
			b.setCurrentPage(buildLoadingPage(w, h-b.menuHeight, dest), true)
		}
		b.mu.Unlock()
		if b.formActive == "" {
			_ = b.buildDraw("loading")
		}
	}

	if pq.Stream {
		loggo.Info("connecting to stream (no page-timeout deadline)")
		stream := make(chan *pb.PageResponse, 4)
		go b.streamHandler(stream, gen)
		go func() {
			pqc := b.addCookiesTo(ctx, pq)
			err := b.sess.getStream(ctx, pqc, stream)
			if err != nil && !isCancelled(err) {
				loggo.Error("error getting stream", "error", err.Error())
				msg := "stream ended"
				if isDeadline(err) {
					msg = "stream timed out — press F5 or re-open the page"
				} else {
					msg = "stream ended — F5 reconnect / re-open play"
				}
				b.sendMessage(msg, "get2-stream-fail")
				// Allow new navigation; mark stream idle
				b.streamConnected = false
				b.streamKeysBound = false
			} else if err == nil || isCancelled(err) {
				b.streamConnected = false
				b.streamKeysBound = false
			}
		}()
		return
	}

	go func() {
		pqc := b.addCookiesTo(ctx, pq)
		page, err := b.sess.get2(ctx, pqc)
		b.postFetchDone(&fetchDoneEvent{
			gen:  gen,
			pq:   pq,
			page: page,
			err:  err,
			dest: dest,
		})
	}()
}

// get2 is the legacy synchronous entry used by a few paths; it now starts an
// async fetch. Prefer startFetchPage directly.
func (b *ugglyBrowser) get2(ctx context.Context, pq *pb.PageRequest) {
	// Ignore passed ctx for lifecycle — beginFetch owns cancellation.
	_ = ctx
	b.startFetchPage(pq)
}

// applyFetchResult runs on the UI thread when a fetch completes.
func (b *ugglyBrowser) applyFetchResult(ev *fetchDoneEvent) {
	if ev == nil || b.exiting() {
		return
	}
	if !b.isFetchCurrent(ev.gen) {
		loggo.Info("ignoring stale fetch result", "gen", ev.gen, "current", atomic.LoadUint64(&b.fetchGen))
		return
	}
	if ev.err != nil {
		b.reportFetchError(ev.err, ev.dest, ev.pq)
		return
	}
	if ev.page == nil {
		b.sendMessage("empty page response", "get2-empty")
		return
	}
	isStream := ev.dest == "stream" || (ev.pq != nil && ev.pq.Stream)
	// Status: once for streams so every frame does not rebuild the menu strip.
	if isStream {
		if !b.streamConnected {
			b.sendMessage("streaming… (F8 screenshot, F9 pause)", "get2-stream-start")
			b.streamConnected = true
		}
		// F9 pause: keep receiving but do not repaint (frozen frame for capture/read)
		if b.streamPaused {
			return
		}
	} else {
		b.streamConnected = false
		b.streamPaused = false
		b.streamKeysBound = false
		b.sendMessage("connected!", "get2-success")
	}
	b.mu.Lock()
	// Preserve scroll offsets across stream frames of the same page name.
	samePage := b.currentPageWire != nil && ev.page != nil &&
		b.currentPageWire.Name == ev.page.Name
	if !samePage {
		b.divScrollY = make(map[string]int)
	}
	b.setCurrentPage(ev.page, false)
	// Skip cookie jar work when server sent none (every stream frame used to re-set).
	if len(ev.page.SetCookies) > 0 {
		for _, setCookie := range b.currentPage.SetCookies {
			loggo.Debug("Got cookie from server", "key", setCookie.Key)
		}
		b.setCookies(b.currentPage)
	}
	b.mu.Unlock()
	// Stream frames: content-focused draw (still dirty-region painted).
	if isStream {
		b.handle(b.buildDrawStream("stream"))
	} else {
		b.handle(b.buildDraw("get2"))
	}
}

func (b *ugglyBrowser) reportFetchError(err error, dest string, pq *pb.PageRequest) {
	name := ""
	if pq != nil {
		name = pq.Name
	}
	switch {
	case isDeadline(err):
		b.sendMessage(fmt.Sprintf("connection timeout to '%s'", dest), "get2-timeout")
	case isCancelled(err):
		b.sendMessage("connection cancelled", "get2-cancelled")
	case strings.Contains(err.Error(), "connection refused"):
		b.sendMessage("connection refused", "get2-refused")
	case strings.Contains(err.Error(), "error getting page from server"):
		b.sendMessage(fmt.Sprintf("error getting page '%s' from server", name), "get2-notfound")
	default:
		msg := fmt.Sprintf("error: %s", err.Error())
		if len(msg) > 80 {
			msg = msg[:80] + "…"
		}
		b.sendMessage(msg, "get2-error")
		loggo.Error("fetch error", "error", err.Error())
	}
	// Keep last good page; build a small local error banner page if nothing drawn
	b.mu.Lock()
	empty := b.currentPage == nil || b.currentPage.Name == ""
	b.mu.Unlock()
	if empty {
		b.showLocalErrorPage(dest, name, err)
	}
}

func (b *ugglyBrowser) showLocalErrorPage(dest, pageName string, err error) {
	w, h := b.vW, b.vH
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	body := fmt.Sprintf(
		"CONNECTION ERROR\n\nTarget: %s/%s\n\n%v\n\nPress F1 to enter a new address.\nF5 to retry.",
		dest, pageName, err,
	)
	page := buildStatus(body, w, h)
	page.Name = "uggcli-error"
	b.mu.Lock()
	b.setCurrentPage(page, true)
	b.mu.Unlock()
	_ = b.buildDraw("local-error")
}

// addCookiesTo attaches cookies using the provided context for metadata.
func (b *ugglyBrowser) addCookiesTo(ctx context.Context, pq *pb.PageRequest) *pb.PageRequest {
	// reuse existing addCookies but it returns (ctx, pq) — adapt
	_, pqc := b.addCookies(ctx, pq)
	return pqc
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

// processFormSubmission handles a form submit using data collected from the
// form instance that was actually polled. Do NOT re-lookup by name in b.forms:
// menu status redraws recreate address-bar/settings forms with DefaultValue
// from the previous session page, which would discard the user's typed input
// and "reload" the same (often errored) page.
func (b *ugglyBrowser) processFormSubmission(ctx context.Context, name string, data map[string]string, submitAction interface{}) {
	if data == nil {
		data = map[string]string{}
	}
	settingsSubmissionAuthorizedName := fmt.Sprintf("uggcli-settings-%s", localAuthUuid)
	loggo.Info("processFormSubmission", "name", name, "fields", len(data))

	if name == "address-bar" {
		l, err := b.processAddressBarInput(data)
		if err != nil {
			go b.sendMessage("error parsing UGRI", "process-form")
			return
		}
		loggo.Info("dialing form submitted server",
			"server", l.Server,
			"port", l.Port,
			"page", l.PageName,
			"secure", l.Secure,
		)
		// Navigate with a fresh page context; update session target first so
		// any concurrent menu rebuild shows the new address, not the old error.
		b.sess.setServer(l.Server, l.Port, l.Secure)
		b.sess.currPage = l.PageName
		b.get2(ctx, linkRequest(l))
		return
	}

	if name == settingsSubmissionAuthorizedName {
		loggo.Debug("detected settings submission")
		b.settingsProcess(data)
		return
	}

	// Page form: use SubmitAction from the polled form instance
	li := submitAction
	loggo.Debug("got mainbody form submission link")
	if li, ok := li.(*pb.Link); ok {
		l, _ := b.linkFiller(li)
		loggo.Debug("type assertion succeeded, getting link",
			"pageName", l.PageName,
			"server", l.Server,
			"port", l.Port,
		)
		pr := linkRequest(l)
		pr.FormData = []*pb.FormData{}
		fd := &pb.FormData{
			Name:        name,
			TextBoxData: []*pb.TextBoxData{},
		}
		for k, v := range data {
			td := pb.TextBoxData{
				Name:     k,
				Contents: v,
			}
			fd.TextBoxData = append(fd.TextBoxData, &td)
		}
		pr.FormData = append(pr.FormData, fd)
		b.cexJobs <- "page"
		pageCtx := <-b.cexOut
		go b.sendMessage("waiting for server (LLM may take a while)...", "form-submit")
		b.get2(pageCtx, pr)
		return
	}
	loggo.Warn("processFormSubmission: no handler", "name", name)
}

// passForm activates a form and blocks until the user submits (Enter) or
// cancels (Esc/F10). Poll runs on this goroutine so key events are not split
// across competing PollEvent loops.
func (b *ugglyBrowser) passForm(ctx context.Context, name string) {
	b.ensurePageForms()
	f := b.findForm(name)
	if f == nil {
		names := make([]string, 0, len(b.forms))
		for _, x := range b.forms {
			names = append(names, x.Name)
		}
		loggo.Warn("passForm: form not found", "name", name, "available", names)
		go b.sendMessage(fmt.Sprintf("form '%s' not found (have: %v)", name, names), "passForm")
		return
	}
	loggo.Info("passForm activating", "form", name)
	// Avoid sendMessage here — it rebuilds the menu and replaces the live form.
	// Status is updated after submit/dismiss instead.
	if err := f.Start(); err != nil {
		loggo.Error("form Start failed", "form", name, "err", err.Error())
		go b.sendMessage(fmt.Sprintf("form start failed: %s", err.Error()), "passForm")
		return
	}
	// buffered submit so Poll never blocks if we return early
	interrupt := make(chan struct{})
	submit := make(chan string, 1)
	b.formActive = name
	b.activeForm = f // pin the instance we poll so rebuilds can't steal Collect()
	// Run Poll inline (same event thread). Do not race with main PollEvent.
	f.Poll(ctx, interrupt, submit)
	// Collect from the polled instance BEFORE clearing formActive / menu rebuilds
	data := f.Collect()
	submitAction := f.SubmitAction
	b.activeForm = nil
	b.formActive = ""
	select {
	case formName := <-submit:
		loggo.Info("passForm: submit", "form", formName, "dataKeys", len(data))
		if v, ok := data["connstring"]; ok {
			loggo.Info("passForm: address-bar value", "connstring", v)
		}
		b.cexJobs <- "page"
		subCtx := <-b.cexOut
		b.processFormSubmission(subCtx, formName, data, submitAction)
	default:
		loggo.Info("passForm: dismissed without submit", "form", name)
	}
	loggo.Debug("passForm returned to main event loop")
}

// ensurePageForms reloads page forms from currentPage if missing (e.g. after races).
func (b *ugglyBrowser) ensurePageForms() {
	if b.currentPage == nil {
		return
	}
	if len(b.pageForms) > 0 {
		return
	}
	loggo.Warn("ensurePageForms: pageForms empty, rebuilding from currentPage")
	src := b.currentPageWire
	if src == nil {
		src = b.currentPage
	}
	page := clonePage(src)
	if page != nil {
		page = expandPage(page, b.vW, max(1, b.vH-b.menuHeight))
		b.currentPage = page
	}
	b.processPageForms(b.currentPage, false, "ensurePageForms")
}

func (b *ugglyBrowser) findForm(name string) *ugform.Form {
	for _, f := range b.forms {
		if f.Name == name {
			return f
		}
	}
	// also search pageForms directly
	for _, f := range b.pageForms {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// keyMatches reports whether the event activates the given keystroke string.
func keyMatches(ev *tcell.EventKey, want string) bool {
	if want == "" {
		return false
	}
	for k, v := range tcell.KeyNames {
		if v == want && ev.Key() == k {
			return true
		}
	}
	if ev.Key() == tcell.KeyRune && want == string(ev.Rune()) {
		return true
	}
	// case-insensitive single letter
	if ev.Key() == tcell.KeyRune && len(want) == 1 {
		if strings.EqualFold(want, string(ev.Rune())) {
			return true
		}
	}
	return false
}

// isFormActivationEvent true if this key would activate a form (not navigate).
func (b *ugglyBrowser) isFormActivationEvent(ev *tcell.EventKey) bool {
	for _, ks := range b.activeKeyStrokes {
		if !keyMatches(ev, ks.KeyStroke) {
			continue
		}
		if _, ok := ks.Action.(*pb.KeyStroke_FormActivation); ok {
			return true
		}
	}
	return false
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
			b.localLinkRouter(x.Link)
		} else {
			loggo.Debug("keyStrokeRouter starting async fetch")
			link, _ := b.linkFiller(x.Link)
			b.startFetchPage(linkRequest(link))
			localAuthUuid = uggo.NewUuid()
		}
	case *pb.KeyStroke_FormActivation:
		name := ""
		if x.FormActivation != nil {
			name = x.FormActivation.FormName
		}
		loggo.Info("detected form activation action, passing to passForm", "form", name)
		// Fresh non-cancelled context for the form session
		b.cexJobs <- "form"
		formCtx := <-b.cexOut
		b.passForm(formCtx, name)
	case *pb.KeyStroke_Event:
		evName := ""
		if x.Event != nil {
			evName = x.Event.GetName()
		}
		loggo.Debug("detected event action", "name", evName)
		// Local feed browser pagination (not sent to server)
		switch evName {
		case "feed_next":
			b.feedNext()
			return
		case "feed_prev":
			b.feedPrev()
			return
		}
		// During an active stream, inject input without tearing down GetPageStream.
		if b.streamConnected {
			b.injectStreamEvent(x.Event)
		} else {
			b.startFetchPage(b.eventRequest(x.Event))
			localAuthUuid = uggo.NewUuid()
		}
	case *pb.KeyStroke_DivScroll:
		if x.DivScroll == nil {
			return
		}
		name := x.DivScroll.GetDivName()
		down := x.DivScroll.GetDown()
		loggo.Debug("div scroll requested", "div", name, "down", down)
		b.applyDivScroll(name, down)
	}
}

// injectStreamEvent sends a PageRequest carrying an Event on the current
// session without cancelling the in-flight stream (no beginFetch / fetchGen bump).
// gRPC multiplexes this unary RPC alongside GetPageStream on the same connection.
// Query _input=1 asks servers to skip full render (input-only ack).
func (b *ugglyBrowser) injectStreamEvent(ev *pb.Event) {
	if ev == nil || b.exiting() || b.sess == nil {
		return
	}
	go func() {
		// Short deadline: input is best-effort; stream will resync state.
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		defer cancel()
		b.mu.Lock()
		w, h := b.vW, b.vH
		b.mu.Unlock()
		pq := b.eventRequest(ev)
		pq.ClientWidth = int32(w)
		pq.ClientHeight = int32(h)
		pq.Stream = false
		if pq.Query == nil {
			pq.Query = map[string]string{}
		}
		pq.Query["_input"] = "1"
		if strings.HasSuffix(pq.Name, "->") {
			pq.Name = strings.TrimSuffix(pq.Name, "->")
		}
		if pq.Name == "" || pq.Name == "home" {
			if b.sess.currPage != "" {
				pq.Name = strings.TrimSuffix(b.sess.currPage, "->")
			}
		}
		pq = b.addCookiesTo(ctx, pq)
		loggo.Debug("injectStreamEvent",
			"event", ev.GetName(),
			"page", pq.Name,
			"cookies", len(pq.SendCookies),
		)
		_, err := b.sess.get2(ctx, pq)
		if err != nil && !isCancelled(err) && !isDeadline(err) {
			loggo.Debug("injectStreamEvent failed", "event", ev.GetName(), "err", err.Error())
		}
	}()
}

// isStreamGameEvent true when this key would fire an Event while a page stream
// is live — those must not cancel the stream context.
func (b *ugglyBrowser) isStreamGameEvent(ev *tcell.EventKey) bool {
	if !b.streamConnected {
		return false
	}
	for _, ks := range b.activeKeyStrokes {
		if !keyMatches(ev, ks.KeyStroke) {
			continue
		}
		if _, ok := ks.Action.(*pb.KeyStroke_Event); ok {
			return true
		}
	}
	return false
}

// applyDivScroll shifts the named content box by one line and repaints.
// Offset is stored so subsequent expand/draw re-applies the scroll.
func (b *ugglyBrowser) applyDivScroll(divName string, down bool) {
	if divName == "" {
		return
	}
	b.mu.Lock()
	if b.divScrollY == nil {
		b.divScrollY = make(map[string]int)
	}
	if down {
		b.divScrollY[divName]++
	} else {
		if b.divScrollY[divName] > 0 {
			b.divScrollY[divName]--
		}
	}
	off := b.divScrollY[divName]
	// Live-adjust current contentExt if present
	var target *boxes.DivBox
	for _, bx := range b.contentExt {
		if bx != nil && bx.Name == divName {
			target = bx
			break
		}
	}
	b.mu.Unlock()
	if target != nil {
		// Re-expand from wire so scroll is applied cleanly from offset 0 baseline
		_ = b.buildDraw("divscroll")
		loggo.Debug("div scrolled", "div", divName, "offset", off)
		return
	}
	// No matching box yet — offset stored for next draw
	_ = b.buildDraw("divscroll")
}

func (b *ugglyBrowser) handleKeyStrokes(ctx context.Context, ev *tcell.EventKey) {
	keyLabel := ""
	if ev.Key() == tcell.KeyRune {
		keyLabel = string(ev.Rune())
	} else {
		_, keyLabel = detectSpecialKey(ev)
	}
	loggo.Debug("handleKeyStrokes", "key", keyLabel, "numKeyStrokes", len(b.activeKeyStrokes))
	// Prefer form activations first so "i" never races with a same-key link
	var formKS, otherKS []*pb.KeyStroke
	for _, ks := range b.activeKeyStrokes {
		if !keyMatches(ev, ks.KeyStroke) {
			continue
		}
		if _, ok := ks.Action.(*pb.KeyStroke_FormActivation); ok {
			formKS = append(formKS, ks)
		} else {
			otherKS = append(otherKS, ks)
		}
	}
	if len(formKS) > 0 {
		// Only activate the first matching form binding
		loggo.Debug("routing form activation key", "key", keyLabel, "form", formKS[0].GetFormActivation().GetFormName())
		b.keyStrokeRouter(ctx, formKS[0])
		return
	}
	for _, ks := range otherKS {
		loggo.Debug("routing keystroke", "key", keyLabel)
		b.keyStrokeRouter(ctx, ks)
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
	// Feed synthetic steps into the screen event queue so both the main
	// loop and form Poll() (autofocus) receive them uniformly.
	go b.feedSyntheticEvents()

	for {
		// Autofocus: activate form once after page draw (Prompt widgets, Form.autofocus)
		if b.autofocusForm != "" {
			name := b.autofocusForm
			b.autofocusForm = ""
			loggo.Info("autofocus activating form", "form", name)
			b.cexJobs <- "form"
			fctx := <-b.cexOut
			b.passForm(fctx, name)
			continue
		}
		loggo.Debug("polling and watching for keyStrokes", "keyStrokes", len(b.activeKeyStrokes))
		ev := b.view.PollEvent()
		switch ev := ev.(type) {
		case *fetchDoneEvent:
			b.applyFetchResult(ev)
		case *streamApplyEvent:
			b.applyStreamPending()
		case *statusOnlyEvent:
			// Empty msg used as toast-clear tick; also normal status updates
			if ev.msg == "" && b.toastMsg != "" && time.Now().After(b.toastUntil) {
				b.toastMsg = ""
				b.drawContent("toast-clear")
			} else if ev.msg != "" {
				b.applyStatusOnUI(ev.msg)
			} else if b.toastMsg != "" {
				// keep toast visible until expiry; force redraw
				b.drawContent("toast-keep")
			}
		case *resizeApplyEvent:
			b.applyResize()
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyF10:
				b.cexCancel <- "user-cancel"
				b.exit(0)
				return
			case tcell.KeyEscape:
				// Cancel in-flight fetch (loading state)
				if b.formActive == "" {
					b.fetchMu.Lock()
					if b.fetchCancel != nil {
						b.fetchCancel()
						b.sendMessage("fetch cancelled", "esc-cancel")
					}
					b.fetchMu.Unlock()
				}
				// also allow form Poll to see Esc via fallthrough only when form active
				if b.formActive == "" {
					continue
				}
				fallthrough
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
			case tcell.KeyF8:
				// Screenshot to disk (works during live streams; does not cancel stream)
				b.saveScreenshot()
			case tcell.KeyF9:
				// Toggle stream display pause (server may keep sending; UI freezes)
				b.toggleStreamPause()
			default:
				// Do NOT cancel context before form activation — that used to
				// kill the form session / hang Poll. Only cancel for navigation.
				// Stream game Event keys inject input without tearing down GetPageStream.
				if b.isFormActivationEvent(ev) {
					loggo.Debug("form activation key, skipping cexCancel")
					b.handleKeyStrokes(ctx, ev)
				} else if b.isStreamGameEvent(ev) {
					loggo.Debug("stream game event, keeping stream alive")
					b.handleKeyStrokes(ctx, ev)
				} else {
					b.cexCancel <- "user-cancel"
					b.handleKeyStrokes(ctx, ev)
				}
			}
		case *tcell.EventResize:
			b.view.Sync()
			b.scheduleResize(ctx)
		case fakeEvent:
			loggo.Debug("reloaded keyStrokes", "numKeyStrokes", len(b.activeKeyStrokes))
		}
	}
}

// applyStatusOnUI updates the status bar on the UI thread.
func (b *ugglyBrowser) applyStatusOnUI(msg string) {
	if b.exiting() {
		return
	}
	b.mu.Lock()
	b.messages = append(b.messages, &msg)
	if len(b.messages) > 50 {
		b.messages = b.messages[len(b.messages)-50:]
	}
	formBusy := b.formActive != ""
	b.mu.Unlock()
	if formBusy {
		return
	}
	// Prefer a cheap status-strip patch; fall back to full menu rebuild.
	if !b.patchStatusBar(msg) {
		b.buildContentMenu("statusOnly")
	}
}

// patchStatusBar rebuilds only the statusbar DivBox and paints the menu strip.
// Skips form/keystroke reprocessing. Returns false if menu is not ready yet.
func (b *ugglyBrowser) patchStatusBar(msg string) bool {
	b.mu.Lock()
	if len(b.contentMenu) == 0 || b.vW <= 0 {
		b.mu.Unlock()
		return false
	}
	w := b.vW
	menuH := b.menuHeight
	if menuH <= 0 {
		menuH = 3
	}
	b.mu.Unlock()

	strip := &pb.PageResponse{
		Name: "uggcli-status-patch",
		DivBoxes: &pb.DivBoxes{
			Boxes: []*pb.DivBox{{
				Name:     "uggcli-statusbar",
				Border:   false,
				FillChar: uggo.ConvertStringCharRune(" "),
				StartX:   0,
				StartY:   2,
				Width:    int32(w),
				Height:   1,
				FillSt:   uggo.Style("white", "white"),
			}},
		},
		Elements: &pb.Elements{
			TextBlobs: []*pb.TextBlob{{
				Content:  msg,
				Wrap:     true,
				Style:    uggo.Style("black", "white"),
				DivNames: []string{"uggcli-statusbar"},
			}},
		},
	}
	patched, err := convertPageBoxes(strip)
	if err != nil || len(patched) == 0 {
		return false
	}
	b.mu.Lock()
	replaced := false
	for i, mb := range b.contentMenu {
		if mb != nil && mb.Name == "uggcli-statusbar" {
			b.contentMenu[i] = patched[0]
			replaced = true
			break
		}
	}
	// Fallback: status is typically the last menu box
	if !replaced && len(b.contentMenu) >= 3 {
		b.contentMenu[2] = patched[0]
		replaced = true
	}
	b.mu.Unlock()
	if !replaced {
		return false
	}
	b.drawMenuStrip()
	return true
}

// drawMenuStrip paints only contentMenu (top chrome) via dirty-region diff,
// leaving page content cells untouched in the front buffer.
func (b *ugglyBrowser) drawMenuStrip() {
	if b.exiting() || b.view == nil {
		return
	}
	b.mu.Lock()
	menu := make([]*boxes.DivBox, len(b.contentMenu))
	copy(menu, b.contentMenu)
	forms := make([]*ugform.Form, len(b.menuForms))
	copy(forms, b.menuForms)
	vW, vH := b.vW, b.vH
	b.mu.Unlock()

	if sw, sh := b.view.Size(); sw > 0 && sh > 0 {
		vW, vH = sw, sh
	}
	if vW <= 0 || vH <= 0 {
		return
	}
	// Start from last flushed frame so page body cells stay identical and skip.
	frame := b.paint.beginFromFront(vW, vH)
	for _, bi := range menu {
		frame.stampBox(bi)
	}
	written, skipped := b.paint.flushDiff(b.view)
	for _, f := range forms {
		if f != nil {
			f.Start()
		}
	}
	b.view.Show()
	loggo.Debug("drawMenuStrip dirty", "written", written, "skipped", skipped)
}

// resizeApplyEvent is posted after debounce so refresh runs on the UI thread.
type resizeApplyEvent struct{}

func (e *resizeApplyEvent) When() time.Time { return time.Now() }

// scheduleResize debounces resize to a single pending refresh on the UI thread.
func (b *ugglyBrowser) scheduleResize(ctx context.Context) {
	_ = ctx
	b.mu.Lock()
	if b.resizePending || b.resizing {
		b.mu.Unlock()
		return
	}
	b.resizePending = true
	b.mu.Unlock()
	go func() {
		time.Sleep(b.resizeDelay)
		if b.exiting() || b.view == nil {
			b.mu.Lock()
			b.resizePending = false
			b.mu.Unlock()
			return
		}
		// Deliver to pollEvents so paint stays single-threaded.
		_ = b.view.PostEvent(&resizeApplyEvent{})
	}()
}

func (b *ugglyBrowser) applyResize() {
	b.mu.Lock()
	b.resizePending = false
	if b.view != nil {
		w, h := b.view.Size()
		b.vW, b.vH = w, h
		if b.sess != nil {
			b.sess.clientWidth = int32(w)
			b.sess.clientHeight = int32(h)
		}
	}
	b.mu.Unlock()
	// Force full repaint after geometry change
	b.paint.invalidate()
	if b.exiting() {
		return
	}
	// refresh starts async get2 for remote pages; local rebuilds stay on UI thread
	b.refresh(context.Background())
}

// feedSyntheticEvents posts -key/-delay steps onto the tcell event queue.
// Using PostEvent means form Poll() (used for autofocus prompts) receives
// the same synthetic keystrokes as the main browser loop.
func (b *ugglyBrowser) feedSyntheticEvents() {
	for i, step := range b.syntheticSteps {
		if step.wait > 0 {
			loggo.Debug("synthetic delay", "duration", step.wait, "index", i)
			time.Sleep(step.wait)
		}
		if step.ev != nil {
			loggo.Debug("posting synthetic event", "index", i)
			// retry if event queue is full
			for tries := 0; tries < 50; tries++ {
				if err := b.view.PostEvent(step.ev); err == nil {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		b.synthStepIndex = i + 1
	}
}

func linkRequest(in *pb.Link) *pb.PageRequest {
	if in == nil {
		return &pb.PageRequest{}
	}
	pr := &pb.PageRequest{
		Name:   in.PageName,
		Server: in.Server,
		Port:   in.Port,
		Secure: in.Secure,
		Stream: in.Stream,
		Query:  in.Query,
		Event:  in.Event,
	}
	return pr
}

// eventRequest builds a PageRequest that posts an Event on the current page.
func (b *ugglyBrowser) eventRequest(ev *pb.Event) *pb.PageRequest {
	return &pb.PageRequest{
		Name:   b.sess.currPage,
		Server: b.sess.server,
		Port:   b.sess.port,
		Secure: b.sess.secure,
		Event:  ev,
	}
}

// linkFiller takes a potentially partial Link and
// tries to fill in all of the properties using context
// from the current server session
func (b *ugglyBrowser) linkFiller(partial *pb.Link) (*pb.Link, error) {
	var err error
	var full pb.Link
	if partial == nil {
		return &full, err
	}
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
	// Preserve structured navigation fields from the server
	if len(partial.Query) > 0 {
		full.Query = partial.Query
	}
	if partial.Event != nil && partial.Event.Name != "" {
		full.Event = partial.Event
	}
	full.KeyStroke = partial.KeyStroke
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
	b.vH = h - b.menuHeight
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
		case *pb.KeyStroke_Event:
			loggo.Debug("found event action on page", "event", x.Event.GetName())
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
	return b.buildDrawOpts(label, false)
}

// buildDrawStream paints a stream frame: re-expand content, avoid form teardown.
func (b *ugglyBrowser) buildDrawStream(label string) (err error) {
	return b.buildDrawOpts(label, true)
}

func (b *ugglyBrowser) buildDrawOpts(label string, streamFrame bool) (err error) {
	label = fmt.Sprintf("%s-buildDraw", label)
	// Clone from wire snapshot (or current) so expand is idempotent and
	// re-layout on resize gets original Tables/Lists/Prompts.
	b.mu.Lock()
	src := b.currentPageWire
	if src == nil {
		src = b.currentPage
	}
	vW, vH, menuH := b.vW, b.vH, b.menuHeight
	scrollY := make(map[string]int, len(b.divScrollY))
	for k, v := range b.divScrollY {
		scrollY[k] = v
	}
	formBusy := b.formActive != ""
	b.mu.Unlock()

	page := clonePage(src)
	if page != nil {
		if page.Error != nil && page.Error.Message != "" && !streamFrame {
			// Structured error page (protocol PageError) — full local chrome
			go b.sendMessage(page.Error.Message, "page-error")
			errPage := buildPageErrorPage(vW, vH-menuH, page.Name, page.Error)
			// Keep wire as original error-bearing page; draw the local error view
			page = errPage
			b.mu.Lock()
			b.currentPage = errPage
			b.currentPageLocal = errPage
			b.mu.Unlock()
		}
		page = expandPage(page, vW, vH-menuH)
	}
	ext, err := convertPageBoxes(page)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		return err
	}
	// Apply DivScroll offsets into box pixel maps (client-side).
	for _, bx := range ext {
		if bx == nil {
			continue
		}
		if off, ok := scrollY[bx.Name]; ok && off != 0 {
			boxes.ScrollDivBox(bx, off)
		}
	}
	// check for empty content to give a user a more helpful explanation
	totalBoxes := len(ext)
	emptyBoxes := 0
	for _, box := range ext {
		if len(box.RawContents) == 0 {
			emptyBoxes += 1
		}
	}
	if totalBoxes == emptyBoxes && !streamFrame {
		go b.sendMessage("all content empty...", "notifyEmptyBoxes")
	}

	autofocus := ""
	b.mu.Lock()
	b.contentExt = ext
	// Expanded page becomes current so menu rebuild can re-parse list keystrokes.
	// Wire snapshot is left intact for the next expand.
	if page != nil {
		b.currentPage = page
		if streamFrame {
			// P0: bind forms/keys once per stream session — game keystrokes are stable.
			if !b.streamKeysBound && !formBusy {
				b.processPageForms(page, false, label)
				b.parseKeyStrokes(page, false)
				b.streamKeysBound = true
			}
		} else {
			b.streamKeysBound = false
			if !formBusy {
				b.processPageForms(page, false, label)
			}
			b.parseKeyStrokes(page, false)
			if page.Elements != nil {
				for _, form := range page.Elements.Forms {
					if form.GetAutofocus() {
						autofocus = form.Name
						break
					}
				}
			}
		}
	}
	needMenu := len(b.contentMenu) == 0
	b.mu.Unlock()
	if needMenu {
		b.buildContentMenu(label + "-initmenu")
	}

	// Dirty-region paint — no view.Clear(); compositor blanks + diffs.
	b.drawContent(label)
	if autofocus != "" {
		b.autofocusForm = autofocus
	}
	b.setPageReady(true)
	return err
}

// setCurrentPage stores a new page and a wire snapshot for re-expand.
func (b *ugglyBrowser) setCurrentPage(page *pb.PageResponse, local bool) {
	b.currentPage = page
	b.currentPageWire = clonePage(page)
	if local {
		b.currentPageLocal = page
	} else {
		b.currentPageLocal = nil
	}
}

// drawContent composites menu + page boxes into the paint buffer and flushes
// only cells that changed since the last frame (dirty-region).
func (b *ugglyBrowser) drawContent(label string) {
	if b.exiting() {
		return
	}
	loggo.Debug("drawing content", "label", label)
	// Snapshot under lock; paint outside lock so we never hold mu during SetContent.
	b.mu.Lock()
	menuH := b.menuHeight
	vW, vH := b.vW, b.vH
	menu := make([]*boxes.DivBox, len(b.contentMenu))
	copy(menu, b.contentMenu)
	ext := make([]*boxes.DivBox, len(b.contentExt))
	copy(ext, b.contentExt)
	forms := make([]*ugform.Form, len(b.forms))
	copy(forms, b.forms)
	b.mu.Unlock()

	if b.view == nil {
		return
	}
	// Prefer live screen size for the compositor.
	if sw, sh := b.view.Size(); sw > 0 && sh > 0 {
		vW, vH = sw, sh
	}
	if vW <= 0 {
		vW = 80
	}
	if vH <= 0 {
		vH = 24
	}

	frame := b.paint.begin(vW, vH)
	// Menu chrome
	for _, bi := range menu {
		frame.stampBox(bi)
	}
	// Page content shifted down by menu height
	for _, bi := range ext {
		if bi == nil {
			continue
		}
		bj := *bi
		bj.StartY += menuH
		frame.stampBox(&bj)
	}

	// Toast overlay (screenshot path, pause notice) — stamped after page content
	b.stampToast(frame, vW, vH)

	written, skipped := b.paint.flushDiff(b.view)
	// draw forms on top of canvas (forms use their own SetContent)
	for _, f := range forms {
		loggo.Debug("starting form", "formName", f.Name)
		f.Start()
	}
	b.view.Show()
	if b.emitKeystrokes || b.keystrokesFile != "" {
		b.exportKeystrokes()
	}
	loggo.Debug("draw stats",
		"label", label,
		"forms", len(forms), "menuBoxes", len(menu),
		"extBoxes", len(ext),
		"cellsWritten", written, "cellsSkipped", skipped,
		"dirtyPct", dirtyPercent(written, skipped))
}

// stampToast draws a centered popup if a toast is active.
func (b *ugglyBrowser) stampToast(frame *paintFrame, vW, vH int) {
	if b.toastMsg == "" || time.Now().After(b.toastUntil) {
		if b.toastMsg != "" && time.Now().After(b.toastUntil) {
			b.toastMsg = ""
		}
		return
	}
	msg := b.toastMsg
	// Word-wrap toast to fit width
	maxW := vW - 6
	if maxW < 20 {
		maxW = vW
	}
	if maxW < 1 {
		return
	}
	lines := wrapToastLines(msg, maxW)
	boxW := 0
	for _, ln := range lines {
		if len(ln) > boxW {
			boxW = len(ln)
		}
	}
	boxW += 4
	if boxW > vW {
		boxW = vW
	}
	boxH := len(lines) + 2
	if boxH > vH {
		boxH = vH
	}
	x0 := (vW - boxW) / 2
	y0 := (vH - boxH) / 2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	stBorder := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow)
	stFill := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow)
	// fill
	for y := 0; y < boxH; y++ {
		for x := 0; x < boxW; x++ {
			ch := ' '
			if y == 0 || y == boxH-1 || x == 0 || x == boxW-1 {
				ch = '*'
			}
			frame.put(x0+x, y0+y, ch, stBorder)
		}
	}
	for i, ln := range lines {
		if i+1 >= boxH-1 {
			break
		}
		for j, r := range ln {
			if j+2 >= boxW {
				break
			}
			frame.put(x0+2+j, y0+1+i, r, stFill)
		}
	}
}

func wrapToastLines(msg string, maxW int) []string {
	if maxW < 8 {
		maxW = 8
	}
	words := strings.Fields(msg)
	if len(words) == 0 {
		return []string{msg}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) <= maxW {
			cur = cur + " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	lines = append(lines, cur)
	// hard-break any remaining long tokens
	var out []string
	for _, ln := range lines {
		for len(ln) > maxW {
			out = append(out, ln[:maxW])
			ln = ln[maxW:]
		}
		out = append(out, ln)
	}
	return out
}

// showToast displays a centered popup for a few seconds (UI thread).
func (b *ugglyBrowser) showToast(msg string, secs float64) {
	if secs <= 0 {
		secs = 4
	}
	b.toastMsg = msg
	b.toastUntil = time.Now().Add(time.Duration(secs * float64(time.Second)))
	// Redraw current content with toast (no refetch)
	if b.view != nil && !b.exiting() {
		b.drawContent("toast")
		// Schedule clear
		go func() {
			time.Sleep(time.Duration(secs * float64(time.Second)))
			if b.exiting() {
				return
			}
			// Post a status event so UI thread clears toast and redraws
			_ = b.view.PostEvent(&statusOnlyEvent{msg: ""})
		}()
	}
}

func (b *ugglyBrowser) toggleStreamPause() {
	if !b.streamConnected {
		b.sendMessage("nothing streaming to pause (F9)", "pause")
		return
	}
	b.streamPaused = !b.streamPaused
	if b.streamPaused {
		b.sendMessage("stream PAUSED (F9 resume, F8 screenshot)", "pause")
		b.showToast("STREAM PAUSED — F9 resume · F8 save screenshot", 3)
	} else {
		b.sendMessage("stream resumed", "pause")
		b.showToast("stream resumed", 1.5)
	}
}

// saveScreenshot captures the current terminal buffer to a timestamped file
// and shows a popup with the absolute path.
func (b *ugglyBrowser) saveScreenshot() {
	text, err := b.CaptureScreenText()
	if err != nil {
		b.sendMessage("screenshot failed: "+err.Error(), "screenshot")
		b.showToast("screenshot failed: "+err.Error(), 4)
		return
	}
	if text == "" {
		b.sendMessage("screenshot empty", "screenshot")
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = os.TempDir()
	}
	name := fmt.Sprintf("uggly-screenshot-%s.txt", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if err := os.WriteFile(abs, []byte(text), 0644); err != nil {
		b.sendMessage("screenshot write failed", "screenshot")
		b.showToast("screenshot write failed: "+err.Error(), 4)
		return
	}
	loggo.Info("screenshot saved", "path", abs)
	b.sendMessage("screen saved to "+abs, "screenshot")
	b.showToast("screen saved to "+abs, 5)
}

func dirtyPercent(written, skipped int) int {
	t := written + skipped
	if t == 0 {
		return 0
	}
	return (written * 100) / t
}

type ugglyBrowser struct {
	view             tcell.Screen
	contentMenu      []*boxes.DivBox
	forms            []*ugform.Form  // pageForms + menuForms (composed)
	pageForms        []*ugform.Form  // forms from the current external page
	menuForms        []*ugform.Form  // forms from the local menu chrome
	contentExt       []*boxes.DivBox // e.g., non-menu content
	currentPage      *pb.PageResponse
	currentPageWire  *pb.PageResponse // pre-expand snapshot for safe re-expand
	currentPageLocal *pb.PageResponse // so we don't get from external
	interrupt        chan struct{}
	sess             *session    // gRPC stuff buried in session.go
	messages         []*string   // messages accessed from here
	messageBuffer    chan string // buffer mostly used as trigger/stack
	resizeBuffer     chan int    // buffers resize events
	resizing         bool        // locks out other resize attempts
	resizePending    bool        // debounce: a resize refresh is already scheduled
	resizeDelay      time.Duration
	activeKeyStrokes []*pb.KeyStroke
	menuKeyStrokes   []*pb.KeyStroke
	cookies          map[string][]*pb.Cookie // all cookies stored for each server string
	menuHeight       int
	exitFlag         bool
	exitOnce         sync.Once // ensure exit runs exactly once
	exitAtomic       int32     // atomic exit flag for non-UI goroutines
	pageReadyAtomic  int32     // atomic pageReady for headless timer
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
	emitKeystrokes  bool    // emit keystrokes as JSON (headless only)
	keystrokesFile  string  // file path for keystroke JSON output
	pageReady       bool    // true after at least one successful buildDraw
	autofocusForm   string  // form name to activate after page draw
	formActive      string  // non-empty while passForm is polling a form
	activeForm      *ugform.Form // pinned form instance being polled
	// control socket fields
	controlServer   net.Listener
	controlCancel   context.CancelFunc
	controlPort     int // actual port if using TCP with port 0
	// concurrency / async fetch
	mu          sync.Mutex        // protects page/content/forms/dimensions shared state
	fetchMu     sync.Mutex        // protects fetchCancel
	fetchCancel context.CancelFunc
	fetchGen    uint64 // incremented on each navigation; stale results discarded
	// dirty-region compositor (UI thread only)
	paint paintFramePair
	// stream: suppress repeated "connected!" status storms
	streamConnected bool
	streamPaused    bool // F9: freeze applying stream frames (for reading / capture)
	// Coalesced stream frames (UI thread drains via streamApplyEvent)
	pendingStream     *fetchDoneEvent
	streamPostPending bool
	streamKeysBound   bool // skip re-parse keystrokes after first stream frame
	// toast overlay (e.g. screenshot path)
	toastMsg   string
	toastUntil time.Time
	// feed browser pagination
	feedListings []*pb.PageListing
	feedOffset   int
	// per-div vertical scroll offsets (line shift into HiddenContents)
	divScrollY map[string]int
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
	b.resizeBuffer = make(chan int, 8)
	// Buffered so safeSendMessage can coalesce without blocking UI
	b.messageBuffer = make(chan string, 16)
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
	b.divScrollY = make(map[string]int)
	b.paint.forceFull = true
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
	w, h := b.view.Size()
	b.vW = w
	b.vH = h - b.menuHeight
	b.headless = headless
	b.autoExit = *autoExit
	b.exitDelay = *timeoutF
	b.headlessCapture = *output
	b.emitKeystrokes = *emitKeystrokes && headless
	b.keystrokesFile = *keystrokesOutput
	b.controlPort = *controlPort

	if headless {
		go b.startHeadlessTimer()
	}

	// Start control server if requested
	if *controlSocket != "" || *controlPort > 0 {
		if err := b.startControlServer(); err != nil {
			loggo.Error("failed to start control server", "error", err)
			// Don't fail the entire startup, just log the error
		}
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
	// 1. Single-character tokens are always keys (digits 0-9, letters, etc.).
	// Bare floats like "1" must NOT be treated as a 1s delay — that broke
	// headless tests for detail rows bound to keys 1-9.
	if len([]rune(raw)) == 1 {
		if ev, err := synthesizeEvent(raw); err == nil {
			return synthStep{ev: ev}
		}
	}
	// 2. Try as a Go duration string ("2s", "500ms")
	if d, err := time.ParseDuration(raw); err == nil {
		return synthStep{wait: d}
	}
	// 3. Bare multi-char number as seconds ("1.5", "2") — require a decimal
	// or multi-digit so "1" remains a key via step 1.
	if strings.Contains(raw, ".") {
		if n, err := strconv.ParseFloat(raw, 64); err == nil && n >= 0 {
			return synthStep{wait: time.Duration(n * float64(time.Second))}
		}
	}
	// 4. Multi-char key names (Enter, Tab, F5, …)
	if ev, err := synthesizeEvent(raw); err == nil {
		return synthStep{ev: ev}
	}
	// 4. Common aliases
	aliases := map[string]string{
		"Escape": "Esc", "ESC": "Esc", "escape": "Esc",
		"Return": "Enter", "return": "Enter",
	}
	if alt, ok := aliases[raw]; ok {
		if ev, err := synthesizeEvent(alt); err == nil {
			return synthStep{ev: ev}
		}
	}
	// 5. Can't parse → warn and skip (loggo may be nil during early main)
	if loggo != nil {
		loggo.Warn("unrecognized step value",
			"value", raw,
			"hint", "use a duration e.g. '2s'/'0.5s' or a key e.g. 'Enter'/'Tab'/'a'/'Esc'")
	}
	return synthStep{}
}

// KeystrokeExport represents machine-readable output of available actions
type KeystrokeExport struct {
	Page       string           `json:"page"`
	Server     string           `json:"server"`
	Port       string           `json:"port"`
	Secure     bool             `json:"secure"`
	Keystrokes []KeystrokeInfo  `json:"keystrokes"`
	Forms      []FormInfo       `json:"forms,omitempty"`
	Timestamp  string           `json:"timestamp"`
}

type KeystrokeInfo struct {
	Key    string `json:"key"`
	Type   string `json:"type"` // "link", "form", "scroll"
	Target string `json:"target,omitempty"`
	Server string `json:"server,omitempty"`
	Port   string `json:"port,omitempty"`
	Secure bool   `json:"secure,omitempty"`
	Stream bool   `json:"stream,omitempty"`
}

type FormInfo struct {
	Name      string   `json:"name"`
	DivName   string   `json:"div"`
	TextBoxes []string `json:"textboxes"`
}

// ControlRequest represents a command from the control client
type ControlRequest struct {
	Action string `json:"action"` // "status", "key", "capture", "exit", "delay"
	Key    string `json:"key,omitempty"`
	Delay  int    `json:"delay,omitempty"` // delay in milliseconds
	ID     string `json:"id,omitempty"`    // request correlation ID
}

// ControlResponse represents a response to the control client
type ControlResponse struct {
	ID         string           `json:"id,omitempty"`
	Success    bool             `json:"success"`
	Error      string           `json:"error,omitempty"`
	Page       string           `json:"page,omitempty"`
	Server     string           `json:"server,omitempty"`
	Port       string           `json:"port,omitempty"`
	Secure     bool             `json:"secure,omitempty"`
	Keystrokes []KeystrokeInfo  `json:"keystrokes,omitempty"`
	Screen     string           `json:"screen,omitempty"`
	Message    string           `json:"message,omitempty"`
}

// startControlServer starts the control socket/server for interactive mode
func (b *ugglyBrowser) startControlServer() error {
	var listener net.Listener
	var err error

	ctx, cancel := context.WithCancel(context.Background())
	b.controlCancel = cancel

	if *controlSocket != "" {
		// Unix socket
		os.Remove(*controlSocket) // cleanup existing socket
		listener, err = net.Listen("unix", *controlSocket)
		if err != nil {
			return fmt.Errorf("failed to create unix socket: %w", err)
		}
		loggo.Info("control server started", "socket", *controlSocket)
	} else if *controlPort > 0 {
		// TCP
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", *controlPort))
		if err != nil {
			return fmt.Errorf("failed to create TCP listener: %w", err)
		}
		// If port was 0, get the actual port
		if *controlPort == 0 {
			b.controlPort = listener.Addr().(*net.TCPAddr).Port
		}
		loggo.Info("control server started", "port", b.controlPort)
	} else {
		return nil // no control server requested
	}

	b.controlServer = listener

	// Start accepting connections
	go func() {
		for {
			select {
			case <-ctx.Done():
				listener.Close()
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					if !strings.Contains(err.Error(), "use of closed network connection") {
						loggo.Error("control accept error", "error", err)
					}
					continue
				}
				go b.handleControlConn(conn)
			}
		}
	}()

	return nil
}

// handleControlConn handles a single control connection
func (b *ugglyBrowser) handleControlConn(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req ControlRequest
		if err := decoder.Decode(&req); err != nil {
			if err.Error() != "EOF" {
				loggo.Error("control decode error", "error", err)
			}
			return
		}

		resp := ControlResponse{
			ID:      req.ID,
			Success: true,
		}

		switch req.Action {
		case "status":
			// Return current page state and available keystrokes
			resp.Page = b.sess.currPage
			if resp.Page == "" {
				resp.Page = "home"
			}
			resp.Server = b.sess.server
			resp.Port = b.sess.port
			resp.Secure = b.sess.secure
			resp.Keystrokes = make([]KeystrokeInfo, 0, len(b.activeKeyStrokes))
			for _, ks := range b.activeKeyStrokes {
				ki := KeystrokeInfo{Key: ks.KeyStroke}
				switch action := ks.Action.(type) {
				case *pb.KeyStroke_Link:
					ki.Type = "link"
					if action.Link != nil {
						ki.Target = action.Link.PageName
						ki.Server = action.Link.Server
						ki.Port = action.Link.Port
						ki.Secure = action.Link.Secure
						ki.Stream = action.Link.Stream
					}
				case *pb.KeyStroke_FormActivation:
					ki.Type = "form"
					if action.FormActivation != nil {
						ki.Target = action.FormActivation.FormName
					}
				case *pb.KeyStroke_Event:
					ki.Type = "event"
					if action.Event != nil {
						ki.Target = action.Event.Name
					}
				case *pb.KeyStroke_DivScroll:
					ki.Type = "scroll"
					if action.DivScroll != nil {
						ki.Target = action.DivScroll.DivName
					}
				}
				resp.Keystrokes = append(resp.Keystrokes, ki)
			}
			resp.Message = fmt.Sprintf("Page: %s, %d keystrokes available", resp.Page, len(resp.Keystrokes))

		case "key":
			// Inject a keystroke
			if req.Key == "" {
				resp.Success = false
				resp.Error = "key field required"
			} else {
				if ev, err := synthesizeEvent(req.Key); err == nil {
					b.view.PostEvent(ev)
					resp.Message = fmt.Sprintf("Posted key: %s", req.Key)
				} else {
					resp.Success = false
					resp.Error = err.Error()
				}
			}

		case "capture":
			// Capture current screen
			b.view.Show()
			time.Sleep(100 * time.Millisecond)
			screen, err := b.ScreenOutput()
			if err != nil {
				resp.Success = false
				resp.Error = err.Error()
			} else {
				// Escape the screen content to ensure valid JSON
				resp.Screen = strings.ReplaceAll(screen, "\n", "\\n")
				resp.Screen = strings.ReplaceAll(resp.Screen, "\"", "\\\"")
				resp.Message = fmt.Sprintf("Captured %d bytes", len(screen))
			}

		case "delay":
			// Wait for specified milliseconds
			ms := req.Delay
			if ms <= 0 {
				ms = 1000 // default 1 second
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			resp.Message = fmt.Sprintf("Delayed %dms", ms)

		case "exit":
			// Trigger exit
			b.postF10()
			resp.Message = "Exit triggered"

		default:
			resp.Success = false
			resp.Error = fmt.Sprintf("unknown action: %s", req.Action)
		}

		if err := encoder.Encode(resp); err != nil {
			loggo.Error("control encode error", "error", err)
			return
		}
	}
}

// exportKeystrokes outputs available keystrokes as JSON to file or stderr
func (b *ugglyBrowser) exportKeystrokes() {
	if !b.emitKeystrokes && b.keystrokesFile == "" {
		return
	}

	export := KeystrokeExport{
		Page:       b.sess.currPage,
		Server:     b.sess.server,
		Port:       b.sess.port,
		Secure:     b.sess.secure,
		Keystrokes: make([]KeystrokeInfo, 0, len(b.activeKeyStrokes)),
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	for _, ks := range b.activeKeyStrokes {
		ki := KeystrokeInfo{Key: ks.KeyStroke}

		switch action := ks.Action.(type) {
		case *pb.KeyStroke_Link:
			ki.Type = "link"
			if action.Link != nil {
				ki.Target = action.Link.PageName
				ki.Server = action.Link.Server
				ki.Port = action.Link.Port
				ki.Secure = action.Link.Secure
				ki.Stream = action.Link.Stream
			}
		case *pb.KeyStroke_FormActivation:
			ki.Type = "form"
			if action.FormActivation != nil {
				ki.Target = action.FormActivation.FormName
			}
		case *pb.KeyStroke_Event:
			ki.Type = "event"
			if action.Event != nil {
				ki.Target = action.Event.Name
			}
		case *pb.KeyStroke_DivScroll:
			ki.Type = "scroll"
			if action.DivScroll != nil {
				ki.Target = action.DivScroll.DivName
			}
		}

		export.Keystrokes = append(export.Keystrokes, ki)
	}

	// Export forms - note: textBoxes is private in ugform.Form
	// so we just export the form name for now
	for _, f := range b.forms {
		fi := FormInfo{Name: f.Name}
		// TODO: Add method to ugform.Form to expose textbox names
		export.Forms = append(export.Forms, fi)
	}

	jsonData, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		loggo.Error("failed to marshal keystrokes", "error", err)
		return
	}

	if b.keystrokesFile != "" {
		// Write to file (works in both headless and interactive modes)
		if err := os.WriteFile(b.keystrokesFile, jsonData, 0644); err != nil {
			loggo.Error("failed to write keystrokes file", "error", err, "file", b.keystrokesFile)
		} else {
			loggo.Info("keystrokes written to file", "file", b.keystrokesFile)
		}
	} else if b.emitKeystrokes {
		// Fallback: stderr for headless mode with old flag
		fmt.Fprintf(os.Stderr, "\n---KEystrokes---\n%s\n---END---\n", string(jsonData))
	}
}

// CaptureScreenText captures the current screen (interactive or headless) as
// ANSI-colored text. Works with SimulationScreen and real tcell screens.
func (b *ugglyBrowser) CaptureScreenText() (string, error) {
	if b.view == nil {
		return "", fmt.Errorf("no screen")
	}
	// Prefer SimulationScreen bulk API when available (headless)
	if sim, ok := b.view.(tcell.SimulationScreen); ok {
		return b.screenOutputFromSim(sim)
	}
	return b.screenOutputFromGetContent()
}

// ScreenOutput is the legacy headless name for CaptureScreenText.
func (b *ugglyBrowser) ScreenOutput() (string, error) {
	return b.CaptureScreenText()
}

func (b *ugglyBrowser) screenOutputFromSim(sim tcell.SimulationScreen) (string, error) {
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
			if len(c.Runes) == 0 && len(c.Bytes) > 0 {
				c.Runes = []rune(string(c.Bytes))
			}
			if len(c.Runes) == 0 {
				c.Runes = []rune{' '}
			}
			r := c.Runes[0]
			fg, bg, attrs := c.Style.Decompose()
			appendANSICell(&buf, r, fg, bg, attrs, &lastFg, &lastBg, &lastAttrs)
		}
		if y < h-1 {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString("\033[0m\n")
	return buf.String(), nil
}

func (b *ugglyBrowser) screenOutputFromGetContent() (string, error) {
	w, h := b.view.Size()
	if w == 0 || h == 0 {
		return "", nil
	}
	var buf strings.Builder
	var lastFg, lastBg uint8
	var lastAttrs tcell.AttrMask
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mainc, _, style, _ := b.view.GetContent(x, y)
			if mainc == 0 {
				mainc = ' '
			}
			fg, bg, attrs := style.Decompose()
			appendANSICell(&buf, mainc, fg, bg, attrs, &lastFg, &lastBg, &lastAttrs)
		}
		if y < h-1 {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString("\033[0m\n")
	return buf.String(), nil
}

func appendANSICell(buf *strings.Builder, r rune, fg, bg tcell.Color, attrs tcell.AttrMask, lastFg, lastBg *uint8, lastAttrs *tcell.AttrMask) {
	fg8 := uint8(fg)
	bg8 := uint8(bg)
	if fg8 != *lastFg || bg8 != *lastBg || attrs != *lastAttrs {
		buf.WriteString("\033[0m")
		buf.WriteString(fmt.Sprintf("\033[38;5;%dm", rgbTo256(fg)))
		buf.WriteString(fmt.Sprintf("\033[48;5;%dm", rgbTo256(bg)))
		if attrs&tcell.AttrBold != 0 {
			buf.WriteString("\033[1m")
		}
		if attrs&tcell.AttrDim != 0 {
			buf.WriteString("\033[2m")
		}
		if attrs&tcell.AttrUnderline != 0 {
			buf.WriteString("\033[4m")
		}
		if attrs&tcell.AttrReverse != 0 {
			buf.WriteString("\033[7m")
		}
		*lastFg, *lastBg, *lastAttrs = fg8, bg8, attrs
	}
	buf.WriteRune(r)
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
