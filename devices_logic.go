package main

import (
	"encoding/binary"
	"sort"
	"strings"
)

// Device is one connected piece of hardware, as the window lists it.
type Device struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // audio, mouse, keyboard, usb, bt, phone, other
	Battery int    `json:"battery"` // percent, -1 when Windows does not report one
}

// rawDevice is what SetupAPI reports before filtering.
type rawDevice struct {
	Name     string // friendly name, else device description
	Class    string // SPDRP_CLASS, e.g. "Mouse", "AudioEndpoint"
	Instance string // device instance id, e.g. "USB\VID_046D&PID_C52B\..."
	Battery  int    // percent from the Bluetooth battery device property, -1 unknown
}

var kindOrder = map[string]int{"audio": 0, "mouse": 1, "keyboard": 2, "usb": 3, "bt": 4, "phone": 5, "other": 6}

// noiseNames are generic containers Windows reports as devices; they say nothing about
// what the user plugged in, so they are left out.
var noiseNames = []string{
	"usb root hub", "generic usb hub", "usb composite device", "usb hub",
	"microsoft bluetooth enumerator", "microsoft bluetooth le enumerator",
	"root hub", "superSpeed hub",
}

func isNoise(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	for _, bad := range noiseNames {
		if strings.Contains(n, strings.ToLower(bad)) {
			return true
		}
	}
	return false
}

