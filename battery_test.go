package main

import (
	"testing"
	"time"
)

func TestEstimateToFull(t *testing.T) {
	now := time.Now()
	if got := estimateToFull(nil, now); got != -1 {
		t.Fatalf("no samples -> %d", got)
	}
	one := []batSample{{now.Add(-time.Hour), 40}}
	if got := estimateToFull(one, now); got != -1 {
		t.Fatalf("one sample -> %d", got)
	}
	// 40% -> 60% over 40 minutes is 2 minutes per percent, 40 to go = 80 minutes
	two := []batSample{{now.Add(-40 * time.Minute), 40}, {now, 60}}
	if got := estimateToFull(two, now); got != 80*60 {
		t.Fatalf("40->60 over 40m -> %d want %d", got, 80*60)
	}
	// too short a span is not trusted
	short := []batSample{{now.Add(-2 * time.Minute), 40}, {now, 41}}
	if got := estimateToFull(short, now); got != -1 {
		t.Fatalf("short span -> %d", got)
	}
	// not gaining: no estimate
	flat := []batSample{{now.Add(-40 * time.Minute), 50}, {now, 50}}
	if got := estimateToFull(flat, now); got != -1 {
		t.Fatalf("flat -> %d", got)
	}
	// already full
	full := []batSample{{now.Add(-40 * time.Minute), 90}, {now, 100}}
	if got := estimateToFull(full, now); got != 0 {
		t.Fatalf("full -> %d", got)
	}
	// absurdly slow charge is dropped rather than shown as a huge number
	slow := []batSample{{now.Add(-10 * time.Hour), 1}, {now, 2}}
	if got := estimateToFull(slow, now); got != -1 {
		t.Fatalf("slow -> %d", got)
	}
}
