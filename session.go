package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	pb "github.com/rendicott/uggly"
	"github.com/rendicott/uggo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"strings"
	"time"
	//"crypto/x509"
)

type session struct {
	conn            *grpc.ClientConn
	server          string
	port            string
	stream          bool
	secure, secured bool
	currPage        string
	clientWidth     int32
	clientHeight    int32
}

func (s *session) genUgri() *string {
	proto := "ugtp://"
	if s.secure {
		proto = "ugtps://"
	}
	ugri := fmt.Sprintf("%s%s:%s/%s",
		proto, s.server, s.port, s.currPage)
	return &ugri
}

// dialTimeout caps how long we wait for TCP/TLS handshake separately from page RPC.
const dialTimeout = 10 * time.Second

func (s *session) getConnection(ctx context.Context) (err error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithBlock())
	tempConnString := fmt.Sprintf("%s:%s", s.server, s.port)
	loggo.Info("dialing server", "connString", tempConnString)

	// Prefer a short dial budget; if parent already has a tighter deadline, honor it.
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	if s.secure {
		config := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		loggo.Info("attempting secure connection", "host", tempConnString)
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(config)))
		s.conn, err = grpc.DialContext(dialCtx, tempConnString, opts...)
		s.secured = true
	} else {
		loggo.Info("attempting insecure connection")
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		s.conn, err = grpc.DialContext(dialCtx, tempConnString, opts...)
		s.secured = false
	}
	if err != nil {
		loggo.Error("fail to dial", "error", err.Error())
		s.secure = false
		return fmt.Errorf("dial %s: %w", tempConnString, err)
	}
	loggo.Info("connection successful", "connString", tempConnString)
	// Best-effort capability handshake (Meta service is optional)
	s.tryMetaHello(dialCtx)
	return err
}

// tryMetaHello calls Meta.Hello when the server implements it. Failure is non-fatal.
func (s *session) tryMetaHello(ctx context.Context) {
	if s.conn == nil {
		return
	}
	client := pb.NewMetaClient(s.conn)
	helloCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := client.Hello(helloCtx, &pb.ClientHello{
		ClientVersion: version,
		Features: []string{
			"stream", "stream_input", "page_timeout", "divscroll",
			"screenshot", "stream_pause", "query", "event",
		},
		Width:  s.clientWidth,
		Height: s.clientHeight,
		ReservedKeys: map[string]string{
			"F1": "address-bar", "F2": "color-demo", "F3": "settings",
			"F4": "feed", "F5": "refresh", "F6": "bookmarks", "F7": "add-bookmark",
			"F8": "screenshot", "F9": "stream-pause", "F10": "exit",
		},
	})
	if err != nil {
		loggo.Debug("Meta.Hello unavailable", "error", err.Error())
		return
	}
	loggo.Info("Meta.Hello ok",
		"app", resp.GetAppName(),
		"serverVersion", resp.GetServerVersion(),
		"features", resp.GetFeatures(),
		"defaultPage", resp.GetDefaultPage(),
	)
	if resp.GetDefaultPage() != "" && s.currPage == "" {
		s.currPage = resp.GetDefaultPage()
	}
}

func (s *session) prepGet(ctx context.Context, pq *pb.PageRequest) (err error) {
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
		err = s.getConnection(ctx)
	}
	return err
}

func (s *session) getStream(ctx context.Context, pq *pb.PageRequest, r chan *pb.PageResponse) (err error) {
	err = s.prepGet(ctx, pq)
	if err != nil {
		close(r)
		return err
	}
	clientPage := pb.NewPageClient(s.conn)
	stream, err := clientPage.GetPageStream(ctx, pq)
	if err != nil {
		loggo.Error("GetPageStream error", "error", err.Error())
		return err
	}
	s.stream = true
	for {
		if ctx.Err() != nil {
			loggo.Info("ctx", "status", ctx.Err().Error())
		}
		select {
		case <-ctx.Done():
			loggo.Info("caught ctx close")
			close(r)
			return err
		default:
			page, err := stream.Recv()
			if err == io.EOF {
				loggo.Info("GetPageStream EOF")
				close(r)
				err = nil
				return err
			}
			if err != nil {
				loggo.Error("GetPageStream error", "error", err.Error())
				close(r)
				return err
			}
			if !strings.Contains(pq.Name, "->") && pq.Stream {
				pq.Name += "->"
			}
			s.currPage = pq.Name
			r <- page
		}
	}
	close(r)
	return err
}
func (s *session) get2(ctx context.Context, pq *pb.PageRequest) (pr *pb.PageResponse, err error) {
	err = s.prepGet(ctx, pq)
	if err != nil {
		return pr, err
	}
	clientPage := pb.NewPageClient(s.conn)
	pr, err = clientPage.GetPage(ctx, pq)
	if err != nil {
		loggo.Error("error getting page from server", "error", err.Error())
		// Wrap so callers can still errors.Is / gRPC status.FromError the cause,
		// while keeping a stable outer message for legacy string matches.
		err = fmt.Errorf("error getting page from server: %w", err)
	} else {
		s.currPage = pq.Name
	}
	return pr, err
}

func newSession() *session {
	var s session
	return &s
}

// setServer just sets things up for dialing the gRPC connection
// and some place to store our current connection so we can prevent
// having to redial. If host/port/secure change, drop the old conn so
// prepGet cannot reuse a stale dial (e.g. address-bar jump 5566→50051).
func (s *session) setServer(server, port string, secure bool) {
	if s.server != server || s.port != port || s.secure != secure {
		if s.conn != nil {
			_ = s.conn.Close()
			s.conn = nil
		}
		s.stream = false
		s.secured = false
	}
	s.server = server
	s.port = port
	s.secure = secure
}

func (s *session) getFeed() (feed *pb.FeedResponse, err error) {
	feedErrMsg := "no server connection"
	feedErrMsgNoFeed := "server provides no feed"
	if s.conn == nil {
		return nil, errors.New(feedErrMsg)
	}
	clientFeed := pb.NewFeedClient(s.conn)
	loggo.Info("New feed client created, requesting feed from server")
	fr := pb.FeedRequest{SendData: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	feed, err = clientFeed.GetFeed(ctx, &fr)
	if err != nil {
		loggo.Error("error getting feed from server", "error", err.Error())
		if strings.Contains(err.Error(), "connection refused") {
			err = errors.New(feedErrMsg)
		} else if strings.Contains(err.Error(), "unknown service") {
			err = errors.New(feedErrMsgNoFeed)
		}
		return nil, err
	}
	return feed, nil
}

// feedKeyStrokes builds keystrokes for one page of feed listings (legacy helper).
func (s *session) feedKeyStrokes() (keyStrokes []*pb.KeyStroke, err error) {
	feed, err := s.getFeed()
	if err != nil {
		return keyStrokes, err
	}
	for i, page := range feed.Pages {
		if i >= len(uggo.StrokeMap) {
			break
		}
		keyStrokes = append(keyStrokes, &pb.KeyStroke{
			KeyStroke: uggo.StrokeMap[i],
			Action: &pb.KeyStroke_Link{
				Link: &pb.Link{
					PageName: page.Name,
					Server:   s.server,
					Port:     s.port,
				},
			},
		})
	}
	return keyStrokes, err
}
