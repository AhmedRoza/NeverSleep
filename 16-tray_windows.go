//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed assets/tray-awake.png
var trayAwakePNG []byte

//go:embed assets/tray-sleep.png
var traySleepPNG []byte

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	gdi32                     = windows.NewLazySystemDLL("gdi32.dll")
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
	pSetTimer                 = user32.NewProc("SetTimer")
	pKillTimer                = user32.NewProc("KillTimer")
	pTrackMouseEvent          = user32.NewProc("TrackMouseEvent")
	pGetClientRect            = user32.NewProc("GetClientRect")
	pBeginPaint               = user32.NewProc("BeginPaint")
	pEndPaint                 = user32.NewProc("EndPaint")
	pInvalidateRect           = user32.NewProc("InvalidateRect")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pGetDC                    = user32.NewProc("GetDC")
	pReleaseDC                = user32.NewProc("ReleaseDC")
	pGetSysColor              = user32.NewProc("GetSysColor")
	pGetSysColorBrush         = user32.NewProc("GetSysColorBrush")
	pFillRect                 = gdi32.NewProc("FillRect")
	pFrameRect                = gdi32.NewProc("FrameRect")
	pSelectObject             = gdi32.NewProc("SelectObject")
	pSetBkMode                = gdi32.NewProc("SetBkMode")
	pSetTextColor             = gdi32.NewProc("SetTextColor")
	pDrawText                 = user32.NewProc("DrawTextW")
	pCreateFontW              = gdi32.NewProc("CreateFontW")
)

const (
	wmApp            = 0x8000
	wmTray           = wmApp + 1
	wmShowMain       = wmApp + 2
	wmMouseMove      = 0x0200
	wmMouseLeave     = 0x02A3
	wmTimerMsg       = 0x0113
	wmPaint          = 0x000F
	wmDeviceChange   = 0x0219
	wmClose          = 0x0010
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmRButtonUp      = 0x0205
	wmLButtonDblClk  = 0x0203
	nimAdd           = 0
	nimModify        = 1
	nimDelete        = 2
	nifMessage       = 1
	nifIcon          = 2
	nifTip           = 4
	idShow           = 1
	idToggle         = 2
	idAuto           = 3
	idQuit           = 4
	idSmart          = 5
	idStats          = 6
	idDurBase        = 100
	idClickTimer     = 11
	idTipTimer       = 12
	mfString         = 0
	mfChecked        = 8
	mfSeparator      = 0x800
	mfPopup          = 0x10
	tpmReturn        = 0x100
	tpmRight         = 0x8
	swRestore        = 9
	swShow           = 5
	swShowNoActivate = 4
	swHide           = 0

	hwndTopmost      = ^uintptr(0) // -1
	swpNoActivate    = 0x0010
	swpShowWindow    = 0x0040
	smCxScreen       = 0
	smCyScreen       = 1
	colorWindowFrame = 6
	colorInfoText    = 23
	colorInfoBk      = 24
	tmeLeave         = 0x00000002
	dtLeft           = 0x0000
	dtTop            = 0x0000
	dtNoPrefix       = 0x0020
	dtWordBreak      = 0x0010
	dtCalcRect       = 0x0400
	dtRtlReading     = 0x20000
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
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type paintStruct struct {
	hdc         windows.Handle
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type trackMouseEvent struct {
	cbSize      uint32
	dwFlags     uint32
	hwndTrack   windows.Handle
	dwHoverTime uint32
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
	trayHwnd     windows.Handle
	iconAwake    windows.Handle
	iconSleep    windows.Handle
	taskbarMsg   uint32
	trayMu       sync.Mutex
	trayReady    bool
	lastTip      string
	lastIconOn   bool
	lastHover    bool
	trayCls      = "NeverSleepTrayWnd"
	tipCls       = "NeverSleepTipWnd"
	tipHwnd      windows.Handle
	tipFont      windows.Handle
	tipText      string
	tipVisible   bool
	tipMu        sync.Mutex
	pendingClick bool
	lastDbl      time.Time
)

func loadPNGIcon(b []byte, size int) windows.Handle {
	h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return windows.Handle(h)
}

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

// ---- hover popup: a tiny owner-painted window, created once and only shown on hover ----

func tipLines() []string {
	st := eng.Snapshot()
	out := []string{Clock12(time.Now())}
	switch {
	case st.PausedBattery:
		out = append(out, "متوقف مؤقتًا (على البطارية)")
	case st.Active && st.EndsAt > 0:
		out = append(out, "شغال · متبقي "+HumanSpanShort(time.Until(time.Unix(st.EndsAt, 0))))
	case st.Active && st.ShowElapsed:
		out = append(out, "شغال بقاله "+HumanSpanShort(time.Since(time.Unix(st.StartedAt, 0))))
	case st.Active:
		out = append(out, "شغال")
	default:
		out = append(out, "متوقف")
	}
	out = append(out, BatteryLine(st.Battery))
	if st.SmartOn {
		out = append(out, "وضع النوم لحد "+clock12(st.SmartTo))
	}
	return out
}

// clock12 renders a stored "HH:MM" the way the user reads time: 7:00 ص.
func clock12(clock string) string {
	m, ok := ParseClock(clock)
	if !ok {
		return clock
	}
	return Clock12(time.Date(2000, 1, 1, m/60, m%60, 0, 0, time.Local))
}

func measureTip(hdc uintptr, text string, width int32) (int32, int32) {
	r := rect{0, 0, width, 0}
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(u16(text))), ^uintptr(0), uintptr(unsafe.Pointer(&r)),
		uintptr(dtCalcRect|dtLeft|dtTop|dtNoPrefix|dtWordBreak|dtRtlReading))
	return r.Right - r.Left, r.Bottom - r.Top
}

