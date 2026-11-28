package main

import (
	"fmt"
	"time"
)

// BatteryInfo is what the window and the tray popup show.
type BatteryInfo struct {
	Have     bool `json:"have"`
	Percent  int  `json:"percent"`  // -1 when Windows does not say
	Charging bool `json:"charging"` // plugged in and gaining
	OnAC     bool `json:"onAc"`
	LeftSecs int  `json:"leftSecs"` // running time left, -1 unknown
	FullSecs int  `json:"fullSecs"` // time to full while charging, -1 unknown
	Low      bool `json:"low"`      // at or under LowPercent
}

const LowPercent = 20

// A short sample history lets us estimate time-to-full while charging, because Windows
// usually reports no estimate for that direction.
type batSample struct {
	at  time.Time
	pct int
}

// estimateToFull extrapolates from the oldest sample that is at least 10 minutes old.
func estimateToFull(s []batSample, now time.Time) int {
	if len(s) < 2 {
		return -1
	}
	first := s[0]
	span := now.Sub(first.at)
	if span < 10*time.Minute {
		return -1
	}
	gain := s[len(s)-1].pct - first.pct
	if gain <= 0 {
		return -1
	}
	perPct := span.Seconds() / float64(gain)
	left := float64(100 - s[len(s)-1].pct)
	if left <= 0 {
		return 0
	}
	est := perPct * left
	if est > 12*3600 {
		return -1
	}
	return int(est)
}

// BatteryLine is the one-line battery summary used by the tray popup and the tooltip.
func BatteryLine(b BatteryInfo) string {
	if !b.Have {
		return "مفيش بطارية"
	}
	pct := "—"
	if b.Percent >= 0 {
		pct = fmt.Sprintf("%d%%", b.Percent)
	}
	switch {
	case b.Charging && b.FullSecs > 0:
		return fmt.Sprintf("%s · بيشحن، فاضل %s", pct, HumanSpanShort(time.Duration(b.FullSecs)*time.Second))
	case b.Charging:
		return pct + " · بيشحن"
	case b.OnAC:
		return pct + " · على الشاحن"
	case b.LeftSecs > 0:
		return fmt.Sprintf("%s · فاضل %s", pct, HumanSpanShort(time.Duration(b.LeftSecs)*time.Second))
	}
	return pct + " · على البطارية"
}
