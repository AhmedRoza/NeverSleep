package main

import "testing"

func TestClassifyDevice(t *testing.T) {
	keep := []struct {
		r    rawDevice
		kind string
	}{
		{rawDevice{Name: "Headphones (Realtek(R) Audio)", Class: "AudioEndpoint", Instance: `SWD\MMDEVAPI\{0.0.1.00000000}.{abc}`, Battery: -1}, "audio"},
		{rawDevice{Name: "Speakers (Realtek(R) Audio)", Class: "AudioEndpoint", Instance: `SWD\MMDEVAPI\{0.0.0.00000000}.{abc}`, Battery: -1}, "audio"},
		{rawDevice{Name: "Logitech USB Mouse", Class: "Mouse", Instance: `USB\VID_046D&PID_C077\5&2A1`, Battery: -1}, "mouse"},
		{rawDevice{Name: "HID-compliant mouse", Class: "HIDClass", Instance: `USB\VID_046D&PID_C077&MI_00\6&1`, Battery: -1}, "mouse"},
		{rawDevice{Name: "USB Keyboard", Class: "Keyboard", Instance: `USB\VID_04D9&PID_1702\7&3`, Battery: -1}, "keyboard"},
		{rawDevice{Name: "SanDisk Ultra", Class: "USB", Instance: `USB\VID_0781&PID_5581\4C530`, Battery: -1}, "usb"},
		{rawDevice{Name: "Galaxy Buds", Class: "Bluetooth", Instance: `BTHENUM\{0000110b}\7&2`, Battery: -1}, "bt"},
		{rawDevice{Name: "Xbox Wireless Controller", Class: "HIDClass", Instance: `BTHENUM\{00001124}\7&1`, Battery: -1}, "bt"},
		{rawDevice{Name: "Galaxy S23", Class: "WPD", Instance: `USB\VID_04E8&PID_6860\R58`, Battery: -1}, "phone"},
		{rawDevice{Name: "USB Serial Device (COM4)", Class: "Ports", Instance: `USB\VID_1A86&PID_7523\5&1`, Battery: -1}, "usb"},
		{rawDevice{Name: "Logitech USB Receiver", Class: "HIDClass", Instance: `USB\VID_046D&PID_C52B&MI_01\6&2`, Battery: -1}, "usb"},
	}
	for _, c := range keep {
		d, ok := classifyDevice(c.r)
		if !ok {
			t.Fatalf("%q should be listed", c.r.Name)
		}
		if d.Kind != c.kind {
			t.Fatalf("%q kind %q want %q", c.r.Name, d.Kind, c.kind)
		}
	}
	drop := []rawDevice{
		{Name: "USB Root Hub (USB 3.0)", Class: "USB", Instance: `USB\ROOT_HUB30\4&1`, Battery: -1},
		{Name: "Generic USB Hub", Class: "USB", Instance: `USB\VID_8087&PID_0024\5&1`, Battery: -1},
		{Name: "USB Composite Device", Class: "USB", Instance: `USB\VID_046D&PID_C52B\5&2`, Battery: -1},
		{Name: "Microsoft Bluetooth Enumerator", Class: "Bluetooth", Instance: `BTH\MS_BTHBRB\6&1`, Battery: -1},
		{Name: "", Class: "Mouse", Instance: `USB\VID_1&PID_2\3`, Battery: -1},
		{Name: "HID-compliant touch pad", Class: "HIDClass", Instance: `HID\VID_06CB&PID_CE44\7&1`, Battery: -1},
		{Name: "ACPI Lid", Class: "System", Instance: `ACPI\PNP0C0D\2&DABA3FF&0`, Battery: -1},
		{Name: "Realtek PCIe GbE Family Controller", Class: "Net", Instance: `PCI\VEN_10EC&DEV_8168\4&1`, Battery: -1},
		{Name: "Intel(R) Core(TM) i7", Class: "Processor", Instance: `ACPI\GENUINEINTEL_-_INTEL64\1`, Battery: -1},
		{Name: "System Firmware", Class: "Firmware", Instance: `ACPI\PNP0C01\0`, Battery: -1},
		{Name: "Volume", Class: "Volume", Instance: `STORAGE\VOLUME\1&30A96598&0`, Battery: -1},
	}
	for _, r := range drop {
		if d, ok := classifyDevice(r); ok {
			t.Fatalf("%q should be hidden, got %+v", r.Name, d)
		}
	}
}