// tipRender rebuilds the text, resizes the popup and parks it above the cursor.
func tipRender() {
	txt := strings.Join(tipLines(), "\r\n")
	hdc, _, _ := pGetDC.Call(0)
	if hdc == 0 {
		return
	}
	pSelectObject.Call(hdc, uintptr(tipFont))
	w, h := measureTip(hdc, txt, 240)
	pReleaseDC.Call(0, hdc)
	if w < 150 {
		w = 150
	}
	w += 22
	h += 18

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	x := pt.X - w/2
	y := pt.Y - h - 26
	if y < 0 {
		y = pt.Y + 26
	}
	sw, _, _ := pGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := pGetSystemMetrics.Call(smCyScreen)
	if x < 0 {
		x = 0
	}
	if x+w > int32(sw) {
		x = int32(sw) - w
	}
	if y+h > int32(sh) {
		y = int32(sh) - h
	}
	if y < 0 {
		y = 0
	}
	tipMu.Lock()
	tipText = txt
	tipMu.Unlock()
	pSetWindowPos.Call(uintptr(tipHwnd), hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoActivate|swpShowWindow)
	pInvalidateRect.Call(uintptr(tipHwnd), 0, 1)
}

func tipShow(on bool) {
	if tipHwnd == 0 {
		return
	}
	if !on {
		if tipVisible {
			pKillTimer.Call(uintptr(tipHwnd), idTipTimer)
			pShowWindow.Call(uintptr(tipHwnd), swHide)
			tipVisible = false
		}
		return
	}
	if eng == nil {
		return
	}
	if st := eng.Snapshot(); !st.HoverInfo {
		return
	}
	tipRender()
	pSetTimer.Call(uintptr(tipHwnd), idTipTimer, 1000, 0)
	tipVisible = true
}

func tipProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			var rc rect
			pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
			if br, _, _ := pGetSysColorBrush.Call(colorInfoBk); br != 0 {
				pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), br)
			}
			if br, _, _ := pGetSysColorBrush.Call(colorWindowFrame); br != 0 {
				pFrameRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), br)
			}
			pSelectObject.Call(hdc, uintptr(tipFont))
			pSetBkMode.Call(hdc, 1) // TRANSPARENT
			if col, _, _ := pGetSysColor.Call(colorInfoText); col != 0 {
				pSetTextColor.Call(hdc, col)
			}
			tipMu.Lock()
			txt := tipText
			tipMu.Unlock()
			r2 := rect{rc.Left + 11, rc.Top + 9, rc.Right - 11, rc.Bottom - 9}
			pDrawText.Call(hdc, uintptr(unsafe.Pointer(u16(txt))), ^uintptr(0), uintptr(unsafe.Pointer(&r2)),
				uintptr(dtLeft|dtTop|dtNoPrefix|dtWordBreak|dtRtlReading))
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmTimerMsg:
		if wp == idTipTimer {
			if tipVisible {
				tipRender()
			}
		}
		return 0
	case wmMouseLeave:
		tipShow(false)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

func tipCreate(hinst windows.Handle) {
	cb := windows.NewCallback(tipProc)
	var wc wndClassEx
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = cb
	wc.Instance = hinst
	wc.ClassName = u16(tipCls)
	wc.Background = windows.Handle(colorInfoBk + 1) // COLOR_INFOBK + 1 is the matching brush
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	h13 := -13
	fh, _, _ := pCreateFontW.Call(uintptr(h13), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(u16("Segoe UI"))))
	tipFont = windows.Handle(fh)
	h, _, _ := pCreateWindowEx.Call(0x00000008|0x00000080|0x08000000, // WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE
		uintptr(unsafe.Pointer(u16(tipCls))), uintptr(unsafe.Pointer(u16(""))), 0x80000000,
		0, 0, 10, 10, 0, 0, uintptr(hinst), 0)
	tipHwnd = windows.Handle(h)
}

var _ = fmt.Sprintf

// ---- tray icon ----

func notify(op uint32, on bool, tip string, withTip bool) {
	var d notifyIconData
	d.Size = uint32(unsafe.Sizeof(d))
	d.Hwnd = trayHwnd
	d.ID = 1
	d.Flags = nifMessage | nifIcon | nifTip
	d.CallbackMessage = wmTray
	if on {
		d.Icon = iconAwake
	} else {
		d.Icon = iconSleep
	}
	if withTip {
		t, _ := windows.UTF16FromString(tip)
		if len(t) > 127 {
			t = t[:127]
			t[126] = 0
		}
		copy(d.Tip[:], t)
	}
	pShellNotifyIcon.Call(uintptr(op), uintptr(unsafe.Pointer(&d)))
}

