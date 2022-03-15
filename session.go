package main

import (
	"fmt"
	"context"
	"errors"
	"time"
	"google.golang.org/grpc"
	pb "github.com/rendicott/uggly"
)

type session struct {
	conn *grpc.ClientConn
	server  string
	port    string
	connString string
	currPage string
}


func (s *session) getConnection() (err error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithBlock())
	loggo.Info("dialing server", "connString", s.connString)
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s.conn, err = grpc.DialContext(ctx, s.connString, opts...)
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
		return err
	}
	loggo.Info("connection successful", "connString", s.connString)
	return err
}

func (s *session) directDial(host, port, page string) (sr *pb.PageResponse, err error) {
	s.setServer(host, port)
	err = s.getConnection()
	if err != nil {
		return sr, err
	}
	s.currPage = page
	return s.getPage()
}

func (s *session) getPage() (page *pb.PageResponse , err error) {
	clientPage := pb.NewPageClient(s.conn)
	loggo.Info("New page client created")
	pr := pb.PageRequest{
		Name: s.currPage,
	}
	// get page from server
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	page, err = clientPage.GetPage(ctx, &pr)
	if err != nil {
		loggo.Error("error getting page from server", "error", err.Error())
	}
	return page, err
}

func newSession() (*session) {
	var s session
	return &s
}

func (s *session) setServer(host string, port string) {
	s.server = host
	s.port = port
	s.connString = fmt.Sprintf("%s:%s", host, port)
}

func (s *session) feedLinks() (links []*pb.Link, err error) {
	if s.conn == nil {
		msg := "no server connection"
		err = errors.New(msg)
		loggo.Error(msg)
		return links, err
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
		return links, err
	}
	strokeMap := []string{"1","2","3","4","5","6","7","8","9",
		"a","b","c","d","e","f","g","h","i","j","k","l","m",
		"n","o","p","q","r","s","t","u","v","w","x","y","z"}
	for i, page := range(feed.Pages) {
		links = append(links, &pb.Link{
			KeyStroke: strokeMap[i],
			PageName: page.Name,
			Server: s.server,
			Port: s.port,
		})
	}
	return links, err
}


