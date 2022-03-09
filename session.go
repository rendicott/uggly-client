package main

import (
	"fmt"
	"context"
	"time"
	"errors"
	"google.golang.org/grpc"
	"github.com/AlecAivazis/survey/v2"
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
	s.conn, err = grpc.Dial(s.connString, opts...)
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
	}
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

func (s *session) browseFeed() (err error) {
	clientFeed := pb.NewFeedClient(s.conn)
	loggo.Info("New feed client created, requesting feed from server")
	fr := pb.FeedRequest{
		SendData: true,
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	feed, err := clientFeed.GetFeed(ctx, &fr)
	if err != nil {
		loggo.Error("error getting feed from server", "error", err.Error())
		return err
	}
	s.currPage, err = promptPages(feed)
	if err != nil {
		loggo.Error("error prompting for page name", "error", err.Error())
	}
	return  err
}

func promptPages(feed *pb.FeedResponse) (pageName string, err error) {
	var pages []string
	for _, page := range(feed.Pages) {
		fmt.Println(page.Name)
		pages = append(pages, page.Name)
	}
	loggo.Info("got pages", "len", len(pages))
	if len(pages) < 1 {
		err = errors.New("no pages returned from server feed")
		return pageName, err
	}
	if len(pages) == 1 {
		pageName = pages[0]
		return pageName, err
	}
	prompt := &survey.Select{
		Message: "Select a page from the server: ",
		Options: pages,
	}
	err = survey.AskOne(prompt, &pageName)
	return pageName, err
}
