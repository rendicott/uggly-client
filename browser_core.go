package main

import (
	"context"
	"sync/atomic"
	"time"

	pb "github.com/rendicott/uggly"
	"google.golang.org/protobuf/proto"
)

// fetchDoneEvent is posted to the tcell event queue when an async GetPage finishes.
// Only the pollEvents loop applies results, keeping paint single-threaded.
type fetchDoneEvent struct {
	gen  uint64
	pq   *pb.PageRequest
	page *pb.PageResponse
	err  error
	dest string
}

func (e *fetchDoneEvent) When() time.Time { return time.Now() }

// statusOnlyEvent requests a status-bar update on the UI thread.
type statusOnlyEvent struct {
	msg string
}

func (e *statusOnlyEvent) When() time.Time { return time.Now() }

// clonePage returns a deep copy so expand/draw cannot mutate the stored wire page.
func clonePage(page *pb.PageResponse) *pb.PageResponse {
	if page == nil {
		return nil
	}
	c, ok := proto.Clone(page).(*pb.PageResponse)
	if !ok || c == nil {
		return page
	}
	return c
}

func (b *ugglyBrowser) exiting() bool {
	return atomic.LoadInt32(&b.exitAtomic) != 0
}

func (b *ugglyBrowser) setExiting() {
	atomic.StoreInt32(&b.exitAtomic, 1)
}

func (b *ugglyBrowser) setPageReady(v bool) {
	if v {
		atomic.StoreInt32(&b.pageReadyAtomic, 1)
	} else {
		atomic.StoreInt32(&b.pageReadyAtomic, 0)
	}
	b.pageReady = v
}

func (b *ugglyBrowser) isPageReady() bool {
	return atomic.LoadInt32(&b.pageReadyAtomic) != 0
}

// beginFetch cancels any in-flight fetch and returns a generation + context.
// stream=true uses a cancel-only context (no page-timeout deadline) so live
// surfaces can run indefinitely until the user navigates away or presses Esc.
// Unary page fetches keep -page-timeout (default 180s) for slow LLM servers.
func (b *ugglyBrowser) beginFetch(stream bool) (gen uint64, ctx context.Context) {
	b.fetchMu.Lock()
	defer b.fetchMu.Unlock()
	if b.fetchCancel != nil {
		b.fetchCancel()
	}
	gen = atomic.AddUint64(&b.fetchGen, 1)
	if stream {
		ctx, b.fetchCancel = context.WithCancel(context.Background())
	} else {
		to := time.Duration(*pageTimeout) * time.Second
		if to <= 0 {
			to = 180 * time.Second
		}
		ctx, b.fetchCancel = context.WithTimeout(context.Background(), to)
	}
	return gen, ctx
}

func (b *ugglyBrowser) isFetchCurrent(gen uint64) bool {
	return atomic.LoadUint64(&b.fetchGen) == gen
}

// streamApplyEvent wakes the UI thread to paint the latest coalesced stream frame.
// Multiple stream frames collapse into one pending pointer so the UI never
// falls behind painting every intermediate frame (critical for game feel).
type streamApplyEvent struct{}

func (e *streamApplyEvent) When() time.Time { return time.Now() }

// postFetchDone delivers a fetch result to the UI event loop.
// Stream frames are coalesced: only the latest is kept; a single streamApplyEvent
// is posted if one is not already pending.
func (b *ugglyBrowser) postFetchDone(ev *fetchDoneEvent) {
	if b.view == nil || b.exiting() || ev == nil {
		return
	}
	isStream := ev.dest == "stream" || (ev.pq != nil && ev.pq.Stream)
	if isStream {
		b.mu.Lock()
		b.pendingStream = ev
		needPost := !b.streamPostPending
		if needPost {
			b.streamPostPending = true
		}
		b.mu.Unlock()
		if !needPost {
			return
		}
		// Wake UI once; applyStreamPending drains the latest pointer.
		for i := 0; i < 80; i++ {
			if err := b.view.PostEvent(&streamApplyEvent{}); err == nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		b.mu.Lock()
		b.streamPostPending = false
		b.mu.Unlock()
		loggo.Warn("failed to post streamApplyEvent")
		return
	}
	for i := 0; i < 80; i++ {
		if err := b.view.PostEvent(ev); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	loggo.Warn("failed to post fetchDoneEvent")
}

// applyStreamPending paints the newest stream frame and re-arms if newer arrived.
func (b *ugglyBrowser) applyStreamPending() {
	for {
		b.mu.Lock()
		ev := b.pendingStream
		b.pendingStream = nil
		b.streamPostPending = false
		b.mu.Unlock()
		if ev == nil {
			return
		}
		b.applyFetchResult(ev)
		// If a newer frame landed during apply, loop without re-posting.
		b.mu.Lock()
		more := b.pendingStream != nil
		b.mu.Unlock()
		if !more {
			return
		}
	}
}

// safeSendMessage queues a status update without blocking and without panicking
// after shutdown. Also posts a statusOnlyEvent so the UI thread can redraw.
func (b *ugglyBrowser) safeSendMessage(msg, label string) {
	if b.exiting() {
		return
	}
	loggo.Debug("safeSendMessage", "msg", msg, "label", label)
	if b.view != nil {
		_ = b.view.PostEvent(&statusOnlyEvent{msg: msg})
	}
	select {
	case b.messageBuffer <- msg:
	default:
		// Drop one old message, then try again with latest
		select {
		case <-b.messageBuffer:
		default:
		}
		select {
		case b.messageBuffer <- msg:
		default:
		}
	}
}