// tipFor is the classic tooltip, used only when the hover popup is switched off.
func tipFor(st StateJSON) (string, bool) {
	on := st.Active && !st.PausedBattery
	var s string
	switch {
	case st.PausedBattery:
		s = "Never Sleep: متوقف مؤقتًا (على البطارية)"
	case st.Active && st.EndsAt > 0:
		s = "Never Sleep: شغال، متبقي " + HumanSpanShort(time.Until(time.Unix(st.EndsAt, 0)))
	case st.Active && st.ShowElapsed:
		s = "Never Sleep: شغال بقاله " + HumanSpanShort(time.Since(time.Unix(st.StartedAt, 0)))
	case st.Active:
		s = "Never Sleep: شغال"
	case st.SmartPaused:
		s = "Never Sleep: متوقف (وضع النوم)"
	default:
		s = "Never Sleep: متوقف"
	}
	if st.Battery.Have {
		s += " · " + BatteryLine(st.Battery)
	}
	return s, on
}

func trayRefresh() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if !trayReady || eng == nil {
		return
	}
	st := eng.Snapshot()
	tip, on := tipFor(st)
	if tip == lastTip && on == lastIconOn && st.HoverInfo == lastHover {
		return
	}
	lastTip, lastIconOn, lastHover = tip, on, st.HoverInfo
	notify(nimModify, on, tip, !st.HoverInfo)
}

func trayRemove() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayReady {
		notify(nimDelete, false, "", false)
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
		if st.TimerMode == "duration" && st.DurationMin == d.m {
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
	if st.SmartSleep {
		f |= mfChecked
	}
	pAppendMenu.Call(menu, f, idSmart, uintptr(unsafe.Pointer(u16("وضع النوم الذكي"))))
	f = uintptr(mfString)
	if st.AutoStart {
		f |= mfChecked
	}
	pAppendMenu.Call(menu, f, idAuto, uintptr(unsafe.Pointer(u16("تشغيل مع ويندوز"))))
	pAppendMenu.Call(menu, mfString, idStats, uintptr(unsafe.Pointer(u16("استهلاك الرام والمعالج"))))
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
	case cmd == idSmart:
		eng.SetFlag("smartSleep", !st.SmartSleep)
	case cmd == idAuto:
		eng.SetFlag("autoStart", !st.AutoStart)
	case cmd == idStats:
		go eng.ShowStats()
	case cmd == idQuit:
		eng.Quit()
	case cmd >= idDurBase && cmd < idDurBase+4:
		eng.SetDuration(durs[cmd-idDurBase].m)
	}
}

var lastTipRender time.Time

func trayProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmTray:
		switch uint32(lp) {
		case wmLButtonUp:
			// wait a moment: a double click must open the window, not toggle twice
			if time.Since(lastDbl) < 350*time.Millisecond {
				return 0
			}
			pendingClick = true
			pSetTimer.Call(hwnd, idClickTimer, 220, 0)
			return 0
		case wmLButtonDblClk:
			lastDbl = time.Now()
			pendingClick = false
			pKillTimer.Call(hwnd, idClickTimer)
			eng.ShowUI()
			return 0
		case wmLButtonDown:
			return 0
		case wmRButtonUp:
			tipShow(false)
			showMenu(windows.Handle(hwnd))
			return 0
		case wmMouseMove:
			var tme trackMouseEvent
			tme.cbSize = uint32(unsafe.Sizeof(tme))
			tme.dwFlags = tmeLeave
			tme.hwndTrack = windows.Handle(hwnd)
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			if !tipVisible || time.Since(lastTipRender) > 500*time.Millisecond {
				lastTipRender = time.Now()
				tipShow(true)
			}
			return 0
		case wmMouseLeave:
			tipShow(false)
			return 0
		}
		return 0
	case wmTimerMsg:
		if wp == idClickTimer {
			pKillTimer.Call(hwnd, idClickTimer)
			if pendingClick {
				pendingClick = false
				eng.Toggle()
			}
		}
		return 0
	case wmShowMain:
		eng.ShowUI()
		return 0
	case wmDeviceChange:
		invalidateDevices()
		return 0
	}
	if taskbarMsg != 0 && uint32(m) == taskbarMsg { // Explorer restarted: re-add the icon
		trayMu.Lock()
		notify(nimAdd, lastIconOn, lastTip, !lastHover)
		trayMu.Unlock()
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

// runTray owns a hidden window and the message loop on a dedicated OS thread.
func runTray() {
	runtime.LockOSThread()
	iconAwake = loadPNGIcon(trayAwakePNG, 32)
	iconSleep = loadPNGIcon(traySleepPNG, 32)
	hi, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	hinst := windows.Handle(hi)
	tipCreate(hinst)
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
	st := eng.Snapshot()
	tip, on := tipFor(st)
	trayMu.Lock()
	lastTip, lastIconOn, lastHover, trayReady = tip, on, st.HoverInfo, true
	notify(nimAdd, on, tip, !st.HoverInfo)
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
