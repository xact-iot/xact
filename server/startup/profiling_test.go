package main

import "testing"

func TestProfilingDisabledByDefault(t *testing.T) {
	t.Setenv("XACT_PPROF_ADDR", "")
	stop, err := startProfiling()
	if err != nil || stop == nil {
		t.Fatalf("disabled profiler: %v", err)
	}
	stop()
}

func TestProfilingRejectsNonLoopbackListeners(t *testing.T) {
	for _, addr := range []string{":6060", "0.0.0.0:6060", "[::]:6060", "192.168.1.2:6060", "localhost:6060", "invalid"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("XACT_PPROF_ADDR", addr)
			if _, err := startProfiling(); err == nil {
				t.Fatal("non-loopback listener accepted")
			}
		})
	}
}
