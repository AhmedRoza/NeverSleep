//go:build windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
	esDisplayRequire = 0x00000002
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procSetThreadExec = kernel32.NewProc("SetThreadExecutionState")
	procTickCount64   = kernel32.NewProc("GetTickCount64")
)

// uptimeSeconds is how long Windows has been running, straight from the kernel counter.
func uptimeSeconds() int64 {
	r, _, _ := procTickCount64.Call()
	return int64(r)
}

type Engine struct {
	mu          sync.Mutex
	s           Settings
	active      bool
	startedAt   time.Time
	endsAt      time.Time // zero = until stopped
	updateMsg   string
	pendingUpd  string // path of a verified, downloaded update waiting to be applied
	smartIn     bool   // inside the smart-sleep window right now
	smartPaused bool   // smart sleep turned keep-awake off; cleared by the next manual start
	powerCh     chan [2]bool
	wake        chan struct{}
	token       string
	port        int
	uiCmd       *exec.Cmd
}

var eng *Engine

func settingsPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "neversleep", "settings.json")
}

func newEngine() *Engine {
	e := &Engine{s: LoadSettings(settingsPath()), powerCh: make(chan [2]bool, 4), wake: make(chan struct{}, 1)}
	e.smartIn = e.s.SmartSleep && InWindow(time.Now(), e.s.SmartFrom, e.s.SmartTo)
	go e.powerWorker()
	go e.loop()
	return e
}

// Power: one process-wide PowerCreateRequest handle (visible in "powercfg /requests").
// It does not depend on which thread set it. If the API is unavailable we fall back to
// SetThreadExecutionState on one dedicated locked thread that stays alive for the whole process.
type reasonContext struct {
	Version uint32
	Flags   uint32
	Reason  *uint16
	_       uintptr
}

var (
	procPowerCreate = kernel32.NewProc("PowerCreateRequest")
	procPowerSet    = kernel32.NewProc("PowerSetRequest")
	procPowerClear  = kernel32.NewProc("PowerClearRequest")
)

const (
	reqDisplay = 0
	reqSystem  = 1
)

type powerCtl struct {
	h        windows.Handle
	haveReq  bool
	sys, dsp bool
}

func newPowerCtl() *powerCtl {
	c := &powerCtl{}
	rc := reasonContext{Version: 0, Flags: 1, Reason: u16("Never Sleep keeps the PC awake while it is switched on")}
	h, _, _ := procPowerCreate.Call(uintptr(unsafe.Pointer(&rc)))
	if h != 0 && h != uintptr(windows.InvalidHandle) {
		c.h, c.haveReq = windows.Handle(h), true
	}
	return c
}

func (c *powerCtl) set(sys, dsp bool) {
	if !c.haveReq {
		return
	}
	do := func(kind uintptr, want, have bool) {
		if want && !have {
			procPowerSet.Call(uintptr(c.h), kind)
		} else if !want && have {
			procPowerClear.Call(uintptr(c.h), kind)
		}
	}
	do(reqSystem, sys, c.sys)
	do(reqDisplay, dsp, c.dsp)
	c.sys, c.dsp = sys, dsp
}

func (c *powerCtl) close() {
	c.set(false, false)
	if c.haveReq {
		windows.CloseHandle(c.h)
		c.haveReq = false
	}
}

// powerSelfTest checks the power API round trip without keeping anything awake.
func powerSelfTest() bool {
	c := newPowerCtl()
	if !c.haveReq {
		return false
	}
	c.set(true, false)
	c.set(false, false)
	c.close()
	return true
}

func (e *Engine) powerWorker() {
	runtime.LockOSThread() // never unlocked and never returns: the thread lives as long as the process
	ctl := newPowerCtl()
	for w := range e.powerCh {
		if ctl.haveReq {
			ctl.set(w[0], w[1])
			continue
		}
		flags := uintptr(esContinuous)
		if w[0] {
			flags |= esSystemRequired
		}
		if w[1] {
			flags |= esDisplayRequire
		}
		procSetThreadExec.Call(flags)
	}
	ctl.close()
}

