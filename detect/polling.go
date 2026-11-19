//go:build linux || (darwin && (!cgo || !cardbot_native))

package detect

import (
	"fmt"
	"sync"
	"time"
)

// Detector polls for observed mount transitions in the default release builds.
type Detector struct {
	life    sync.Mutex
	mu      sync.RWMutex // publishing a fresh stream on restart
	stream  *cardStream
	started bool
	wg      sync.WaitGroup
	scan    func() // Optional per-instance scan injection; set before Start.
}

func NewDetector() *Detector { return &Detector{stream: newCardStream()} }

func (d *Detector) Start() error {
	d.life.Lock()
	defer d.life.Unlock()
	if d.started {
		return fmt.Errorf("detector already started")
	}
	d.mu.Lock()
	select {
	case <-d.stream.stop:
		d.stream = newCardStream()
	default:
	}
	d.mu.Unlock()
	d.started = true
	d.wg.Add(1)
	go d.pollLoop()
	return nil
}

func (d *Detector) Stop() {
	d.life.Lock()
	defer d.life.Unlock()
	if !d.started {
		return
	}
	close(d.stream.stop)
	d.wg.Wait()
	d.started = false
}

// Events delivers arrivals and removals through one FIFO. Subscribe again
// after restarting the detector; buffered events belong to the previous run.
func (d *Detector) Events() <-chan Event {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stream.events
}

func (d *Detector) Remove(path string) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	d.stream.forget(path)
}

func (d *Detector) pollLoop() {
	defer d.wg.Done()
	scan := d.scan
	if scan == nil {
		scan = d.scanVolumes
	}
	scan()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.stream.stop:
			return
		case <-ticker.C:
			scan()
		}
	}
}

func (d *Detector) processCard(path, name string) {
	if !d.stream.tracked(path) {
		if card := buildCard(path, name); card != nil {
			d.stream.observe(Event{Card: card})
		}
	}
}
