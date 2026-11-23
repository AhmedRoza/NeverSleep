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
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procSetThreadExec     = kernel32.NewProc("SetThreadExecutionState")
	procGetSystemPowerSts = kernel32.NewProc("GetSystemPowerStatus")
)

type powerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func onBattery() bool {
	var p powerStatus
	r, _, _ := procGetSystemPowerSts.Call(uintptr(unsafe.Pointer(&p)))
	return r != 0 && p.ACLineStatus == 0
}

type Engine struct {
	mu         sync.Mutex
	s          Settings
	active     bool
	startedAt  time.Time
	endsAt     time.Time // zero = until stopped
	updateMsg  string
	pendingUpd string // path of a verified, downloaded update waiting to be applied
	powerCh    chan [2]bool
	wake       chan struct{}
	token      string
	port       int
	uiCmd      *exec.Cmd
}

var eng *Engine

func settingsPath() string {
	return filepath.Join(os.Getenv("APPDATA"), "neversleep", "settings.json")
}

func newEngine() *Engine {
	e := &Engine{s: LoadSettings(settingsPath()), powerCh: make(chan [2]bool, 4), wake: make(chan struct{}, 1)}
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
	sys, disp := Wanted(e.active, e.s, onBattery())
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

func (e *Engine) loop() {
	t := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-t.C:
		case <-e.wake:
		}
		e.mu.Lock()
		if e.active && !e.endsAt.IsZero() && !time.Now().Before(e.endsAt) {
			e.active = false
			e.endsAt = time.Time{}
		}
		e.applyPowerLocked()
		e.mu.Unlock()
		trayRefresh()
		maybeApplyUpdate()
	}
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) save() { _ = SaveSettings(settingsPath(), e.s) }

func (e *Engine) Toggle() {
	e.mu.Lock()
	if e.active {
		e.active = false
		e.endsAt = time.Time{}
	} else {
		e.active = true
		e.startedAt = time.Now()
		if e.s.DurationMinutes > 0 {
			e.endsAt = e.startedAt.Add(time.Duration(e.s.DurationMinutes) * time.Minute)
		}
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
}

func (e *Engine) SetPref(k, v string) {
	e.mu.Lock()
	switch {
	case k == "theme" && ValidTheme(v):
		e.s.Theme = v
	case k == "accent" && ValidAccent(v):
		e.s.Accent = strings.ToLower(v)
	}
	e.save()
	e.mu.Unlock()
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
	}
	e.save()
	e.applyPowerLocked()
	e.mu.Unlock()
	if k == "autoUpdate" && v {
		go checkForUpdate()
	}
	trayRefresh()
}

type StateJSON struct {
	Active        bool   `json:"active"`
	KeepScreen    bool   `json:"keepScreen"`
	ShowElapsed   bool   `json:"showElapsed"`
	AcOnly        bool   `json:"acOnly"`
	AutoStart     bool   `json:"autoStart"`
	AutoUpdate    bool   `json:"autoUpdate"`
	DurationMin   int    `json:"durationMin"`
	StartedAt     int64  `json:"startedAt"`
	EndsAt        int64  `json:"endsAt"`
	PausedBattery bool   `json:"pausedBattery"`
	Theme         string `json:"theme"`
	Accent        string `json:"accent"`
	Version       string `json:"version"`
	UpdateMsg     string `json:"updateMsg"`
}

func (e *Engine) Snapshot() StateJSON {
	bat := onBattery()
	e.mu.Lock()
	defer e.mu.Unlock()
	st := StateJSON{Active: e.active, KeepScreen: e.s.KeepScreenOn, ShowElapsed: e.s.ShowElapsed, AcOnly: e.s.AcOnly,
		AutoStart: e.s.AutoStart, AutoUpdate: e.s.AutoUpdate, DurationMin: e.s.DurationMinutes, Version: Version, Theme: e.s.Theme, Accent: e.s.Accent,
		UpdateMsg: e.updateMsg, PausedBattery: e.active && e.s.AcOnly && bat}
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
	mux.HandleFunc("/toggle", auth(func(w http.ResponseWriter, r *http.Request) { e.Toggle(); w.Write([]byte("ok")) }))
	mux.HandleFunc("/duration", auth(func(w http.ResponseWriter, r *http.Request) {
		m, _ := strconv.Atoi(r.URL.Query().Get("m"))
		e.SetDuration(m)
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/flag", auth(func(w http.ResponseWriter, r *http.Request) {
		k := r.URL.Query().Get("k")
		switch k {
		case "keepScreen", "acOnly", "showElapsed", "autoUpdate", "autoStart":
			e.SetFlag(k, r.URL.Query().Get("v") == "1")
		}
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/pref", auth(func(w http.ResponseWriter, r *http.Request) {
		e.SetPref(r.URL.Query().Get("k"), r.URL.Query().Get("v"))
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/quit", auth(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")); go e.Quit() }))
	go http.Serve(ln, mux)
	return nil
}

func (e *Engine) ShowUI(extra ...string) {
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