func (e *Engine) applyPowerLocked() {
	sys, disp := Wanted(e.active, e.s, !readBattery().OnAC)
	select {
	case e.powerCh <- [2]bool{sys, disp}:
	default:
		select {
		case <-e.powerCh:
		default:
		}
		select {
		case e.powerCh <- [2]bool{sys, disp}:
		default:
		}
	}
}

// loop is the only timer in the app. It sleeps 20 s while idle and wakes exactly when a
// countdown ends, so a switched-off app costs almost nothing.
func (e *Engine) loop() {
	for {
		e.mu.Lock()
		active, ends := e.active, e.endsAt
		e.mu.Unlock()
		d := 20 * time.Second
		if active && !ends.IsZero() {
			d = time.Until(ends) + 150*time.Millisecond
			if d < 250*time.Millisecond {
				d = 250 * time.Millisecond
			}
		}
		select {
		case <-time.After(d):
		case <-e.wake:
		}
		e.tick()
		trayRefresh()
		maybeApplyUpdate()
	}
}

// tick applies the clock: smart-sleep window, countdown end, power requests.
func (e *Engine) tick() {
	now := time.Now()
	e.mu.Lock()
	in := e.s.SmartSleep && InWindow(now, e.s.SmartFrom, e.s.SmartTo)
	if in && !e.smartIn && e.active {
		e.active = false
		e.endsAt = time.Time{}
		e.smartPaused = true
	}
	e.smartIn = in
	if e.active && !e.endsAt.IsZero() && !now.Before(e.endsAt) {
		e.active = false
		e.endsAt = time.Time{}
	}
	e.applyPowerLocked()
	e.mu.Unlock()
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) save() { _ = SaveSettings(settingsPath(), e.s) }

// endForLocked is when a start should end: the until-clock if that mode is on, else the duration.
func (e *Engine) endForLocked(now time.Time) time.Time {
	if e.s.TimerMode == "until" {
		if t, ok := NextClock(now, e.s.UntilTime); ok {
			return t
		}
	}
	if e.s.DurationMinutes > 0 {
		return now.Add(time.Duration(e.s.DurationMinutes) * time.Minute)
	}
	return time.Time{}
}

func (e *Engine) Toggle() {
	e.mu.Lock()
	if e.active {
		e.active = false
		e.endsAt = time.Time{}
	} else {
		e.active = true
		e.smartPaused = false
		e.startedAt = time.Now()
		e.endsAt = e.endForLocked(e.startedAt)
	}
	e.applyPowerLocked()
	e.mu.Unlock()
	trayRefresh()
}

func (e *Engine) SetDuration(m int) {
	if !ValidDuration(m) {
		m = 0
	}
	e.mu.Lock()
	e.s.DurationMinutes = m
	e.s.TimerMode = "duration"
	e.smartPaused = false
	if e.active {
		if m > 0 {
			e.endsAt = time.Now().Add(time.Duration(m) * time.Minute)
		} else {
			e.endsAt = time.Time{}
		}
	}
	e.save()
	e.mu.Unlock()
	trayRefresh()
	e.poke()
}

// SetUntil switches to "awake until HH:MM" and starts keep-awake if it is off.
func (e *Engine) SetUntil(clock string) bool {
	if !ValidClock(clock) {
		return false
	}
	now := time.Now()
	end, ok := NextClock(now, clock)
	if !ok {
		return false
	}
	e.mu.Lock()
	e.s.UntilTime = clock
	e.s.TimerMode = "until"
	e.smartPaused = false
	e.active = true
	e.startedAt = now
	e.endsAt = end
	e.save()
	e.applyPowerLocked()
	e.mu.Unlock()
	trayRefresh()
	e.poke()
	return true
}

