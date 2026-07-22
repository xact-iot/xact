package tree

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewTimingWheel(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*100, 100)
	if tw == nil {
		t.Fatal("NewTimingWheel returned nil")
	}
	if tw.resolution != time.Millisecond*100 {
		t.Errorf("Expected resolution %v, got %v", time.Millisecond*100, tw.resolution)
	}
	if tw.wheelSize != 100 {
		t.Errorf("Expected wheelSize 100, got %v", tw.wheelSize)
	}
	if len(tw.buckets) != 100 {
		t.Errorf("Expected 100 buckets, got %v", len(tw.buckets))
	}
}

func TestNewTimingWheelDefaultValues(t *testing.T) {
	tw := NewTimingWheel(0, 0)
	if tw.resolution != DefaultResolution {
		t.Errorf("Expected default resolution %v, got %v", DefaultResolution, tw.resolution)
	}
	if tw.wheelSize != DefaultWheelSize {
		t.Errorf("Expected default wheel size %v, got %v", DefaultWheelSize, tw.wheelSize)
	}
}

func TestTimingWheelStartStop(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	time.Sleep(time.Millisecond * 50)
	tw.Stop()
}

func TestTimingWheelAfterFuncZeroDuration(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	var executed atomic.Bool
	tw.AfterFunc(0, func() {
		executed.Store(true)
	})
	time.Sleep(time.Millisecond * 5)
	if !executed.Load() {
		t.Error("Zero duration callback should have executed")
	}
}

func TestTimingWheelAfterFunc(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	var executed atomic.Bool
	tw.AfterFunc(time.Millisecond*25, func() {
		executed.Store(true)
	})

	time.Sleep(time.Millisecond * 100)
	if !executed.Load() {
		t.Error("Timer callback should have executed")
	}
}

func TestTimingWheelAfterFuncMultipleTimers(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	var count atomic.Int32
	tw.AfterFunc(time.Millisecond*25, func() {
		count.Add(1)
	})
	tw.AfterFunc(time.Millisecond*25, func() {
		count.Add(1)
	})
	tw.AfterFunc(time.Millisecond*30, func() {
		count.Add(1)
	})

	time.Sleep(time.Millisecond * 100)
	if count.Load() != 3 {
		t.Errorf("Expected 3 executions, got %d", count.Load())
	}
}

func TestTimerStop(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	executed := make(chan struct{})
	tw.AfterFunc(time.Millisecond*20, func() {
		close(executed)
	})

	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("Timer should have executed")
	}

	var stoppedTimerExecuted atomic.Bool
	timer2 := tw.AfterFunc(time.Millisecond*50, func() {
		stoppedTimerExecuted.Store(true)
	})

	timer2.Stop()

	// Wait well beyond the timer's scheduled slot so a callback from a broken
	// Stop implementation has time to run, even on a loaded CI runner.
	time.Sleep(time.Millisecond * 250)
	if stoppedTimerExecuted.Load() {
		t.Error("Stopped timer should not execute")
	}
}

func TestTimerStopNil(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	tw.AfterFunc(0, func() {})
	(&Timer{}).Stop()
}

func TestTimerStopIdempotent(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	timer := tw.AfterFunc(time.Millisecond*50, func() {})

	timer.Stop()
	timer.Stop()
}

func TestTimingWheelNoTimerOverlap(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	var mu sync.Mutex
	executed := make(map[int]bool)

	for i := 0; i < 10; i++ {
		delay := time.Millisecond * 25 * time.Duration(i+1)
		tw.AfterFunc(delay, func() {
			mu.Lock()
			defer mu.Unlock()
			executed[i] = true
		})
	}

	time.Sleep(time.Millisecond * 400)

	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 10; i++ {
		if !executed[i] {
			t.Errorf("Timer %d did not execute", i)
		}
	}
}

func TestTimingWheelLargeDuration(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 3600)
	tw.Start()
	defer tw.Stop()

	var executed atomic.Bool
	tw.AfterFunc(time.Second, func() {
		executed.Store(true)
	})

	time.Sleep(time.Second + time.Millisecond*100)
	if !executed.Load() {
		t.Error("Timer with 1s delay should have executed")
	}
}

func TestTimerStoppedFlag(t *testing.T) {
	tw := NewTimingWheel(time.Millisecond*10, 100)
	tw.Start()
	defer tw.Stop()

	var executed atomic.Bool
	timer := tw.AfterFunc(time.Millisecond*50, func() {
		executed.Store(true)
	})

	timer.Stop()
	time.Sleep(time.Millisecond * 100)
	if executed.Load() {
		t.Error("Stopped timer should not execute")
	}
}
