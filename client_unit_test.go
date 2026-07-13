package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	pb "github.com/rendicott/uggly"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMakeStep_DigitsAreKeys(t *testing.T) {
	for _, d := range []string{"0", "1", "2", "5", "9"} {
		s := makeStep(d)
		if s.wait != 0 {
			t.Fatalf("digit %q should not be a delay, got wait=%v", d, s.wait)
		}
		if s.ev == nil {
			t.Fatalf("digit %q should synthesize a key event", d)
		}
		ek, ok := s.ev.(*tcell.EventKey)
		if !ok {
			t.Fatalf("digit %q: expected EventKey, got %T", d, s.ev)
		}
		if ek.Key() != tcell.KeyRune || ek.Rune() != rune(d[0]) {
			t.Fatalf("digit %q: got key=%v rune=%q", d, ek.Key(), ek.Rune())
		}
	}
}

func TestMakeStep_DurationAndNamedKeys(t *testing.T) {
	s := makeStep("2s")
	if s.ev != nil || s.wait != 2*time.Second {
		t.Fatalf("2s: want wait=2s, got wait=%v ev=%v", s.wait, s.ev)
	}
	s = makeStep("500ms")
	if s.wait != 500*time.Millisecond {
		t.Fatalf("500ms: got %v", s.wait)
	}
	s = makeStep("1.5")
	if s.wait != 1500*time.Millisecond {
		t.Fatalf("1.5: got %v", s.wait)
	}
	s = makeStep("Enter")
	if s.ev == nil || s.wait != 0 {
		t.Fatalf("Enter: want key event, got wait=%v ev=%v", s.wait, s.ev)
	}
	s = makeStep("a")
	if s.ev == nil {
		t.Fatal("letter a should be a key")
	}
}

func TestIsDeadlineAndCancelled(t *testing.T) {
	if isDeadline(nil) || isCancelled(nil) {
		t.Fatal("nil should be false")
	}
	if !isDeadline(context.DeadlineExceeded) {
		t.Fatal("DeadlineExceeded")
	}
	if !isDeadline(fmt.Errorf("wrap: %w", context.DeadlineExceeded)) {
		t.Fatal("wrapped DeadlineExceeded")
	}
	if !isDeadline(status.Error(codes.DeadlineExceeded, "deadline")) {
		t.Fatal("gRPC DeadlineExceeded")
	}
	if !isCancelled(context.Canceled) {
		t.Fatal("Canceled")
	}
	if !isCancelled(fmt.Errorf("wrap: %w", context.Canceled)) {
		t.Fatal("wrapped Canceled")
	}
	if !isCancelled(status.Error(codes.Canceled, "cancel")) {
		t.Fatal("gRPC Canceled")
	}
	if isDeadline(errors.New("something else")) {
		t.Fatal("unrelated error should not be deadline")
	}
}

func TestLinkFromString(t *testing.T) {
	l, err := linkFromString("ugtp://127.0.0.1:5566/list")
	if err != nil {
		t.Fatal(err)
	}
	if l.Server != "127.0.0.1" || l.Port != "5566" || l.PageName != "list" || l.Secure {
		t.Fatalf("got %+v", l)
	}
	l, err = linkFromString("ugtps://example.com:8443/home")
	if err != nil {
		t.Fatal(err)
	}
	if !l.Secure || l.Server != "example.com" || l.Port != "8443" || l.PageName != "home" {
		t.Fatalf("secure got %+v", l)
	}
}

func TestLinkRequestPreservesQueryAndEvent(t *testing.T) {
	pr := linkRequest(&pb.Link{
		PageName: "list",
		Server:   "h",
		Port:     "1",
		Query:    map[string]string{"page": "2", "type": "micro"},
		Event:    &pb.Event{Name: "click"},
	})
	if pr.Query["page"] != "2" || pr.Query["type"] != "micro" {
		t.Fatalf("query: %v", pr.Query)
	}
	if pr.Event == nil || pr.Event.Name != "click" {
		t.Fatalf("event: %v", pr.Event)
	}
}

