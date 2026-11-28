package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 11, 28, h, m, 0, 0, time.Local)
}

func TestParseClock(t *testing.T) {
	good := map[string]int{"00:00": 0, "05:00": 300, "23:59": 1439, "07:30": 450}
	for s, want := range good {
		got, ok := ParseClock(s)
		if !ok || got != want {
			t.Fatalf("%q -> %d,%v want %d", s, got, ok, want)
		}
	}
	bad := []string{"", "5:00", "24:00", "23:60", "05-00", "aa:bb", "005:0", "-1:00"}
	for _, s := range bad {
		if _, ok := ParseClock(s); ok {
			t.Fatalf("%q should be invalid", s)
		}
	}
	if FmtClock(300) != "05:00" || FmtClock(1439) != "23:59" || FmtClock(1440) != "00:00" {
		t.Fatalf("FmtClock wrong")
	}
}

func TestNextClock(t *testing.T) {
	now := at(22, 10)
	got, ok := NextClock(now, "05:00")
	if !ok || !got.Equal(at(5, 0).AddDate(0, 0, 1)) {
		t.Fatalf("22:10 -> 05:00 gave %v", got)
	}
	now = at(3, 0)
	got, _ = NextClock(now, "05:00")
	if !got.Equal(at(5, 0)) {
		t.Fatalf("03:00 -> 05:00 gave %v", got)
	}
	now = at(5, 0)
	got, _ = NextClock(now, "05:00")
	if !got.Equal(at(5, 0).AddDate(0, 0, 1)) {
		t.Fatalf("exact 05:00 should roll to tomorrow, got %v", got)
	}
	if _, ok := NextClock(now, "bad"); ok {
		t.Fatalf("bad clock accepted")
	}
}

func TestInWindow(t *testing.T) {
	cases := []struct {
		h, m     int
		from, to string
		want     bool
	}{
		{23, 30, "23:00", "07:00", true},
		{0, 0, "23:00", "07:00", true},
		{6, 59, "23:00", "07:00", true},
		{7, 0, "23:00", "07:00", false},
		{22, 59, "23:00", "07:00", false},
		{12, 0, "23:00", "07:00", false},
		{13, 0, "13:00", "18:00", true},
		{18, 0, "13:00", "18:00", false},
		{12, 59, "13:00", "18:00", false},
		{1, 0, "07:00", "07:00", false},
	}
	for _, c := range cases {
		if got := InWindow(at(c.h, c.m), c.from, c.to); got != c.want {
			t.Fatalf("%02d:%02d in %s-%s = %v want %v", c.h, c.m, c.from, c.to, got, c.want)
		}
	}
}

func TestWindowEnd(t *testing.T) {
	end, ok := WindowEnd(at(23, 30), "23:00", "07:00")
	if !ok || !end.Equal(at(7, 0).AddDate(0, 0, 1)) {
		t.Fatalf("23:30 end = %v", end)
	}
	end, ok = WindowEnd(at(2, 0), "23:00", "07:00")
	if !ok || !end.Equal(at(7, 0)) {
		t.Fatalf("02:00 end = %v", end)
	}
}

func TestHumanSpan(t *testing.T) {
	if got := HumanSpan(0); got != "00:00:00" {
		t.Fatalf("zero %q", got)
	}
	if got := HumanSpan(7*time.Minute + 3*time.Second); got != "00:07:03" {
		t.Fatalf("7m %q", got)
	}
	if got := HumanSpan(27*time.Hour + 4*time.Minute + 9*time.Second); got != "1 يوم · 03:04:09" {
		t.Fatalf("27h %q", got)
	}
	if got := HumanSpanShort(2*time.Hour + 5*time.Minute + 40*time.Second); got != "02:05" {
		t.Fatalf("short %q", got)
	}
	if got := HumanSpan(-5 * time.Second); got != "00:00:00" {
		t.Fatalf("negative %q", got)
	}
}