func (e *Engine) SetPref(k, v string) {
	e.mu.Lock()
	switch {
	case k == "theme" && ValidTheme(v):
		e.s.Theme = v
	case k == "accent" && ValidAccent(v):
		e.s.Accent = strings.ToLower(v)
	case k == "untilTime" && ValidClock(v):
		e.s.UntilTime = v
	case k == "timerMode" && ValidTimerMode(v):
		e.s.TimerMode = v
	case k == "smartFrom" && ValidClock(v):
		e.s.SmartFrom = v
		e.smartIn = e.s.SmartSleep && InWindow(time.Now(), e.s.SmartFrom, e.s.SmartTo)
	case k == "smartTo" && ValidClock(v):
		e.s.SmartTo = v
		e.smartIn = e.s.SmartSleep && InWindow(time.Now(), e.s.SmartFrom, e.s.SmartTo)
	}
	e.save()
	e.mu.Unlock()
	e.poke()
}

func (e *Engine) SetFlag(k string, v bool) {
	e.mu.Lock()
	switch k {
	case "keepScreen":
		e.s.KeepScreenOn = v
	case "acOnly":
		e.s.AcOnly = v
	case "showElapsed":
		e.s.ShowElapsed = v
	case "autoUpdate":
		e.s.AutoUpdate = v
	case "autoStart":
		e.s.AutoStart = v
		setRunKey(v)
	case "smartSleep":
		e.s.SmartSleep = v
		e.smartIn = v && InWindow(time.Now(), e.s.SmartFrom, e.s.SmartTo)
		if !v {
			e.smartPaused = false
		}
	case "hoverInfo":
		e.s.HoverInfo = v
	}
	e.save()
	e.applyPowerLocked()
	e.mu.Unlock()
	if k == "autoUpdate" && v {
		go checkForUpdate()
	}
	if k == "hoverInfo" && !v {
		tipShow(false)
	}
	trayRefresh()
	e.poke()
}

// CheckUpdate is the "check now" button: it reports back even when there is nothing new.
func (e *Engine) CheckUpdate() {
	if !runningInstalled() {
		e.setMsg("التحديث التلقائي شغال من النسخة المتثبتة بس")
		return
	}
	e.setMsg("بيشيّك على تحديث…")
	go func() {
		if !checkForUpdate() {
			e.setMsg("إنت على آخر نسخة (v" + Version + ")")
			time.AfterFunc(8*time.Second, func() {
				e.mu.Lock()
				if strings.HasPrefix(e.updateMsg, "إنت على آخر نسخة") {
					e.updateMsg = ""
				}
				e.mu.Unlock()
				trayRefresh()
			})
		}
	}()
}

type StateJSON struct {
	Active        bool        `json:"active"`
	KeepScreen    bool        `json:"keepScreen"`
	ShowElapsed   bool        `json:"showElapsed"`
	AcOnly        bool        `json:"acOnly"`
	AutoStart     bool        `json:"autoStart"`
	AutoUpdate    bool        `json:"autoUpdate"`
	DurationMin   int         `json:"durationMin"`
	StartedAt     int64       `json:"startedAt"`
	EndsAt        int64       `json:"endsAt"`
	PausedBattery bool        `json:"pausedBattery"`
	Theme         string      `json:"theme"`
	Accent        string      `json:"accent"`
	Version       string      `json:"version"`
	UpdateMsg     string      `json:"updateMsg"`
	Battery       BatteryInfo `json:"battery"`
	UptimeSecs    int64       `json:"uptimeSecs"`
	TimerMode     string      `json:"timerMode"`
	UntilTime     string      `json:"untilTime"`
	SmartSleep    bool        `json:"smartSleep"`
	SmartFrom     string      `json:"smartFrom"`
	SmartTo       string      `json:"smartTo"`
	SmartOn       bool        `json:"smartOn"`
	SmartPaused   bool        `json:"smartPaused"`
	HoverInfo     bool        `json:"hoverInfo"`
}

