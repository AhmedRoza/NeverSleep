//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var errNotConfirmed = errors.New("cancelled")

const exeName = "Never Sleep.exe"
const uninstKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\NeverSleep`
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func installDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Never Sleep")
}

func installedExe() string { return filepath.Join(installDir(), exeName) }

func runningInstalled() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(self), filepath.Clean(installedExe()))
}

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	hide(c)
	return c
}

// setRunKey writes the single start-with-Windows entry. It opens in the background, with no window.
func setRunKey(on bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	// Remove any older entry (v1.x Electron wrote its own) that points at our exe without --background.
	if names, err := k.ReadValueNames(-1); err == nil {
		for _, n := range names {
			if v, _, err := k.GetStringValue(n); err == nil && strings.Contains(strings.ToLower(v), "never sleep.exe") {
				_ = k.DeleteValue(n)
			}
		}
	}
	if on {
		_ = k.SetStringValue("NeverSleep", `"`+installedExe()+`" --background`)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func isOldElectronInstall(dir string) bool {
	return fileExists(filepath.Join(dir, "resources")) || fileExists(filepath.Join(dir, "chrome_100_percent.pak"))
}

func askYesNo(title, text string) bool {
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), 0x4|0x20|0x00100000) // YESNO + question + RTL
	return r == 6
}

func regDump(root registry.Key, path string) map[string]string {
	out := map[string]string{}
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer k.Close()
	names, _ := k.ReadValueNames(-1)
	for _, n := range names {
		if v, _, err := k.GetStringValue(n); err == nil {
			out[n] = v
		}
	}
	return out
}

// install copies this exe into the per-user program folder. A v1.x Electron install is never
// deleted: with the user's consent it is moved to a backup folder, and moved back if anything fails.
// Settings live in %APPDATA%\neversleep and are never touched (the v1.x uninstaller did not touch them either).
func install() error {
	self, _ := os.Executable()
	dir := installDir()
	var backup string
	if isOldElectronInstall(dir) {
		if !askYesNo("Never Sleep 2.0", "لقيت نسخة Never Sleep القديمة (1.x) متثبتة.\n\nهنقفلها ونحفظها في فولدر Backup جنب مكانها، وإعداداتك مش هتتمس. تقدر ترجع لها لو حصلت مشكلة.\n\nنكمّل التثبيت؟") {
			return errNotConfirmed
		}
		_ = killOthers().Run()
		time.Sleep(800 * time.Millisecond)
		backup = dir + " v1 backup"
		if fileExists(backup) {
			backup = backup + " " + time.Now().Format("20060102-150405")
		}
		if err := os.Rename(dir, backup); err != nil {
			return fmt.Errorf("مقدرتش أنقل النسخة القديمة (ممكن لسه شغالة): %v", err)
		}
		// copy settings next to themselves as an extra safety net
		sp := settingsPath()
		if fileExists(sp) && !fileExists(sp+".v1-backup") {
			_ = copyFile(sp, sp+".v1-backup")
		}
		// remember the old registry values so the old setup can be restored by hand
		dump := map[string]map[string]string{
			"uninstall": regDump(registry.CURRENT_USER, uninstKey),
			"run":       regDump(registry.CURRENT_USER, runKey),
		}
		if b, err := json.MarshalIndent(dump, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(backup, "registry-backup.json"), b, 0o644)
		}
	} else {
		_ = killOthers().Run()
	}
	fail := func(err error) error {
		_ = os.RemoveAll(dir)
		if backup != "" {
			_ = os.Rename(backup, dir)
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}
	if err := copyFile(self, installedExe()); err != nil {
		return fail(err)
	}
	// shortcuts via PowerShell/WScript.Shell (no extra dependency in the exe)
	ps := `$s=New-Object -ComObject WScript.Shell;` +
		`$t='` + installedExe() + `';$w='` + dir + `';` +
		`$d=[Environment]::GetFolderPath('Desktop');$p=Join-Path ([Environment]::GetFolderPath('Programs')) 'Never Sleep';New-Item -ItemType Directory -Force $p|Out-Null;` +
		`foreach($f in @((Join-Path $d 'Never Sleep.lnk'),(Join-Path $p 'Never Sleep.lnk'))){$l=$s.CreateShortcut($f);$l.TargetPath=$t;$l.WorkingDirectory=$w;$l.IconLocation=$t;$l.Save()}`
	_ = hidden("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps).Run()
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstKey, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DisplayName", "Never Sleep")
		_ = k.SetStringValue("DisplayIcon", installedExe())
		_ = k.SetStringValue("UninstallString", `"`+installedExe()+`" --uninstall`)
		_ = k.SetStringValue("DisplayVersion", Version)
		_ = k.SetStringValue("Publisher", "Ahmed Roza")
		_ = k.SetDWordValue("NoModify", 1)
		k.Close()
	}
	// startup entry last, so a failed install never changes it. Always the --background form.
	s := LoadSettings(settingsPath())
	setRunKey(s.AutoStart)
	if backup != "" {
		msgBox("Never Sleep", "تم التثبيت. النسخة القديمة اتحفظت هنا:\n"+backup+"\nتقدر تمسح الفولدر ده بإيدك لما تتأكد إن كله تمام.")
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// uninstall removes shortcuts, startup entry and the program folder. Settings stay unless asked.
func uninstall() {
	setRunKey(false)
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstKey)
	ps := `$d=[Environment]::GetFolderPath('Desktop');Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $d 'Never Sleep.lnk');` +
		`Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path ([Environment]::GetFolderPath('Programs')) 'Never Sleep')`
	_ = hidden("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps).Run()
	// delete the folder after this process exits
	_ = hidden("cmd", "/C", "ping -n 3 127.0.0.1 >nul & rmdir /S /Q \""+installDir()+"\"").Start()
}

// killOthers stops other running copies without ever matching this process.
func killOthers() *exec.Cmd {
	return hidden("taskkill", "/F", "/IM", exeName, "/FI", fmt.Sprintf("PID ne %d", os.Getpid()))
}
