package main

import (
	"context"
	"testing"

	pb "github.com/rendicott/uggly"
)

func init() {
	// Avoid nil logger panics in cookie helpers during unit tests
	setLogger(true, "uggcli-test.log.json", "error")
}

func TestCookieExists(t *testing.T) {
	b := newBrowser()
	b.cookies = map[string][]*pb.Cookie{
		"127.0.0.1": {
			{Key: "plat_sid", Value: "abc"},
			{Key: "other", Value: "x"},
		},
	}
	if !b.cookieExists("127.0.0.1", &pb.Cookie{Key: "plat_sid"}) {
		t.Fatal("expected plat_sid to exist")
	}
	if b.cookieExists("127.0.0.1", &pb.Cookie{Key: "missing"}) {
		t.Fatal("missing should not exist")
	}
	if b.cookieExists("other.host", &pb.Cookie{Key: "plat_sid"}) {
		t.Fatal("wrong host")
	}
}

func TestSetCookiesBlankServer(t *testing.T) {
	b := newBrowser()
	b.sess = newSession()
	b.sess.server = "127.0.0.1"
	b.setCookies(&pb.PageResponse{
		SetCookies: []*pb.Cookie{
			{Key: "plat_sid", Value: "xyz"},
		},
	})
	if !b.cookieExists("127.0.0.1", &pb.Cookie{Key: "plat_sid"}) {
		t.Fatal("cookie not stored under session host")
	}
	got := b.cookies["127.0.0.1"][0]
	if got.Server != "127.0.0.1" {
		t.Fatalf("server stamp: got %q", got.Server)
	}
	if got.Value != "xyz" {
		t.Fatalf("value: %q", got.Value)
	}
}

func TestAddCookiesSendsSessionCookie(t *testing.T) {
	b := newBrowser()
	b.sess = newSession()
	b.sess.server = "127.0.0.1"
	b.cookies = map[string][]*pb.Cookie{
		"127.0.0.1": {
			{Key: "plat_sid", Value: "sess1", Server: "127.0.0.1"},
		},
	}
	pq := &pb.PageRequest{Server: "127.0.0.1", Port: "5577", Name: "play"}
	_, out := b.addCookies(context.Background(), pq)
	if len(out.SendCookies) != 1 || out.SendCookies[0].Key != "plat_sid" {
		t.Fatalf("send cookies: %+v", out.SendCookies)
	}
}

func TestAddCookiesRejectsStrictCrossHost(t *testing.T) {
	b := newBrowser()
	b.cookies = map[string][]*pb.Cookie{
		"127.0.0.1": {
			{Key: "x", Value: "1", Server: "localhost", SameSite: pb.Cookie_STRICT},
		},
	}
	pq := &pb.PageRequest{Server: "127.0.0.1", Port: "1", Name: "p"}
	_, out := b.addCookies(context.Background(), pq)
	if len(out.SendCookies) != 0 {
		t.Fatalf("expected discard strict cross-host, got %+v", out.SendCookies)
	}
}
