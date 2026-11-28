//go:build windows

package main

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GetSystemPowerStatus is the cheapest honest battery read on Windows: one call, no COM, no WMI.
type sysPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

const (
	batFlagNoBattery = 128
	batFlagCharging  = 8
	unknownSeconds   = 0xFFFFFFFF
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// sampleMu guards only the sample slice. readBattery itself takes no lock, so it can never
// be involved in a lock-order cycle with the engine mutex.
var (
	sampleMu   sync.Mutex
	batSamples []batSample
)

func readBattery() BatteryInfo {
	var p sysPowerStatus
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		return BatteryInfo{Percent: -1, LeftSecs: -1, FullSecs: -1}
	}
	b := BatteryInfo{LeftSecs: -1, FullSecs: -1}
	b.OnAC = p.ACLineStatus == 1
	b.Have = p.BatteryFlag&batFlagNoBattery == 0
	if !b.Have {
		b.Percent = -1
		return b
	}
	if p.BatteryLifePercent != 255 {
		b.Percent = int(p.BatteryLifePercent)
	} else {
		b.Percent = -1
	}
	b.Charging = p.BatteryFlag&batFlagCharging != 0
	if p.BatteryLifeTime != unknownSeconds && p.BatteryLifeTime > 0 && !b.Charging {
		b.LeftSecs = int(p.BatteryLifeTime)
	}
	if p.BatteryFullLifeTime != unknownSeconds && p.BatteryFullLifeTime > 0 {
		b.FullSecs = int(p.BatteryFullLifeTime)
	}
	b.Low = b.Percent >= 0 && b.Percent <= LowPercent && !b.Charging

	// charging estimate from our own samples (no polling thread: sampled when someone asks)
	sampleMu.Lock()
	if b.Charging && b.Percent >= 0 {
		now := time.Now()
		if n := len(batSamples); n == 0 || batSamples[n-1].pct != b.Percent {
			batSamples = append(batSamples, batSample{now, b.Percent})
			if len(batSamples) > 24 {
				batSamples = batSamples[len(batSamples)-24:]
			}
		}
		if b.FullSecs < 0 {
			if est := estimateToFull(batSamples, now); est > 0 {
				b.FullSecs = est
			}
		}
	} else if !b.Charging {
		batSamples = nil
	}
	sampleMu.Unlock()
	return b
}

// resetBatterySamples drops the charging history (charger unplugged, or a test).
func resetBatterySamples() {
	sampleMu.Lock()
	batSamples = nil
	sampleMu.Unlock()
}
