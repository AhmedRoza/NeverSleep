//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed assets/tray-on.png
var trayOnPNG []byte

//go:embed assets/tray-off.png
var trayOffPNG []byte

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	shell32                   = windows.NewLazySystemDLL("shell32.dll")
	pRegisterClassEx          = user32.NewProc("RegisterClassExW")
	pCreateWindowEx           = user32.NewProc("CreateWindowExW")
	pDefWindowProc            = user32.NewProc("DefWindowProcW")
	pGetMessage               = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessage          = user32.NewProc("DispatchMessageW")
	pPostMessage              = user32.NewProc("PostMessageW")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenu               = user32.NewProc("AppendMenuW")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pSetForeground            = user32.NewProc("SetForegroundWindow")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pRegisterWindowMessage    = user32.NewProc("RegisterWindowMessageW")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pFindWindow               = user32.NewProc("FindWindowW")
	pShowWindow               = user32.NewProc("ShowWindow")
	pIsIconic                 = user32.NewProc("IsIconic")
	pShellNotifyIcon          = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmApp       = 0x8000
	wmTray      = wmApp + 1
	wmShowMain  = wmApp + 2
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmDblClk    = 0x0203
	nimAdd      = 0
	nimModify   = 1
	nimDelete   = 2
	nifMessage  = 1
	nifIcon     = 2
	nifTip      = 4
	idShow      = 1
	idToggle    = 2
	idAuto      = 3
	idQuit      = 4
	idDurBase   = 100
	mfString    = 0
	mfChecked   = 8
	mfSeparator = 0x800
	mfPopup     = 0x10
	tpmReturn   = 0x100
	tpmRight    = 0x8
	swRestore   = 9
	swShow      = 5
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type point struct{ X, Y int32 }
type msg struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconData struct {
	Size            uint32
	Hwnd            windows.Handle
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     windows.Handle
}

var (
	trayHwnd   windows.Handle
	iconOn     windows.Handle
	iconOff    windows.Handle
	taskbarMsg uint32
	trayMu     sync.Mutex
	trayReady  bool
	lastTip    string
	lastIconOn bool
	trayCls    = "NeverSleepTrayWnd"
)

func loadPNGIcon(b []byte, size int) windows.Handle {
	h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return windows.Handle(h)
}

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func notify(op uint32, on bool, tip string) {
	var d notifyIconData
	d.Size = uint32(unsafe.Sizeof(d))
	d.Hwnd = trayHwnd
	d.ID = 1
	d.Flags = nifMessage | nifIcon | nifTip
	d.CallbackMessage = wmTray
	if on {
		d.Icon = iconOn
	} else {
		d.Icon = iconOff
	}
	t, _ := windows.UTF16FromString(tip)
	if len(t) > 127 {
		t = t[:127]
		t[126] = 0
	}
	copy(d.Tip[:], t)
	pShellNotifyIcon.Call(uintptr(op), uintptr(unsafe.Pointer(&d)))
}

func tipFor(st StateJSON) (string, bool) {
	switch {
	case st.PausedBattery:
		return "Never Sleep: متوقف مؤقتًا (على البطارية)", false
	case st.Active:
		if st.EndsAt > 0 {
			left := time.Until(time.Unix(st.EndsAt, 0))
			return fmt.Sprintf("Never Sleep: شغال، متبقي %d د", int(left.Minutes())+1), true
		}
		if st.ShowElapsed {
			el := time.Since(time.Unix(st.StartedAt, 0))
			return fmt.Sprintf("Never Sleep: شغال بقاله %d:%02d", int(el.Hours()), int(el.Minutes())%60), true
		}
		return "Never Sleep: شغال", true
	}
	return "Never Sleep: متوقف", false
}

func trayRefresh() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if !trayReady || eng == nil {
		return
	}
	tip, on := tipFor(eng.Snapshot())
	if tip == lastTip && on == lastIconOn {
		return
	}
	lastTip, lastIconOn = tip, on
	notify(nimModify, on, tip)
}

func trayRemove() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayReady {
		notify(nimDelete, false, "")
		trayReady = false
	}
}

