package main

import (
	"context"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggly-client/boxes"
	"github.com/rendicott/uggly-client/ugcon"
	"google.golang.org/grpc"
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
	logFile    = "ttt.log.json"
	//logLevel   = "info"
	logLevel   = "debug"
	serverAddr = "localhost:10000"
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

func makeboxes(s tcell.Screen, bis []*boxes.DivBox, quit chan struct{}) {
	// debugSampleRate := 20 // to minimize debug message volume
	// pixelCount := 0
	for _, bi := range bis {
		for i := 0; i < bi.Width; i++ {
			for j := 0; j < bi.Height; j++ {
				x := bi.StartX + i
				y := bi.StartY + j
				// if pixelCount % debugSampleRate == 0 {
				//     fgcolor,_,_ := bi.RawContents[i][j].St.Decompose()
				//     tcolor := fgcolor.TrueColor()
				//     loggo.Debug("makeboxes setting pixel",
				//         "content",string(bi.RawContents[i][j].C),
				//         "stylefgcolor", tcolor,
				//     )
				// }
				// pixelCount++
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

func main() {
	setLogger(daemonFlag, logFile, logLevel)
	boxes.Loggo = loggo
	ugcon.Loggo = loggo
	// set up rpc client
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithBlock())
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
		os.Exit(1)
	}
	defer conn.Close()
	client := pb.NewFeedClient(conn)
	loggo.Info("New feed client created")
	fr := pb.FeedRequest{
		SendData: true,
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	// get feed from server
	feed, err := client.GetFeed(ctx, &fr)
	if err != nil {
		loggo.Error("error getting divs from server", "error", err.Error())
		os.Exit(1)
	}
	// now convert server divs to boxes
	var myBoxes []*boxes.DivBox
	for _, div := range feed.DivBoxes.Boxes {
		// convert divboxes to local format
		b, err := ugcon.ConvertDivBoxUgglyBoxes(div)
		handle(err)
		myBoxes = append(myBoxes, b)
	}
	// collect elements from feed
	for _, ele := range feed.Elements.TextBlobs {
		// convert and mate textBlobs to boxes
		tb, err := ugcon.ConvertTextBlobUgglyBoxes(ele)
		handle(err)
		fgcolor, _, _ := tb.Style.Decompose()
		tcolor := fgcolor.TrueColor()
		loggo.Debug("style after converstion", "function", "main",
			"fgcolor", tcolor,
		)
		tb.MateBoxes(myBoxes)
	}
	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)
	s, err := tcell.NewScreen()
	handle(err)
	err = s.Init()
	handle(err)
	s.SetStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).
		Background(tcell.Color220))
	s.Clear()
	quit := make(chan struct{})
	go func() {
		for {
			ev := s.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyEscape, tcell.KeyEnter:
					close(quit)
					return
				case tcell.KeyCtrlL:
					s.Sync()
				}
			case *tcell.EventResize:
				s.Sync()
			}
		}
	}()

	cnt := 0
	dur := time.Duration(0)
	for _, bi := range myBoxes {
		bi.Init()
	}
loop:
	for {
		select {
		case <-quit:
			break loop
		case <-time.After(time.Millisecond * 1000):
		}
		start := time.Now()
		makeboxes(s, myBoxes, quit)
		cnt++
		dur += time.Now().Sub(start)
	}
	s.Fini()
}
