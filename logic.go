package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const Version = "2.0.0"
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
}

const MaxDurationMin = 6000

func DefaultSettings() Settings {
	return Settings{ShowElapsed: true, AutoUpdate: true, Theme: "amoled", Accent: "#00e5c8"}
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
	getB("keepScreenOn", &s.KeepScreenOn)
	getB("acOnly", &s.AcOnly)
	getB("autoStart", &s.AutoStart)
	getB("showElapsed", &s.ShowElapsed)
	getB("autoUpdate", &s.AutoUpdate)
	if v, ok := raw["theme"]; ok {
		var x string
		if json.Unmarshal(v, &x) == nil && ValidTheme(x) {
			s.Theme = x
		}
	}
	if v, ok := raw["accent"]; ok {
		var x string
		if json.Unmarshal(v, &x) == nil && ValidAccent(x) {
			s.Accent = strings.ToLower(x)
		}
	}
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