// classifyDevice decides whether a present device is something the user attached, and which
// group it belongs to. Internal motherboard, ACPI and root-enumerated devices are dropped.
func classifyDevice(r rawDevice) (Device, bool) {
	name := strings.TrimSpace(r.Name)
	if isNoise(name) {
		return Device{}, false
	}
	cls := strings.ToLower(strings.TrimSpace(r.Class))
	inst := strings.ToLower(r.Instance)
	external := strings.HasPrefix(inst, `usb\`) || strings.HasPrefix(inst, `bthenum\`) ||
		strings.HasPrefix(inst, `bthle\`) || strings.HasPrefix(inst, `bthhfenum\`)
	lname := strings.ToLower(name)

	switch cls {
	case "audioendpoint", "media":
		return Device{Name: name, Kind: "audio", Battery: -1}, true
	case "mouse":
		return Device{Name: name, Kind: "mouse", Battery: -1}, true
	case "keyboard":
		return Device{Name: name, Kind: "keyboard", Battery: -1}, true
	case "usb":
		return Device{Name: name, Kind: "usb", Battery: -1}, true
	case "bluetooth":
		return Device{Name: name, Kind: "bt", Battery: -1}, true
	case "wpd", "portabledevices":
		return Device{Name: name, Kind: "phone", Battery: -1}, true
	case "ports", "modem":
		if external {
			return Device{Name: name, Kind: "usb", Battery: -1}, true
		}
	case "diskdrive", "net", "smartcardreader", "printer", "printqueue", "image":
		if external {
			if strings.HasPrefix(inst, `bthenum\`) || strings.HasPrefix(inst, `bthle\`) {
				return Device{Name: name, Kind: "bt", Battery: -1}, true
			}
			return Device{Name: name, Kind: "usb", Battery: -1}, true
		}
	case "hidclass":
		// external HID only: USB or Bluetooth. The laptop's own touchpad is HID over
		// ACPI/I2C and is deliberately not listed.
		if !external {
			return Device{}, false
		}
		switch {
		case strings.Contains(lname, "mouse"), strings.Contains(lname, "ماوس"):
			return Device{Name: name, Kind: "mouse", Battery: -1}, true
		case strings.Contains(lname, "keyboard"), strings.Contains(lname, "كيبورد"):
			return Device{Name: name, Kind: "keyboard", Battery: -1}, true
		case strings.Contains(lname, "headset"), strings.Contains(lname, "headphone"), strings.Contains(lname, "سماع"):
			return Device{Name: name, Kind: "audio", Battery: -1}, true
		case strings.HasPrefix(inst, `bthenum\`) || strings.HasPrefix(inst, `bthle\`) || strings.HasPrefix(inst, `bthhfenum\`):
			return Device{Name: name, Kind: "bt", Battery: -1}, true
		}
		return Device{Name: name, Kind: "usb", Battery: -1}, true
	}
	return Device{}, false
}

// buildDeviceList filters, de-duplicates, merges battery readings and orders the devices.
func buildDeviceList(raw []rawDevice, limit int) []Device {
	seen := map[string]bool{}
	out := []Device{}
	for _, r := range raw {
		d, ok := classifyDevice(r)
		if !ok {
			continue
		}
		key := strings.ToLower(d.Kind + "|" + d.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	// Second pass: a Bluetooth headset often appears only as a profile node ("... Hands-Free AG"),
	// which is exactly the node that carries the battery. Merge those readings into the device
	// they belong to, or list the device on its own when nothing else matched.
	for _, r := range raw {
		if r.Battery < 0 || !isBluetoothInstance(r.Instance) {
			continue
		}
		name := cleanDeviceName(r.Name)
		if name == "" {
			continue
		}
		merged := false
		for i := range out {
			if sameDevice(out[i].Name, name) {
				out[i].Battery = betterBattery(out[i].Battery, r.Battery)
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		key := "bt|" + strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Device{Name: name, Kind: "bt", Battery: r.Battery})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if kindOrder[out[i].Kind] != kindOrder[out[j].Kind] {
			return kindOrder[out[i].Kind] < kindOrder[out[j].Kind]
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ---- device batteries ----

// isBluetoothInstance reports whether a device instance id belongs to the Bluetooth stack.
func isBluetoothInstance(inst string) bool {
	l := strings.ToLower(inst)
	return strings.HasPrefix(l, `bth`) // BTH\, BTHENUM\, BTHLE\, BTHLEDevice\, BTHHFENUM\
}

// nameSuffixes are the profile words Windows adds to a Bluetooth device's nodes.
var nameSuffixes = []string{
	" hands-free ag audio", " hands-free ag", " hands-free", " handsfree",
	" avrcp transport", " a2dp", " hfp", " rfcomm", " (rfcomm)",
	" bluetooth le device", " stereo", " audio",
}

// cleanDeviceName turns "Galaxy Buds2 Pro Hands-Free AG Audio" into "Galaxy Buds2 Pro".
func cleanDeviceName(s string) string {
	n := strings.TrimSpace(s)
	if low := strings.ToLower(n); strings.HasPrefix(low, "bluetooth le device (") && strings.HasSuffix(n, ")") {
		n = strings.TrimSpace(n[len("bluetooth le device (") : len(n)-1])
	}
	for changed := true; changed; {
		changed = false
		low := strings.ToLower(n)
		for _, suf := range nameSuffixes {
			if strings.HasSuffix(low, suf) && len(n) > len(suf) {
				n = strings.TrimSpace(n[:len(n)-len(suf)])
				changed = true
				break
			}
		}
	}
	return strings.TrimSpace(n)
}

// betterBattery keeps the finer reading: Windows sometimes reports 10% steps over the
// hands-free profile and 1% steps over Bluetooth LE for the same earbuds.
func betterBattery(old, fresh int) int {
	if fresh < 0 {
		return old
	}
	if old < 0 {
		return fresh
	}
	if fresh%10 != 0 && old%10 == 0 {
		return fresh
	}
	return old
}

// sameDevice matches a profile node name against an already listed device.
func sameDevice(listed, candidate string) bool {
	a, b := strings.ToLower(listed), strings.ToLower(candidate)
	if a == b {
		return true
	}
	long, short := a, b
	if len(a) < len(b) {
		long, short = b, a
	}
	if len(short) < 4 || !strings.HasPrefix(long, short) {
		return false
	}
	rest := long[len(short):]
	return rest[0] == ' ' || rest[0] == '('
}

// batteryFromProp reads the numeric DEVPROP value Windows stores for a Bluetooth battery.
// DEVPROP_TYPE_BYTE (0x11) is what the hands-free profile uses; the others are accepted
// because BLE stacks have been seen to store wider integers.
func batteryFromProp(propType uint32, b []byte) int {
	var v int64 = -1
	switch propType {
	case 0x11: // BYTE
		if len(b) >= 1 {
			v = int64(b[0])
		}
	case 0x12: // UINT16
		if len(b) >= 2 {
			v = int64(binary.LittleEndian.Uint16(b))
		}
	case 0x13: // UINT32
		if len(b) >= 4 {
			v = int64(binary.LittleEndian.Uint32(b))
		}
	case 0x14: // INT16
		if len(b) >= 2 {
			v = int64(int16(binary.LittleEndian.Uint16(b)))
		}
	case 0x15: // INT32
		if len(b) >= 4 {
			v = int64(int32(binary.LittleEndian.Uint32(b)))
		}
	case 0x18: // UINT64
		if len(b) >= 8 {
			v = int64(binary.LittleEndian.Uint64(b))
		}
	}
	if v < 0 || v > 100 {
		return -1
	}
	return int(v)
}
