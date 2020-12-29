package main

import (
	"fmt"
	"os"
	"time"
    "context"
    "github.com/rendicott/uggly-client/boxes"
	"github.com/gdamore/tcell/v2"
	"github.com/inconshreveable/log15"
    pb "github.com/rendicott/uggly"
    "google.golang.org/grpc"
)

// loggo is the global logger
var loggo log15.Logger

// setLogger sets up logging globally for the packages involved
// in the gossamer runtime.
func setLogger(daemonFlag bool, logFileS, loglevel string) {
	loggo = log15.New()
	if daemonFlag {
		loggo.SetHandler(
			log15.LvlFilterHandler(
				log15.LvlInfo,
				log15.Must.FileHandler(logFileS, log15.JsonFormat())))
	} else if loglevel == "debug" {
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
	logLevel   = "info"
    serverAddr = "localhost:10000"
)

func handle(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func sleep() {
	time.Sleep(10 * time.Millisecond)
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

func main() {
	setLogger(daemonFlag, logFile, logLevel)
    // set up rpc client
    var opts []grpc.DialOption
    opts = append(opts, grpc.WithInsecure())
    opts = append(opts, grpc.WithBlock())
    conn, err := grpc.Dial(serverAddr, opts...)
        if err != nil {
            loggo.Error("fail to dial","error", err.Error())
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
    divs, err := client.GetDivs(ctx, &fr)
    if err != nil {
        loggo.Error("error getting divs from server", "error", err.Error())
        os.Exit(1)
    }
    // now convert server divs to boxes
	var myBoxes []*boxes.DivBox
    for _, div := range(divs.Boxes) {
        b := boxes.DivBox{
            Border: div.Border,
            BorderW: int(div.BorderW),
            BorderChar: rune(div.BorderChar),
            FillChar: rune(div.FillChar),
            StartX: int(div.StartX),
            StartY: int(div.StartY),
            Width: int(div.Width),
            Height: int(div.Height),
        }
        myBoxes = append(myBoxes, &b)
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
    boxes.Loggo = loggo
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
	// myBoxes := []*boxes.DivBox{
	// 	&boxes.DivBox{
	// 	    Border: true,
	// 	    BorderW: 1,
	// 	    BorderChar: '+',
	// 	    FillChar: ' ',
	// 	    StartX: 8,
	// 	    StartY: 8,
	// 	    Width: 40,
	// 	    Height: 8,
	// 	},
	// 	&boxes.DivBox{
	// 		Border:     true,
	// 		BorderW:    1,
	// 		FillChar:   '*',
	// 		BorderChar: '$',
	// 		StartX:     8,
	// 		StartY:     18,
	// 		Width:      8,
	// 		Height:     12,
	// 	},
	// 	&boxes.DivBox{
	// 		Border:     true,
	// 		BorderW:    1,
	// 		FillChar:   ' ',
	// 		BorderChar: '$',
	// 		StartX:     45,
	// 		StartY:     18,
	// 		Width:      12,
	// 		Height:     12,
	// 	},
	// }
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