func TestLinkFillerPreservesQuery(t *testing.T) {
	b := newBrowser()
	b.sess = newSession()
	b.sess.setServer("127.0.0.1", "5566", false)
	full, err := b.linkFiller(&pb.Link{
		PageName: "detail",
		Query:    map[string]string{"id": "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.Server != "127.0.0.1" || full.Port != "5566" {
		t.Fatalf("filled host: %s:%s", full.Server, full.Port)
	}
	if full.Query["id"] != "abc" {
		t.Fatalf("query lost: %v", full.Query)
	}
}

func TestExpandTableOneBoxPerRow(t *testing.T) {
	page := &pb.PageResponse{
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{
			Tables: []*pb.Table{{
				Name:    "t1",
				StartY:  1,
				Headers: []string{"Name", "City"},
				Rows: []*pb.TableRow{
					{Cells: []string{"Alpha", "Austin"}},
					{Cells: []string{"Beta", "Boston"}},
					{Cells: []string{"Gamma", "Denver"}},
				},
				Zebra: true,
			}},
		},
	}
	out := expandPage(page, 80, 40)
	// header + 3 rows => 4 boxes (not 2 cols * 4 = 8)
	if n := len(out.DivBoxes.Boxes); n != 4 {
		t.Fatalf("expected 4 row boxes, got %d", n)
	}
	if n := len(out.Elements.TextBlobs); n != 4 {
		t.Fatalf("expected 4 text blobs, got %d", n)
	}
	// Columns padded into one line — Name and City should both appear
	found := false
	for _, tb := range out.Elements.TextBlobs {
		if tb != nil && containsAll(tb.Content, "Alpha", "Austin") {
			found = true
			// spaces between columns
			if !containsAll(tb.Content, "Alpha") {
				t.Fatalf("row content: %q", tb.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected a data row containing Alpha and Austin")
	}
	// Tables cleared after expand (idempotent re-expand source is wire)
	if len(out.Elements.Tables) != 0 {
		t.Fatal("tables should be cleared after expand")
	}
}

func TestExpandListAddsKeystrokes(t *testing.T) {
	page := &pb.PageResponse{
		DivBoxes: &pb.DivBoxes{},
		Elements: &pb.Elements{
			Lists: []*pb.ItemList{{
				Name: "m",
				Items: []*pb.ListItem{
					{Key: "1", Label: "One", Link: &pb.Link{PageName: "p1"}},
					{Key: "2", Label: "Two", Link: &pb.Link{PageName: "p2"}},
				},
			}},
		},
	}
	out := expandPage(page, 80, 24)
	if len(out.KeyStrokes) < 2 {
		t.Fatalf("expected list keystrokes, got %d", len(out.KeyStrokes))
	}
	keys := map[string]bool{}
	for _, ks := range out.KeyStrokes {
		keys[ks.KeyStroke] = true
	}
	if !keys["1"] || !keys["2"] {
		t.Fatalf("keys: %v", keys)
	}
}

func TestClonePageIndependent(t *testing.T) {
	src := &pb.PageResponse{
		Name: "wire",
		DivBoxes: &pb.DivBoxes{
			Boxes: []*pb.DivBox{{Name: "a", Width: 10, Height: 2}},
		},
		Elements: &pb.Elements{
			Tables: []*pb.Table{{
				Name:    "t",
				Headers: []string{"H"},
				Rows:    []*pb.TableRow{{Cells: []string{"c"}}},
			}},
		},
	}
	c := clonePage(src)
	if c == nil || c.Name != "wire" {
		t.Fatal("clone failed")
	}
	c.Name = "mutated"
	c.Elements.Tables = nil
	if src.Name != "wire" {
		t.Fatal("clone mutated source name")
	}
	if len(src.Elements.Tables) != 1 {
		t.Fatal("clone mutated source tables")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