func TestClock12(t *testing.T) {
	cases := []struct {
		h, m int
		want string
	}{
		{0, 0, "12:00 ص"}, {1, 5, "1:05 ص"}, {11, 59, "11:59 ص"},
		{12, 0, "12:00 م"}, {15, 0, "3:00 م"}, {23, 30, "11:30 م"},
	}
	for _, c := range cases {
		if got := Clock12(time.Date(2026, 11, 28, c.h, c.m, 0, 0, time.Local)); got != c.want {
			t.Fatalf("%02d:%02d -> %q want %q", c.h, c.m, got, c.want)
		}
	}
}

func TestWanted(t *testing.T) {
	s := DefaultSettings()
	if sys, disp := Wanted(false, s, false); sys || disp {
		t.Fatal("off must hold nothing")
	}
	s.KeepScreenOn = true
	if sys, disp := Wanted(true, s, false); !sys || !disp {
		t.Fatal("on + screen must hold both")
	}
	s.AcOnly = true
	if sys, disp := Wanted(true, s, true); sys || disp {
		t.Fatal("acOnly on battery must hold nothing")
	}
	if sys, _ := Wanted(true, s, false); !sys {
		t.Fatal("acOnly on charger must hold system")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		tag, cur string
		want     bool
	}{
		{"v2.1.0", "2.0.0", true},
		{"2.1.0", "2.1.0", false},
		{"v2.0.9", "2.1.0", false},
		{"v2.1.10", "2.1.9", true},
		{"v3.0.0", "2.99.99", true},
		{"bad", "2.0.0", false},
		{"v2.1", "2.0.0", false},
		{"v2.1.0-rc1", "2.0.0", false},
	}
	for _, c := range cases {
		if got := NewerVersion(c.tag, c.cur); got != c.want {
			t.Fatalf("%s vs %s = %v", c.tag, c.cur, got)
		}
	}
}

func TestSettingsRoundTripAndMigration(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	old := `{"keepScreenOn":true,"durationMinutes":120,"acOnly":true,"autoStart":true,"showElapsed":false,"autoUpdate":false,"theme":"light","accent":"#FF6FA5"}`
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(p)
	if !s.KeepScreenOn || s.DurationMinutes != 120 || !s.AcOnly || !s.AutoStart || s.ShowElapsed || s.AutoUpdate {
		t.Fatalf("old keys lost: %+v", s)
	}
	if s.Theme != "light" || s.Accent != "#ff6fa5" {
		t.Fatalf("theme/accent: %+v", s)
	}
	if s.TimerMode != "duration" || s.UntilTime != "05:00" || s.SmartFrom != "23:00" || s.SmartTo != "07:00" || !s.HoverInfo || s.SmartSleep {
		t.Fatalf("new defaults wrong: %+v", s)
	}
	s.UntilTime = "04:30"
	s.SmartSleep = true
	s.TimerMode = "until"
	if err := SaveSettings(p, s); err != nil {
		t.Fatal(err)
	}
	r := LoadSettings(p)
	if r.UntilTime != "04:30" || !r.SmartSleep || r.TimerMode != "until" {
		t.Fatalf("round trip: %+v", r)
	}
}

func TestSettingsRejectsJunk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	junk := `{"durationMinutes":-4,"theme":"neon","accent":"red","timerMode":"forever","untilTime":"99:99","smartFrom":"x","smartTo":"07:00","hoverInfo":false}`
	os.WriteFile(p, []byte(junk), 0o644)
	s := LoadSettings(p)
	if s.DurationMinutes != 0 || s.Theme != "amoled" || s.Accent != "#00e5c8" || s.TimerMode != "duration" || s.UntilTime != "05:00" || s.SmartFrom != "23:00" {
		t.Fatalf("junk accepted: %+v", s)
	}
	if s.HoverInfo {
		t.Fatal("hoverInfo false should be kept")
	}
	os.WriteFile(p, []byte("{not json"), 0o644)
	if got := LoadSettings(p); got != DefaultSettings() {
		t.Fatalf("broken file: %+v", got)
	}
	if got := LoadSettings(filepath.Join(dir, "missing.json")); got != DefaultSettings() {
		t.Fatalf("missing file: %+v", got)
	}
}
