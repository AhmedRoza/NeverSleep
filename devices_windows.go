//go:build windows

package main

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SetupAPI is a plain DLL with plain structs: enumerating present devices costs a few
// milliseconds, spawns no process and needs no COM or WMI.
const (
	digcfPresent    = 0x00000002
	digcfAllClasses = 0x00000004
	spdrpDeviceDesc = 0x00000000
	spdrpClass      = 0x00000007
	spdrpFriendly   = 0x0000000C
)

// SP_DEVINFO_DATA is 32 bytes on 64-bit Windows: cbSize (0), GUID (4), DevInst (20), and the
// ULONG_PTR Reserved at 24, which is already 8-aligned, so there is no padding. Windows rejects
// the struct when cbSize is not exactly sizeof(SP_DEVINFO_DATA).
type spDevInfoData struct {
	cbSize    uint32
	ClassGuid windows.GUID
	DevInst   uint32
	Reserved  uintptr
}

const spDevInfoDataSize = 32

// DEVPROPKEY {104EA319-6EE2-4701-BD47-8DDBF425BBE5}, 2 is where Windows keeps the battery
// level of a Bluetooth device (the number Settings > Bluetooth shows). It is populated on
// the profile node (for example "... Hands-Free AG"), not on the device's main node.
type devPropKey struct {
	FmtID windows.GUID
	PID   uint32
}

var bluetoothBatteryKey = devPropKey{
	FmtID: windows.GUID{Data1: 0x104EA319, Data2: 0x6EE2, Data3: 0x4701,
		Data4: [8]byte{0xBD, 0x47, 0x8D, 0xDB, 0xF4, 0x25, 0xBB, 0xE5}},
	PID: 2,
}

// devBattery reads the Bluetooth battery property, or -1 when the device has none.
func devBattery(h uintptr, did *spDevInfoData) int {
	var typ uint32
	buf := make([]byte, 16)
	var need uint32
	r, _, _ := pSetupDiGetDeviceProperty.Call(h, uintptr(unsafe.Pointer(did)),
		uintptr(unsafe.Pointer(&bluetoothBatteryKey)), uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&need)), 0)
	if r == 0 {
		return -1
	}
	return batteryFromProp(typ, buf)
}

var (
	setupapi                          = windows.NewLazySystemDLL("setupapi.dll")
	pSetupDiGetClassDevs              = setupapi.NewProc("SetupDiGetClassDevsW")
	pSetupDiEnumDeviceInfo            = setupapi.NewProc("SetupDiEnumDeviceInfo")
	pSetupDiGetDeviceRegistryProperty = setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	pSetupDiGetDeviceInstanceID       = setupapi.NewProc("SetupDiGetDeviceInstanceIdW")
	pSetupDiGetDeviceProperty         = setupapi.NewProc("SetupDiGetDevicePropertyW")
	pSetupDiDestroyDeviceInfoList     = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
)

const devMaxAge = 30 * time.Second

var (
	devMu   sync.Mutex
	devAt   time.Time
	devList []Device
)

// devicesCached returns the connected-device list, re-enumerating at most every devMaxAge.
// It is only called while the window is open, so an idle tray costs nothing.
func devicesCached() []Device {
	devMu.Lock()
	if devList != nil && time.Since(devAt) < devMaxAge {
		l := devList
		devMu.Unlock()
		return l
	}
	devMu.Unlock()
	l := enumDevices()
	devMu.Lock()
	devList, devAt = l, time.Now()
	devMu.Unlock()
	return l
}

// invalidateDevices makes the next request re-enumerate (a device was plugged or removed).
func invalidateDevices() {
	devMu.Lock()
	devAt = time.Time{}
	devMu.Unlock()
}

func devProp(h uintptr, did *spDevInfoData, prop uint32) string {
	buf := make([]uint16, 512)
	var regType, need uint32
	r, _, _ := pSetupDiGetDeviceRegistryProperty.Call(h, uintptr(unsafe.Pointer(did)), uintptr(prop),
		uintptr(unsafe.Pointer(&regType)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func devInstance(h uintptr, did *spDevInfoData) string {
	buf := make([]uint16, 512)
	var need uint32
	r, _, _ := pSetupDiGetDeviceInstanceID.Call(h, uintptr(unsafe.Pointer(did)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// enumDevices walks every present device once and keeps the ones a person plugged in.
func enumDevices() (out []Device) {
	defer func() {
		if recover() != nil {
			out = []Device{}
		}
	}()
	if unsafe.Sizeof(spDevInfoData{}) != spDevInfoDataSize {
		return []Device{} // never call SetupAPI with a struct it will reject
	}
	h, _, _ := pSetupDiGetClassDevs.Call(0, 0, 0, uintptr(digcfPresent|digcfAllClasses))
	if h == 0 || h == uintptr(windows.InvalidHandle) {
		return []Device{}
	}
	defer pSetupDiDestroyDeviceInfoList.Call(h)

	raw := make([]rawDevice, 0, 64)
	for i := 0; i < 2048; i++ {
		var did spDevInfoData
		did.cbSize = uint32(unsafe.Sizeof(did))
		r, _, _ := pSetupDiEnumDeviceInfo.Call(h, uintptr(i), uintptr(unsafe.Pointer(&did)))
		if r == 0 {
			break
		}
		name := devProp(h, &did, spdrpFriendly)
		if name == "" {
			name = devProp(h, &did, spdrpDeviceDesc)
		}
		inst := devInstance(h, &did)
		bat := -1
		if isBluetoothInstance(inst) {
			bat = devBattery(h, &did)
		}
		raw = append(raw, rawDevice{
			Name:     name,
			Class:    devProp(h, &did, spdrpClass),
			Instance: inst,
			Battery:  bat,
		})
	}
	return buildDeviceList(raw, 24)
}
