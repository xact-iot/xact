package tree

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultResolution = time.Second
	DefaultWheelSize  = 3600 // 1 hour wheel
)

type Timer struct {
	mu       sync.Mutex // guards bucket and elem; always acquired after b.mu
	rounds   int
	callback func()
	bucket   *bucket
	elem     *list.Element
	stopped  atomic.Bool
}

type bucket struct {
	mu     sync.Mutex
	timers *list.List
}

type TimingWheel struct {
	resolution time.Duration
	wheelSize  int

	buckets []bucket
	current int64

	ticker *time.Ticker
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewTimingWheel(resolution time.Duration, wheelSize int) *TimingWheel {
	if resolution <= 0 {
		resolution = DefaultResolution
	}
	if wheelSize <= 0 {
		wheelSize = DefaultWheelSize
	}

	tw := &TimingWheel{
		resolution: resolution,
		wheelSize:  wheelSize,
		buckets:    make([]bucket, wheelSize),
		stopCh:     make(chan struct{}),
	}

	for i := range tw.buckets {
		tw.buckets[i].timers = list.New()
	}

	return tw
}

func (tw *TimingWheel) Start() {
	tw.ticker = time.NewTicker(tw.resolution)
	tw.wg.Add(1)
	go tw.run()
}

func (tw *TimingWheel) Stop() {
	close(tw.stopCh)
	tw.ticker.Stop()
	tw.wg.Wait()
}

func (tw *TimingWheel) run() {
	defer tw.wg.Done()

	for {
		select {
		case <-tw.ticker.C:
			tw.tick()
		case <-tw.stopCh:
			return
		}
	}
}

func (tw *TimingWheel) tick() {
	current := atomic.AddInt64(&tw.current, 1)
	slot := int(current % int64(tw.wheelSize))

	b := &tw.buckets[slot]

	b.mu.Lock()
	for e := b.timers.Front(); e != nil; {
		next := e.Next()
		t := e.Value.(*Timer)

		if t.rounds > 0 {
			t.rounds--
			e = next
			continue
		}

		b.timers.Remove(e)
		t.mu.Lock()
		t.elem = nil
		t.bucket = nil
		t.mu.Unlock()

		if !t.stopped.Load() {
			go t.callback()
		}

		e = next
	}
	b.mu.Unlock()
}

func (tw *TimingWheel) AfterFunc(d time.Duration, cb func()) *Timer {
	if d <= 0 {
		go cb()
		return nil
	}

	ticks := int(d / tw.resolution)
	if ticks == 0 {
		ticks = 1
	}

	current := atomic.LoadInt64(&tw.current)
	slot := int((current + int64(ticks)) % int64(tw.wheelSize))
	rounds := ticks / tw.wheelSize

	t := &Timer{
		rounds:   rounds,
		callback: cb,
	}

	b := &tw.buckets[slot]

	b.mu.Lock()
	t.mu.Lock()
	t.elem = b.timers.PushBack(t)
	t.bucket = b
	t.mu.Unlock()
	b.mu.Unlock()

	return t
}

func (t *Timer) Stop() {
	if t == nil {
		return
	}
	if t.stopped.Swap(true) {
		return
	}

	// Read t.bucket under t.mu to avoid racing with tick()'s write.
	t.mu.Lock()
	b := t.bucket
	t.mu.Unlock()

	if b == nil {
		return
	}

	// Lock order: b.mu then t.mu - same order used in tick().
	b.mu.Lock()
	t.mu.Lock()
	if t.elem != nil {
		b.timers.Remove(t.elem)
		t.elem = nil
		t.bucket = nil
	}
	t.mu.Unlock()
	b.mu.Unlock()
}
