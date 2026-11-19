//go:build linux || (darwin && (!cgo || !cardbot_native))

package detect

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDetector_DeliversSlowConsumerBurstInOrder(t *testing.T) {
	d := NewDetector()
	card := &Card{Path: "same-mount"}
	var want []Event
	for range cap(d.stream.events) + 3 {
		want = append(want, Event{Card: card}, Event{RemovedPath: card.Path})
	}
	full := make(chan struct{})
	var once sync.Once
	d.scan = func() {
		once.Do(func() {
			for i, e := range want {
				if i == cap(d.stream.events) {
					close(full)
				}
				if !d.stream.observe(e) {
					return
				}
			}
		})
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	select {
	case <-full:
	case <-time.After(2 * time.Second):
		t.Fatal("buffer did not fill")
	}
	for i, w := range want {
		select {
		case got := <-d.Events():
			if got != w {
				t.Fatalf("event %d = %+v, want %+v", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing event %d", i)
		}
	}
}

func TestDetector_StopInterruptsFullStreamAndRestartStartsFresh(t *testing.T) {
	d := NewDetector()
	full := make(chan struct{})
	d.scan = func() {
		for i := 0; i <= cap(d.stream.events); i++ {
			if i == cap(d.stream.events) {
				close(full)
			}
			d.stream.observe(Event{Card: &Card{Path: fmt.Sprint(i)}})
		}
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-full:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not fill buffer")
	}
	stopped := make(chan struct{})
	go func() { d.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked on consumer")
	}
	d.scan = func() { d.stream.observe(Event{Card: &Card{Path: "fresh"}}) }
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	select {
	case e := <-d.Events():
		if e.Card == nil || e.Card.Path != "fresh" {
			t.Fatalf("stale event: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no initial event on restart")
	}
}

func TestDetector_Restart(t *testing.T) {
	d := NewDetector()
	var scans atomic.Int32
	d.scan = func() { scans.Add(1) } // Never enumerate the host's mounts.
	t.Cleanup(d.Stop)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	d.Stop()
	if err := d.Start(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	d.Stop()
	if got := scans.Load(); got < 2 {
		t.Fatalf("scans = %d, want an initial scan on each start", got)
	}
}
