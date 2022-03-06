package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"github.com/AlecAivazis/survey/v2"
	"google.golang.org/grpc"
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

func buildMenuSite(width, height int) (*pb.SiteResponse) {
	// since we already have functions for converting to divboxes
	// we'll just build a local siteResponse
	localSite := pb.SiteResponse{
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
	localSite.DivBoxes.Boxes = append(localSite.DivBoxes.Boxes, &menuBar)
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
	localSite.Elements.TextBlobs = append(localSite.Elements.TextBlobs, &menuContent)
	linkBrowse := &pb.Link{
		KeyStroke: "F1",
		SiteName: "basic",
		Server: "",
		Port: int32(10000),
	}
	localSite.Links = append(localSite.Links, linkBrowse)
	return &localSite
}

func injectMenu(screen tcell.Screen, bis []*boxes.DivBox) (boxesWithMenu []*boxes.DivBox, links []*link, err error) {
	// makes boxes for the uggcli menu top bar
	screenWidth, _ := screen.Size() // returns width, height
	menuHeight := 1
	localSite := buildMenuSite(screenWidth, menuHeight)
	links, err = parseLinks(localSite)
	if err != nil {
		return boxesWithMenu, links, err
	}
	boxesWithMenu, _ = compileBoxes(localSite)
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


func promptSites(feed *pb.FeedResponse) (siteName string, err error) {
	var sites []string
	for _, site := range(feed.Sites) {
		fmt.Println(site.Name)
		sites = append(sites, site.Name)
	}
	loggo.Info("got sites", "len", len(sites))
	if len(sites) < 1 {
		err = errors.New("no sites returned from server feed")
		return siteName, err
	}
	if len(sites) == 1 {
		siteName = sites[0]
		return siteName, err
	}
	prompt := &survey.Select{
		Message: "Select a site from the server: ",
		Options: sites,
	}
	err = survey.AskOne(prompt, &siteName)
	return siteName, err
}

func getConnection(serverAddr string) (conn *grpc.ClientConn, err error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithBlock())
	fmt.Printf("dialing server %s\n", serverAddr)
	conn, err = grpc.Dial(serverAddr, opts...)
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
	}
	return conn, err
}

func browseFeed(conn *grpc.ClientConn) (siteName string, err error) {
	clientFeed := pb.NewFeedClient(conn)
	loggo.Info("New feed client created, requesting feed from server")
	fr := pb.FeedRequest{
		SendData: true,
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	feed, err := clientFeed.GetFeed(ctx, &fr)
	if err != nil {
		loggo.Error("error getting feed from server", "error", err.Error())
	}
	siteName, err = promptSites(feed)
	if err != nil {
		loggo.Error("error prompting for site name", "error", err.Error())
	}
	return siteName, err
}

func getSite(conn *grpc.ClientConn, siteName string) (site *pb.SiteResponse , err error) {
	clientSite := pb.NewSiteClient(conn)
	loggo.Info("New site client created")
	sr := pb.SiteRequest{
		Name: siteName,
	}
	// get site from server
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	site, err = clientSite.GetSite(ctx, &sr)
	if err != nil {
		loggo.Error("error getting site from server", "error", err.Error())
	}
	return site, err
}

func compileBoxes(site *pb.SiteResponse) ([]*boxes.DivBox, error) {
	var myBoxes []*boxes.DivBox
	var err error
	for _, div := range site.DivBoxes.Boxes {
		// convert divboxes to local format
		b, err := ugcon.ConvertDivBoxLocalBoxes(div)
		if err != nil {
			return myBoxes, err
		}
		myBoxes = append(myBoxes, b)
	}
	// collect elements from site
	for _, ele := range site.Elements.TextBlobs {
		// convert and mate textBlobs to boxes
		tb, err := ugcon.ConvertTextBlobLocalBoxes(ele)
		if err != nil {
			return myBoxes, err
		}
		fgcolor, _, _ := tb.Style.Decompose()
		tcolor := fgcolor.TrueColor()
		loggo.Debug("style after converstion",
			"fgcolor", tcolor, "site-name", site.Name,
		)
		if site.Name == "uggcli-menu" {
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
	siteName string
	server string
	port int
	connString string
}

func parseLinks(site *pb.SiteResponse) (links []*link, err error) {
	for _, l := range(site.Links) {
		var tempLink link
		tempLink.siteName = l.SiteName
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
	// set up rpc client
	serverAddr := fmt.Sprintf("%s:%s", *host, *port)
	conn, err := getConnection(serverAddr)
	defer conn.Close()
	if err != nil {
		loggo.Error("dialing server failed", "server", serverAddr, "err", err.Error())
		os.Exit(1)
	}
	siteName, err := browseFeed(conn)
	if err != nil {
		loggo.Error("selecting feed failed", "err", err.Error())
		os.Exit(1)
	}
	site, err := getSite(conn, siteName)
	if err != nil {
		loggo.Error("getting site failed", "err", err.Error())
		os.Exit(1)
	}
	// now convert server divs to boxes
	myBoxes, err := compileBoxes(site)
	if err != nil {
		loggo.Error("error compiling boxes", "err", err.Error())
		os.Exit(1)
	}
	links, err := parseLinks(site)
	if err != nil {
		loggo.Error("error parsing links", "err", err.Error())
		os.Exit(1)
	}
	screen, err := initScreen()
	if err != nil {
		loggo.Error("error intitializing screen", "err", err.Error())
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
	quit := make(chan struct{})
	go pollEvents(screen, links, quit)

drawloop:
	for {
		select {
		case <-quit:
			break drawloop
		case <-time.After(time.Millisecond * 200):
		}
		makeboxes(screen, myBoxes, quit)
	}
	//makeboxes(screen, myBoxes, quit)
	screen.Fini()
}
