//go:build windows

package main

import (
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

func hasArg(a string) bool {
	for _, x := range os.Args[1:] {
		if x == a {
			return true
		}
	}
	return false
}

func main() {
	if len(os.Args) >= 4 && os.Args[1] == "--ui" {
		runUI(os.Args[2], os.Args[3])
		return
	}
	if hasArg("--selftest") {
		// used by the updater before it trusts a downloaded build: settings load + power request round trip.
		// The battery and device readers run too, so a build whose new system calls crash would be
		// rejected here instead of after it replaced the working copy.
		_ = LoadSettings(settingsPath())
		if !powerSelfTest() {
			os.Exit(2)
		}
		_ = readBattery()
		_ = devicesCached()
		println2("NeverSleep " + Version + " ok")
		return
	}
	if hasArg("--uninstall") {
		_ = killOthers().Run()
		uninstall()
		return
	}
	// Launched from anywhere other than the install folder (the downloaded Setup file): install, then start.
	if !runningInstalled() && !hasArg("--portable") {
		if err := install(); err != nil {
			if err == errNotConfirmed {
				return
			}
			msgBox("Never Sleep", "التثبيت ماكملش: "+err.Error())
			return
		}
		_ = exec.Command(installedExe()).Start() // opens the window once after install
		return
	}
	if runningInstalled() && trialGuard() {
		return
	}
	if applyAtLaunch() {
		return
	}
	name, _ := windows.UTF16PtrFromString(`Local\NeverSleepRozaSingle`)
	// An update restart is launched by the old process just before it quits, so with --relaunch
	// wait (up to ~15 s) for the old instance to release the single-instance lock instead of
	// mistaking it for a second copy and exiting.
	for tries := 0; ; tries++ {
		h, err := windows.CreateMutex(nil, false, name)
		if err == nil {
			break
		}
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		if hasArg("--relaunch") && tries < 50 {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		// already running: just bring up its window (unless we were started silently at login)
		if !hasArg("--background") {
			signalRunningInstance()
		}
		return
	}
	eng = newEngine()
	if err := eng.serve(); err != nil {
		msgBox("Never Sleep", "مش قادر أبدأ: "+err.Error())
		return
	}
	// on a fresh start, restore the autostart entry in the new format if the user had it on
	if runningInstalled() {
		setRunKey(eng.s.AutoStart)
		go updaterLoop()
		go trialPassed()
	}
	if !hasArg("--background") {
		eng.ShowUI()
	}
	runTray() // blocks; the whole app lives in the tray, no window unless asked
}

func println2(s string) { os.Stdout.WriteString(s + "\n") }