func showMenu(hwnd windows.Handle) {
	st := eng.Snapshot()
	menu, _, _ := pCreatePopupMenu.Call()
	sub, _, _ := pCreatePopupMenu.Call()
	durs := []struct {
		m int
		l string
	}{{0, "لحد ما أوقفه"}, {60, "ساعة"}, {120, "ساعتين"}, {240, "٤ ساعات"}}
	for i, d := range durs {
		f := uintptr(mfString)
		if st.DurationMin == d.m {
			f |= mfChecked
		}
		pAppendMenu.Call(sub, f, uintptr(idDurBase+i), uintptr(unsafe.Pointer(u16(d.l))))
	}
	pAppendMenu.Call(menu, mfString, idShow, uintptr(unsafe.Pointer(u16("فتح Never Sleep"))))
	lbl := "تشغيل منع السكون"
	if st.Active {
		lbl = "إيقاف منع السكون"
	}
	pAppendMenu.Call(menu, mfString, idToggle, uintptr(unsafe.Pointer(u16(lbl))))
	pAppendMenu.Call(menu, mfPopup, sub, uintptr(unsafe.Pointer(u16("المدة"))))
	f := uintptr(mfString)
	if st.AutoStart {
		f |= mfChecked
	}
	pAppendMenu.Call(menu, f, idAuto, uintptr(unsafe.Pointer(u16("تشغيل مع ويندوز"))))
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	pAppendMenu.Call(menu, mfString, idQuit, uintptr(unsafe.Pointer(u16("إيقاف وخروج"))))
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(uintptr(hwnd))
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmReturn|tpmRight, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	pPostMessage.Call(uintptr(hwnd), 0, 0, 0)
	pDestroyMenu.Call(menu)
	switch {
	case cmd == idShow:
		eng.ShowUI()
	case cmd == idToggle:
		eng.Toggle()
	case cmd == idAuto:
		eng.SetFlag("autoStart", !st.AutoStart)
	case cmd == idQuit:
		eng.Quit()
	case cmd >= idDurBase && cmd < idDurBase+4:
		eng.SetDuration(durs[cmd-idDurBase].m)
	}
}

func trayProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmTray:
		switch uint32(lp) {
		case wmLButtonUp, wmDblClk:
			eng.ShowUI()
		case wmRButtonUp:
			showMenu(windows.Handle(hwnd))
		}
		return 0
	case wmShowMain:
		eng.ShowUI()
		return 0
	}
	if taskbarMsg != 0 && uint32(m) == taskbarMsg { // Explorer restarted: re-add the icon
		trayMu.Lock()
		notify(nimAdd, lastIconOn, lastTip)
		trayMu.Unlock()
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

// runTray owns a hidden window and the message loop on a dedicated OS thread.
func runTray() {
	runtime.LockOSThread()
	iconOn = loadPNGIcon(trayOnPNG, 32)
	iconOff = loadPNGIcon(trayOffPNG, 32)
	hi, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	hinst := windows.Handle(hi)
	cb := windows.NewCallback(trayProc)
	var wc wndClassEx
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = cb
	wc.Instance = hinst
	wc.ClassName = u16(trayCls)
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	h, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16(trayCls))), uintptr(unsafe.Pointer(u16("NeverSleepTray"))), 0, 0, 0, 0, 0, 0, 0, uintptr(hinst), 0)
	trayHwnd = windows.Handle(h)
	m, _, _ := pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	taskbarMsg = uint32(m)
	tip, on := tipFor(eng.Snapshot())
	trayMu.Lock()
	lastTip, lastIconOn, trayReady = tip, on, true
	notify(nimAdd, on, tip)
	trayMu.Unlock()
	var ms msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&ms)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&ms)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&ms)))
	}
}

// signalRunningInstance asks an already running instance to show its window.
func signalRunningInstance() bool {
	h, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(u16(trayCls))), 0)
	if h == 0 {
		return false
	}
	pPostMessage.Call(h, wmShowMain, 0, 0)
	return true
}

func focusUI() {
	h, _, _ := pFindWindow.Call(0, uintptr(unsafe.Pointer(u16("Never Sleep"))))
	if h == 0 {
		return
	}
	if ic, _, _ := pIsIconic.Call(h); ic != 0 {
		pShowWindow.Call(h, swRestore)
	} else {
		pShowWindow.Call(h, swShow)
	}
	pSetForeground.Call(h)
}
