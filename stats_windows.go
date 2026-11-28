//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Process numbers are only collected when someone asks for them: nothing here runs on a timer.
type procStats struct {
	Label    string
	PID      uint32
	WorkMB   float64
	PrivMB   float64
	CPUSecs  float64
	CPUPct   float64 // average over the process lifetime
	LifeSecs float64
}

type fileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

type processMemoryCountersEx struct {
	cb                      uint32
	PageFaultCount          uint32
	PeakWorkingSetSize      uintptr
	WorkingSetSize          uintptr
	QuotaPeakPagedPoolUsage uintptr
	QuotaPagedPoolUsage     uintptr
	QuotaPeakNonPagedPool   uintptr
	QuotaNonPagedPoolUsage  uintptr
	PagefileUsage           uintptr
	PeakPagefileUsage       uintptr
	PrivateUsage            uintptr
}

var (
	psapi                 = windows.NewLazySystemDLL("psapi.dll")
	pGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
	pGetProcessTimes      = kernel32.NewProc("GetProcessTimes")
	pOpenProcess          = kernel32.NewProc("OpenProcess")
	pCloseHandle          = kernel32.NewProc("CloseHandle")
)

func fileTimeToDuration(ft fileTime) time.Duration {
	v := uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
	return time.Duration(v * 100) // 100 ns units
}

func statsFor(h windows.Handle, label string, pid uint32) (procStats, bool) {
	st := procStats{Label: label, PID: pid}
	var mem processMemoryCountersEx
	mem.cb = uint32(unsafe.Sizeof(mem))
	if r, _, _ := pGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mem)), uintptr(mem.cb)); r == 0 {
		return st, false
	}
	st.WorkMB = float64(mem.WorkingSetSize) / (1024 * 1024)
	st.PrivMB = float64(mem.PrivateUsage) / (1024 * 1024)

	var created, exited, kern, user fileTime
	if r, _, _ := pGetProcessTimes.Call(uintptr(h), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)),
		uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user))); r == 0 {
		return st, true
	}
	st.CPUSecs = (fileTimeToDuration(kern) + fileTimeToDuration(user)).Seconds()
	// lifetime from the creation time: convert FILETIME (1601) to unix seconds
	createdUnix := float64(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime))/1e7 - 11644473600
	st.LifeSecs = float64(time.Now().Unix()) - createdUnix
	if st.LifeSecs > 0 {
		st.CPUPct = st.CPUSecs / st.LifeSecs * 100
	}
	return st, true
}

func init() {
	// PROCESS_MEMORY_COUNTERS_EX is 80 bytes on 64-bit Windows; fail the build if the layout drifts.
	var _ [80 - unsafe.Sizeof(processMemoryCountersEx{})]byte
	var _ [unsafe.Sizeof(processMemoryCountersEx{}) - 80]byte
}

// childPIDs lists every process below pid (WebView2 starts its own msedgewebview2.exe helpers,
// and that is where most of the window's memory lives).
func childPIDs(root uint32) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	parent := map[uint32]uint32{}
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		parent[pe.ProcessID] = pe.ParentProcessID
	}
	var out []uint32
	for pid := range parent {
		for p, depth := pid, 0; depth < 8; depth++ {
			pp, ok := parent[p]
			if !ok || pp == 0 {
				break
			}
			if pp == root {
				out = append(out, pid)
				break
			}
			p = pp
		}
	}
	return out
}

// collectStats reads the tray process and, when the window is open, the window process.
func collectStats() []procStats {
	out := []procStats{}
	if st, ok := statsFor(windows.CurrentProcess(), "عملية الترايه", uint32(os.Getpid())); ok {
		out = append(out, st)
	}
	if eng != nil {
		eng.mu.Lock()
		var pid uint32
		if eng.uiCmd != nil && eng.uiCmd.Process != nil {
			pid = uint32(eng.uiCmd.Process.Pid)
		}
		eng.mu.Unlock()
		if pid != 0 {
			const processQueryLimited = 0x1000
			h, _, _ := pOpenProcess.Call(processQueryLimited, 0, uintptr(pid))
			if h != 0 {
				if st, ok := statsFor(windows.Handle(h), "عملية النافذة", pid); ok {
					out = append(out, st)
				}
				pCloseHandle.Call(h)
			}
			var web procStats
			web.Label = "مكوّن WebView2 (تابع للنافذة)"
			n := 0
			for _, c := range childPIDs(pid) {
				ch, _, _ := pOpenProcess.Call(processQueryLimited, 0, uintptr(c))
				if ch == 0 {
					continue
				}
				if st, ok := statsFor(windows.Handle(ch), "", c); ok {
					web.WorkMB += st.WorkMB
					web.PrivMB += st.PrivMB
					web.CPUSecs += st.CPUSecs
					if st.LifeSecs > web.LifeSecs {
						web.LifeSecs = st.LifeSecs
					}
					n++
				}
				pCloseHandle.Call(ch)
			}
			if n > 0 {
				if web.LifeSecs > 0 {
					web.CPUPct = web.CPUSecs / web.LifeSecs * 100
				}
				web.PID = uint32(n) // shown as the process count
				web.Label = fmt.Sprintf("مكوّن WebView2 (%d عمليات تابعة للنافذة)", n)
				out = append(out, web)
			}
		}
	}
	return out
}

func statsText(list []procStats) string {
	var b strings.Builder
	b.WriteString("Never Sleep v" + Version + " — قياس الأداء\n\n")
	if len(list) == 0 {
		b.WriteString("مقدرتش أقرا الأرقام على الجهاز ده.\n")
		return b.String()
	}
	var totWork, totPriv, totCPU float64
	for _, s := range list {
		if strings.HasPrefix(s.Label, "مكوّن") {
			b.WriteString(s.Label + "\n")
		} else {
			b.WriteString(fmt.Sprintf("%s (PID %d)\n", s.Label, s.PID))
		}
		b.WriteString(fmt.Sprintf("  الرام: %.1f ميجا (خاص %.1f)\n", s.WorkMB, s.PrivMB))
		b.WriteString(fmt.Sprintf("  المعالج: %.2f ثانية من %.1f دقيقة = %.2f%% في المتوسط\n",
			s.CPUSecs, s.LifeSecs/60, s.CPUPct))
		b.WriteString("\n")
		totWork += s.WorkMB
		totPriv += s.PrivMB
		totCPU += s.CPUSecs
	}
	b.WriteString(fmt.Sprintf("المجموع: %.1f ميجا رام (خاص %.1f)، %.2f ثانية معالج\n", totWork, totPriv, totCPU))
	b.WriteString("\nالنسبة دي متوسط من أول ما البرنامج فتح، مش لحظة القياس. افتح النافذة وقفلها وقيس تاني لو عايز تشوف الفرق.")
	return b.String()
}

// ShowStats writes the numbers next to the settings file and shows them in a message box.
func (e *Engine) ShowStats() {
	txt := statsText(collectStats())
	p := filepath.Join(filepath.Dir(settingsPath()), "stats.txt")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(txt), 0o644)
	msgBox("Never Sleep — الأداء", txt+"\n\nاتحفظ كمان في:\n"+p)
}