func TestBuildDeviceListOrderAndDedupe(t *testing.T) {
	raw := []rawDevice{
		{Name: "Logitech USB Mouse", Class: "Mouse", Instance: `USB\VID_046D&PID_C077\1`, Battery: -1},
		{Name: "Logitech USB Mouse", Class: "HIDClass", Instance: `USB\VID_046D&PID_C077&MI_00\2`, Battery: -1},
		{Name: "Speakers (Realtek(R) Audio)", Class: "AudioEndpoint", Instance: `SWD\MMDEVAPI\a`, Battery: -1},
		{Name: "SanDisk Ultra", Class: "USB", Instance: `USB\VID_0781&PID_5581\3`, Battery: -1},
		{Name: "USB Root Hub (USB 3.0)", Class: "USB", Instance: `USB\ROOT_HUB30\4`, Battery: -1},
	}
	got := buildDeviceList(raw, 10)
	if len(got) != 3 {
		t.Fatalf("want 3 unique devices, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "audio" || got[1].Kind != "mouse" || got[2].Kind != "usb" {
		t.Fatalf("order wrong: %+v", got)
	}
	if short := buildDeviceList(raw, 2); len(short) != 2 {
		t.Fatalf("limit ignored: %+v", short)
	}
	if empty := buildDeviceList(nil, 10); empty == nil || len(empty) != 0 {
		t.Fatalf("empty list must be a slice, got %#v", empty)
	}
}

func TestCleanDeviceName(t *testing.T) {
	cases := map[string]string{
		"Galaxy Buds2 Pro Hands-Free AG Audio":    "Galaxy Buds2 Pro",
		"Galaxy Buds2 Pro Hands-Free AG":          "Galaxy Buds2 Pro",
		"WH-1000XM4 Avrcp Transport":              "WH-1000XM4",
		"Bluetooth LE Device (Galaxy Buds2 Pro)":  "Galaxy Buds2 Pro",
		"soundcore Space Q45 Hands-Free AG Audio": "soundcore Space Q45",
		"MX Master 3S":        "MX Master 3S",
		"Hands-Free AG Audio": "Hands-Free AG",
	}
	for in, want := range cases {
		if got := cleanDeviceName(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestBetterBattery(t *testing.T) {
	if betterBattery(-1, 70) != 70 || betterBattery(70, -1) != 70 {
		t.Fatal("unknown must lose")
	}
	if betterBattery(70, 68) != 68 {
		t.Fatal("1% reading must beat a 10% step")
	}
	if betterBattery(68, 70) != 68 {
		t.Fatal("keep the finer reading")
	}
	if betterBattery(50, 60) != 50 {
		t.Fatal("same granularity keeps the first")
	}
}

func TestBatteryFromProp(t *testing.T) {
	if batteryFromProp(0x11, []byte{87}) != 87 {
		t.Fatal("BYTE")
	}
	if batteryFromProp(0x12, []byte{60, 0}) != 60 {
		t.Fatal("UINT16")
	}
	if batteryFromProp(0x13, []byte{100, 0, 0, 0}) != 100 {
		t.Fatal("UINT32")
	}
	if batteryFromProp(0x11, []byte{0xFF}) != -1 {
		t.Fatal("255 means unknown, not 255%")
	}
	if batteryFromProp(0x11, []byte{120}) != -1 {
		t.Fatal("out of range")
	}
	if batteryFromProp(0x05, []byte{50}) != -1 {
		t.Fatal("unsupported type")
	}
	if batteryFromProp(0x13, []byte{1, 2}) != -1 {
		t.Fatal("short buffer")
	}
}

func TestBuildDeviceListBatteries(t *testing.T) {
	raw := []rawDevice{
		{Name: "Galaxy Buds2 Pro Hands-Free AG Audio", Class: "System", Instance: `BTHENUM\{0000110b}_VID&0001000f\7&1`, Battery: 68},
		{Name: "Galaxy Buds2 Pro Avrcp Transport", Class: "System", Instance: `BTHENUM\{0000110e}\7&2`, Battery: 70},
		{Name: "Logitech USB Mouse", Class: "Mouse", Instance: `USB\VID_046D&PID_C077\1`, Battery: -1},
		{Name: "Galaxy Buds2 Pro", Class: "Bluetooth", Instance: `BTHENUM\DEV_ABCDEF\7&3`, Battery: -1},
	}
	got := buildDeviceList(raw, 10)
	if len(got) != 2 {
		t.Fatalf("want 2 devices, got %d: %+v", len(got), got)
	}
	var buds *Device
	for i := range got {
		if got[i].Name == "Galaxy Buds2 Pro" {
			buds = &got[i]
		}
	}
	if buds == nil {
		t.Fatalf("buds missing: %+v", got)
	}
	if buds.Battery != 68 {
		t.Fatalf("battery = %d, want the 1%% reading 68", buds.Battery)
	}
	for _, d := range got {
		if d.Name == "Logitech USB Mouse" && d.Battery != -1 {
			t.Fatalf("mouse should have no battery: %+v", d)
		}
	}
	only := buildDeviceList([]rawDevice{{Name: "WH-1000XM4 Hands-Free AG", Class: "System", Instance: `BTHENUM\{0000111e}\9&1`, Battery: 90}}, 10)
	if len(only) != 1 || only[0].Kind != "bt" || only[0].Battery != 90 || only[0].Name != "WH-1000XM4" {
		t.Fatalf("profile-only headset: %+v", only)
	}
}
