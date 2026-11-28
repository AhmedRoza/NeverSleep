package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Version = "2.1.1"
const Repo = "AhmedRoza/NeverSleep"

// Settings keep the same JSON keys as the v1.x Electron app so existing choices carry over.
type Settings struct {
	KeepScreenOn    bool   `json:"keepScreenOn"`
	DurationMinutes int    `json:"durationMinutes"`
	AcOnly          bool   `json:"acOnly"`
	AutoStart       bool   `json:"autoStart"`
	ShowElapsed     bool   `json:"showElapsed"`
	AutoUpdate      bool   `json:"autoUpdate"`
	Theme           string `json:"theme"`  // "amoled" or "light"
	Accent          string `json:"accent"` // "#rrggbb"

	TimerMode  string `json:"timerMode"` // "duration" or "until"
	UntilTime  string `json:"untilTime"` // "HH:MM", 24-hour clock
	SmartSleep bool   `json:"smartSleep"`
	SmartFrom  string `json:"smartFrom"` // "HH:MM"
	SmartTo    string `json:"smartTo"`   // "HH:MM"
	HoverInfo  bool   `json:"hoverInfo"`
}

const MaxDurationMin = 6000

func DefaultSettings() Settings {
	return Settings{
		ShowElapsed: true, AutoUpdate: true, Theme: "amoled", Accent: "#00e5c8",
		TimerMode: "duration", UntilTime: "05:00",
		SmartSleep: false, SmartFrom: "23:00", SmartTo: "07:00",
		HoverInfo: true,
	}
}

func ValidDuration(m int) bool { return m >= 0 && m <= MaxDurationMin }

// LoadSettings starts from defaults and applies only valid values from the file.
func LoadSettings(file string) Settings {
	s := DefaultSettings()
	b, err := os.ReadFile(file)
	if err != nil {
		return s
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return s
	}
	getB := func(k string, dst *bool) {
		if v, ok := raw[k]; ok {
			var x bool
			if json.Unmarshal(v, &x) == nil {
				*dst = x
			}
		}
	}
	getS := func(k string, valid func(string) bool, dst *string) {
		if v, ok := raw[k]; ok {
			var x string
			if json.Unmarshal(v, &x) == nil && valid(x) {
				*dst = x
			}
		}
	}
	getB("keepScreenOn", &s.KeepScreenOn)
	getB("acOnly", &s.AcOnly)
	getB("autoStart", &s.AutoStart)
	getB("showElapsed", &s.ShowElapsed)
	getB("autoUpdate", &s.AutoUpdate)
	getB("smartSleep", &s.SmartSleep)
	getB("hoverInfo", &s.HoverInfo)
	getS("theme", ValidTheme, &s.Theme)
	getS("accent", ValidAccent, &s.Accent)
	getS("timerMode", ValidTimerMode, &s.TimerMode)
	getS("untilTime", ValidClock, &s.UntilTime)
	getS("smartFrom", ValidClock, &s.SmartFrom)
	getS("smartTo", ValidClock, &s.SmartTo)
	s.Accent = strings.ToLower(s.Accent)
	if v, ok := raw["durationMinutes"]; ok {
		var f float64
		if json.Unmarshal(v, &f) == nil && f == float64(int(f)) && ValidDuration(int(f)) {
			s.DurationMinutes = int(f)
		}
	}
	return s
}

func SaveSettings(file string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Wanted says which power requests should be held.
func Wanted(active bool, s Settings, onBattery bool) (system, display bool) {
	eff := active && !(s.AcOnly && onBattery)
	return eff, eff && s.KeepScreenOn
}

// NewerVersion reports whether tag (like "v2.1.0") is strictly newer than cur.
func NewerVersion(tag, cur string) bool {
	a, b := parseVer(tag), parseVer(cur)
	if a == nil || b == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseVer(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}

// FirstHashToken extracts the hex SHA-256 from a "<hash>  filename" style file.
func FirstHashToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	h := strings.ToLower(f[0])
	if len(h) != 64 {
		return ""
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return h
}

func ValidTheme(t string) bool { return t == "amoled" || t == "light" }

func ValidTimerMode(t string) bool { return t == "duration" || t == "until" }

// ValidAccent accepts only "#rrggbb".
func ValidAccent(a string) bool {
	if len(a) != 7 || a[0] != '#' {
		return false
	}
	for _, c := range strings.ToLower(a[1:]) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ---- clock helpers ("HH:MM", 24-hour, local time) ----

// ParseClock turns "HH:MM" into minutes after midnight.
func ParseClock(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, e1 := strconv.Atoi(s[0:2])
	m, e2 := strconv.Atoi(s[3:5])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func ValidClock(s string) bool { _, ok := ParseClock(s); return ok }

// FmtClock renders minutes after midnight as "HH:MM".
func FmtClock(min int) string {
	min = ((min % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// NextClock returns the next moment the local clock reads `clock`, strictly after now.
// A time earlier today means tomorrow.
func NextClock(now time.Time, clock string) (time.Time, bool) {
	min, ok := ParseClock(clock)
	if !ok {
		return time.Time{}, false
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), min/60, min%60, 0, 0, now.Location())
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t, true
}

// InWindow reports whether now falls inside the daily [from, to) window.
// from later than to means the window crosses midnight (23:00-07:00). from == to is never on.
func InWindow(now time.Time, from, to string) bool {
	f, ok1 := ParseClock(from)
	t, ok2 := ParseClock(to)
	if !ok1 || !ok2 || f == t {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if f < t {
		return cur >= f && cur < t
	}
	return cur >= f || cur < t
}

// WindowEnd returns when the window that contains now ends.
func WindowEnd(now time.Time, from, to string) (time.Time, bool) {
	f, ok1 := ParseClock(from)
	t, ok2 := ParseClock(to)
	if !ok1 || !ok2 || f == t {
		return time.Time{}, false
	}
	end := time.Date(now.Year(), now.Month(), now.Day(), t/60, t%60, 0, 0, now.Location())
	if !end.After(now) {
		end = end.AddDate(0, 0, 1)
	}
	return end, true
}

// HumanSpan formats a duration for a counter: "3 يوم · 04:12" or "04:12" or "00:07".
func HumanSpan(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	days := total / 86400
	h := (total % 86400) / 3600
	m := (total % 3600) / 60
	s := total % 60
	if days > 0 {
		return fmt.Sprintf("%d يوم · %02d:%02d:%02d", days, h, m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// HumanSpanShort drops the seconds when there is at least an hour to show.
func HumanSpanShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	days := total / 86400
	h := (total % 86400) / 3600
	m := (total % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%d يوم · %02d:%02d", days, h, m)
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

// Clock12 renders a local time the way the user reads it: 12-hour with ص/م.
func Clock12(t time.Time) string {
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	sfx := "ص"
	if t.Hour() >= 12 {
		sfx = "م"
	}
	return fmt.Sprintf("%d:%02d %s", h, t.Minute(), sfx)
}
