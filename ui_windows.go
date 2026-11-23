//go:build windows

package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed ui.html
var uiHTML string

//go:embed assets/photo.jpg
var photoJPG []byte

//go:embed assets/icon.png
var iconPNG []byte

var contactURLs = map[string]string{
	"whatsapp":  "https://wa.me/201095064150",
	"instagram": "https://www.instagram.com/ahmed_roza_",
}

// runUI is the separate window process. When it exits, the whole WebView2 stack goes with it.
func runUI(port, token string) {
	runtime.LockOSThread()
	base := "http://127.0.0.1:" + port
	cli := &http.Client{Timeout: 4 * time.Second}
	call := func(path string) (string, error) {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("X-NS", token)
		resp, err := cli.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return string(b), nil
	}
	if !webview2Installed() {
		msgBox("Never Sleep", "مش لاقي مكوّن WebView2 على الجهاز. نزّله من موقع مايكروسوفت (Microsoft Edge WebView2 Runtime) وجرّب تاني.\n\nWebView2 Runtime was not found. Install it from Microsoft and try again.")
		return
	}
	data := filepath.Join(os.Getenv("LOCALAPPDATA"), "NeverSleep", "webview")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:      data,
		WindowOptions: webview2.WindowOptions{Title: "Never Sleep", IconId: 1, Width: 430, Height: 790, Center: true},
	})
	if w == nil {
		msgBox("Never Sleep", "مش لاقي مكوّن WebView2 على الجهاز. نزّله من موقع مايكروسوفت (Microsoft Edge WebView2 Runtime) وجرّب تاني.")
		return
	}
	defer w.Destroy()
	setWindowIcon(uintptr(w.Window()))
	w.SetSize(430, 790, webview2.HintNone)
	w.Bind("nsState", func() (map[string]interface{}, error) {
		s, err := call("/state")
		if err != nil {
			return nil, err
		}
		var m map[string]interface{}
		err = json.Unmarshal([]byte(s), &m)
		return m, err
	})
	w.Bind("nsToggle", func() { call("/toggle") })
	w.Bind("nsDuration", func(m int) { call(fmt.Sprintf("/duration?m=%d", m)) })
	w.Bind("nsSet", func(k string, v bool) {
		n := "0"
		if v {
			n = "1"
		}
		call("/flag?k=" + k + "&v=" + n)
	})
	w.Bind("nsPref", func(k, v string) {
		call("/pref?k=" + url.QueryEscape(k) + "&v=" + url.QueryEscape(v))
	})
	w.Bind("nsHide", func() { w.Dispatch(func() { w.Destroy() }) })
	w.Bind("nsQuit", func() { call("/quit"); w.Dispatch(func() { w.Destroy() }) })
	w.Bind("nsOpen", func(k string) {
		if u, ok := contactURLs[k]; ok {
			exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
		}
	})
	html := strings.Replace(uiHTML, "{{PHOTO}}", "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(photoJPG), 1)
	w.SetHtml(html)
	w.Run()
}

// webview2Installed checks the documented Evergreen runtime registration before loading it,
// so a missing runtime gives a clear message instead of a crash.
func webview2Installed() bool {
	const guid = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	paths := []struct {
		root windows.Handle
		key  string
	}{
		{windows.Handle(registry.LOCAL_MACHINE), `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + guid},
		{windows.Handle(registry.LOCAL_MACHINE), `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + guid},
		{windows.Handle(registry.CURRENT_USER), `Software\Microsoft\EdgeUpdate\Clients\` + guid},
	}
	for _, p := range paths {
		k, err := registry.OpenKey(registry.Key(p.root), p.key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("pv")
		k.Close()
		if err == nil && v != "" && v != "0.0.0.0" {
			return true
		}
	}
	return false
}

var pSendMessage = user32.NewProc("SendMessageW")
var pMessageBox = user32.NewProc("MessageBoxW")

func setWindowIcon(hwnd uintptr) {
	big := loadPNGIcon(iconPNG, 48)
	small := loadPNGIcon(iconPNG, 16)
	pSendMessage.Call(hwnd, 0x80, 1, uintptr(big))   // WM_SETICON ICON_BIG
	pSendMessage.Call(hwnd, 0x80, 0, uintptr(small)) // ICON_SMALL
}

func msgBox(title, text string) {
	pMessageBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), 0x40|0x00100000) // info + RTL reading
}

var _ = syscall.Errno(0)
var _ = windows.Handle(0)
