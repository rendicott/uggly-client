package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	pb "github.com/rendicott/uggly"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"strings"
	"time"
	//"crypto/x509"
)

type session struct {
	conn            *grpc.ClientConn
	server          string
	port            string
	secure, secured bool
	currPage        string
	clientWidth     int32
	clientHeight    int32
}

func (s *session) getConnection() (err error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithBlock())
	tempConnString := fmt.Sprintf("%s:%s", s.server, s.port)
	loggo.Info("dialing server", "connString", tempConnString)
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if s.secure {
		//certs, err := x509.SystemCertPool()
		//if err != nil {
		//	loggo.Error("error loading system cert pool")
		//	return err
		//}
		config := &tls.Config{
			//RootCAs: certs,
		}
		loggo.Info("attempting secure connection", "host", tempConnString)
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(config)))
		s.conn, err = grpc.DialContext(ctx, tempConnString, opts...)
		s.secured = true
	} else {
		loggo.Info("attempting insecure connection")
		opts = append(opts, grpc.WithInsecure())
		s.conn, err = grpc.DialContext(ctx, tempConnString, opts...)
		s.secured = false
	}
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
		s.secure = false
		return err
	}
	loggo.Info("connection successful", "connString", tempConnString)
	return err
}

func (s *session) get2(ctx context.Context, pq *pb.PageRequest) (pr *pb.PageResponse, err error) {
	loggo.Info("current and desired connection info",
		"rserver", pq.Server, "rport", pq.Port,
		"cserver", s.server, "cport", s.port,
	)
	if pq.Server == s.server && pq.Port == s.port && s.conn != nil {
		// no need for new connection so just log
		loggo.Info("request for same server:port, reusing same connection")
	} else {
		loggo.Info("request for new server:port, establishing new connection")
		s.setServer(pq.Server, pq.Port, pq.Secure)
		err = s.getConnection()
		if err != nil {
			return pr, err
		}
	}
	clientPage := pb.NewPageClient(s.conn)
	pr, err = clientPage.GetPage(ctx, pq)
	if err != nil {
		loggo.Error("error getting page from server", "error", err.Error())
		// reset err text so we can catch it
		err = errors.New("error getting page from server")
	}
	s.currPage = pq.Name
	return pr, err
}

func newSession() *session {
	var s session
	return &s
}

// setServer just sets things up for dialing the gRPC connection
// and some place to store our current connection so we can prevent
// having to redial.
func (s *session) setServer(server, port string, secure bool) {
	// borrow link methods to prevent repetition of construct logic
	s.server = server
	s.port = port
	s.secure = secure
}

func (s *session) feedKeyStrokes() (keyStrokes []*pb.KeyStroke, err error) {
	feedErrMsg := "no server connection"
	feedErrMsgNoFeed := "server provides no feed"
	if s.conn == nil {
		err = errors.New(feedErrMsg)
		loggo.Error(feedErrMsg)
		return keyStrokes, err
	}
	clientFeed := pb.NewFeedClient(s.conn)
	loggo.Info("New feed client created, requesting feed from server")
	fr := pb.FeedRequest{
		SendData: true,
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	feed, err := clientFeed.GetFeed(ctx, &fr)
	if err != nil {
		loggo.Error("error getting feed from server", "error", err.Error())
		if strings.Contains(err.Error(), "connection refused") {
			// reset err text so we can catch it
			err = errors.New(feedErrMsg)
		}
		if strings.Contains(err.Error(), "unknown service") {
			// reset err text so we can catch it
			err = errors.New(feedErrMsgNoFeed)
		}
		return keyStrokes, err
	}
	strokeMap := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9",
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
		"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"}
	for i, page := range feed.Pages {
		keyStrokes = append(keyStrokes, &pb.KeyStroke{
			KeyStroke: strokeMap[i],
			Action: &pb.KeyStroke_Link{
				Link: &pb.Link{
					PageName:  page.Name,
					Server:    s.server,
					Port:      s.port,
				},
			},
		})
	}
	loggo.Debug("feedKeyStrokes returning keyStrokes", "len(keyStrokes)", len(keyStrokes))
	return keyStrokes, err
}