func (e *Engine) Snapshot() StateJSON {
	bat := readBattery()
	e.mu.Lock()
	defer e.mu.Unlock()
	st := StateJSON{Active: e.active, KeepScreen: e.s.KeepScreenOn, ShowElapsed: e.s.ShowElapsed, AcOnly: e.s.AcOnly,
		AutoStart: e.s.AutoStart, AutoUpdate: e.s.AutoUpdate, DurationMin: e.s.DurationMinutes, Version: Version,
		Theme: e.s.Theme, Accent: e.s.Accent, UpdateMsg: e.updateMsg,
		PausedBattery: e.active && e.s.AcOnly && !bat.OnAC,
		Battery:       bat, UptimeSecs: uptimeSeconds(),
		TimerMode: e.s.TimerMode, UntilTime: e.s.UntilTime,
		SmartSleep: e.s.SmartSleep, SmartFrom: e.s.SmartFrom, SmartTo: e.s.SmartTo,
		SmartOn: e.smartIn, SmartPaused: e.smartPaused, HoverInfo: e.s.HoverInfo}
	if e.active {
		st.StartedAt = e.startedAt.Unix()
		if !e.endsAt.IsZero() {
			st.EndsAt = e.endsAt.Unix()
		}
	}
	return st
}

func (e *Engine) setMsg(m string) {
	e.mu.Lock()
	e.updateMsg = m
	e.mu.Unlock()
	trayRefresh()
}

// Quit releases every power request so Windows sleeps normally again, then exits.
func (e *Engine) Quit() {
	e.mu.Lock()
	e.active = false
	e.mu.Unlock()
	e.mu.Lock()
	if e.uiCmd != nil && e.uiCmd.Process != nil {
		_ = e.uiCmd.Process.Kill() // never leave an orphan window talking to a dead control channel
	}
	e.mu.Unlock()
	e.applyPowerLocked() // note: active is already false, so the worker clears every request
	select {
	case e.powerCh <- [2]bool{false, false}:
	default:
	}
	time.Sleep(300 * time.Millisecond)
	tipShow(false)
	trayRemove()
	os.Exit(0)
}

// ---- local control channel for the UI process (loopback + random token) ----

func (e *Engine) serve() error {
	b := make([]byte, 16)
	rand.Read(b)
	e.token = hex.EncodeToString(b)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	e.port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-NS") != e.token {
				http.Error(w, "no", 403)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/state", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(e.Snapshot())
	}))
	mux.HandleFunc("/devices", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(devicesCached())
	}))
	mux.HandleFunc("/toggle", auth(func(w http.ResponseWriter, r *http.Request) { e.Toggle(); w.Write([]byte("ok")) }))
	mux.HandleFunc("/duration", auth(func(w http.ResponseWriter, r *http.Request) {
		m, _ := strconv.Atoi(r.URL.Query().Get("m"))
		e.SetDuration(m)
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/until", auth(func(w http.ResponseWriter, r *http.Request) {
		if !e.SetUntil(r.URL.Query().Get("t")) {
			http.Error(w, "bad time", 400)
			return
		}
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/flag", auth(func(w http.ResponseWriter, r *http.Request) {
		k := r.URL.Query().Get("k")
		switch k {
		case "keepScreen", "acOnly", "showElapsed", "autoUpdate", "autoStart", "smartSleep", "hoverInfo":
			e.SetFlag(k, r.URL.Query().Get("v") == "1")
		}
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/pref", auth(func(w http.ResponseWriter, r *http.Request) {
		e.SetPref(r.URL.Query().Get("k"), r.URL.Query().Get("v"))
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/updatecheck", auth(func(w http.ResponseWriter, r *http.Request) { e.CheckUpdate(); w.Write([]byte("ok")) }))
	mux.HandleFunc("/quit", auth(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")); go e.Quit() }))
	go http.Serve(ln, mux)
	return nil
}

func (e *Engine) ShowUI() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.uiCmd != nil {
		focusUI()
		return
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "--ui", strconv.Itoa(e.port), e.token)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	if err := cmd.Start(); err != nil {
		return
	}
	e.uiCmd = cmd
	go func() { cmd.Wait(); e.mu.Lock(); e.uiCmd = nil; e.mu.Unlock() }()
}
