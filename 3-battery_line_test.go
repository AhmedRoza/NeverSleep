package main

import "testing"

func TestBatteryLine(t *testing.T) {
	cases := []struct {
		name string
		in   BatteryInfo
		want string
	}{
		{"no battery", BatteryInfo{Percent: -1, LeftSecs: -1, FullSecs: -1}, "مفيش بطارية"},
		{"charging with estimate", BatteryInfo{Have: true, Percent: 64, Charging: true, OnAC: true, LeftSecs: -1, FullSecs: 5400}, "64% · بيشحن، فاضل 01:30"},
		{"charging unknown time", BatteryInfo{Have: true, Percent: 64, Charging: true, OnAC: true, LeftSecs: -1, FullSecs: -1}, "64% · بيشحن"},
		{"plugged not charging", BatteryInfo{Have: true, Percent: 100, OnAC: true, LeftSecs: -1, FullSecs: -1}, "100% · على الشاحن"},
		{"on battery with time left", BatteryInfo{Have: true, Percent: 42, OnAC: false, LeftSecs: 7200, FullSecs: -1}, "42% · فاضل 02:00"},
		{"on battery unknown time", BatteryInfo{Have: true, Percent: 42, OnAC: false, LeftSecs: -1, FullSecs: -1}, "42% · على البطارية"},
		{"percent unknown", BatteryInfo{Have: true, Percent: -1, OnAC: false, LeftSecs: -1, FullSecs: -1}, "— · على البطارية"},
	}
	for _, c := range cases {
		if got := BatteryLine(c.in); got != c.want {
			t.Fatalf("%s: %q want %q", c.name, got, c.want)
		}
	}
}

func TestHumanSpanShortEdges(t *testing.T) {
	if got := HumanSpanShort(59 * 1e9); got != "00:00" {
		t.Fatalf("59s -> %q", got)
	}
	if got := HumanSpanShort(90 * 60 * 1e9); got != "01:30" {
		t.Fatalf("90m -> %q", got)
	}
	if got := HumanSpanShort(50 * 3600 * 1e9); got != "2 يوم · 02:00" {
		t.Fatalf("50h -> %q", got)
	}
}
