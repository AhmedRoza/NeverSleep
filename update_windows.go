//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func updateDir() string { return filepath.Join(installDir(), "update") }

var httpc = &http.Client{Timeout: 90 * time.Second}

func httpGet(url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "NeverSleep/"+Version)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("too large")
	}
	return b, err
}

func updaterLoop() {
	time.Sleep(45 * time.Second)
	for {
		eng.mu.Lock()
		on := eng.s.AutoUpdate
		eng.mu.Unlock()
		if on {
			checkForUpdate()
		}
		time.Sleep(6 * time.Hour)
	}
}

func checkForUpdate() {
	if !runningInstalled() {
		return
	}
	body, err := httpGet("https://api.github.com/repos/"+Repo+"/releases/latest", 1<<20)
	if err != nil {
		return // offline or rate limited: stay quiet, retry later
	}
	tag, exe, err := PlanUpdate(body, Version, httpGet)
	if err != nil {
		return
	}
	_ = os.MkdirAll(updateDir(), 0o755)
	tmp := filepath.Join(updateDir(), "new.exe.part")
	final := filepath.Join(updateDir(), "new.exe")
	if os.WriteFile(tmp, exe, 0o755) != nil {
		return
	}
	// The candidate must run and report exactly the version it claims before it can replace us.
	if !selftestOK(tmp, tag) {
		_ = os.Remove(tmp)
		return
	}
	_ = os.WriteFile(filepath.Join(updateDir(), "new.ver"), []byte(tag), 0o644)
	if os.Rename(tmp, final) != nil {
		return
	}
	eng.mu.Lock()
	eng.pendingUpd = final
	eng.updateMsg = "تحديث " + tag + " جاهز، هيتركّب أول ما منع السكون يبقى متوقف"
	eng.mu.Unlock()
	eng.poke()
}

// selftestOK runs "<exe> --selftest" with a timeout and expects "NeverSleep <tag-without-v> ok".
func selftestOK(path, tag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--selftest")
	hide(cmd)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "NeverSleep "+strings.TrimPrefix(tag, "v")+" ok"
}

// maybeApplyUpdate swaps in a verified update only while keep-awake is off.
func maybeApplyUpdate() {
	eng.mu.Lock()
	p, active := eng.pendingUpd, eng.active
	ui := eng.uiCmd != nil
	eng.mu.Unlock()
	if p == "" || active {
		return
	}
	if err := swapExecutable(p); err != nil {
		eng.mu.Lock()
		eng.pendingUpd = ""
		eng.updateMsg = ""
		eng.mu.Unlock()
		return
	}
	self, _ := os.Executable()
	args := []string{"--relaunch", "--background"}
	if ui {
		args = []string{"--relaunch"}
	}
	_ = exec.Command(self, args...).Start()
	eng.Quit()
}

// swapExecutable renames the running exe aside (allowed on Windows) and moves the new one in.
// The old file stays as "<exe>.old" so a bad update can be rolled back.
func swapExecutable(newPath string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	old := self + ".old"
	_ = os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, self); err != nil {
		_ = os.Rename(old, self)
		return err
	}
	// trial counter: the new build must reach a healthy run before this is cleared
	_ = os.WriteFile(filepath.Join(updateDir(), "trial"), []byte("0"), 0o644)
	return nil
}

// applyAtLaunch installs an update downloaded in an earlier session before anything else starts.
func applyAtLaunch() bool {
	p := filepath.Join(updateDir(), "new.exe")
	if _, err := os.Stat(p); err != nil || !runningInstalled() {
		return false
	}
	v, err := os.ReadFile(filepath.Join(updateDir(), "new.ver"))
	if err != nil || !NewerVersion(string(v), Version) || !selftestOK(p, strings.TrimSpace(string(v))) {
		_ = os.Remove(p) // stale, downgrade or not runnable: discard
		return false
	}
	if swapExecutable(p) != nil {
		return false
	}
	self, _ := os.Executable()
	_ = exec.Command(self, append([]string{"--relaunch"}, os.Args[1:]...)...).Start()
	return true
}

// trialGuard rolls back a new build that keeps dying at startup. Returns true if it restored the old build.
func trialGuard() bool {
	self, _ := os.Executable()
	tf := filepath.Join(updateDir(), "trial")
	b, err := os.ReadFile(tf)
	if err != nil {
		return false
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if n >= 2 && fileExists(self+".old") {
		bad := self + ".bad"
		_ = os.Remove(bad)
		if os.Rename(self, bad) == nil && os.Rename(self+".old", self) == nil {
			_ = os.Remove(tf)
			_ = os.Remove(filepath.Join(updateDir(), "new.ver"))
			_ = exec.Command(self, os.Args[1:]...).Start()
			return true
		}
	}
	_ = os.WriteFile(tf, []byte(strconv.Itoa(n+1)), 0o644)
	return false
}

// trialPassed is called once the new build has been healthy for a while.
func trialPassed() {
	time.Sleep(15 * time.Second)
	_ = os.Remove(filepath.Join(updateDir(), "trial"))
}
