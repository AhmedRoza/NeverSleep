package main

import (
	"testing"
	"unsafe"
)

// guid mirrors golang.org/x/sys/windows.GUID so the layout can be checked on any platform.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// spDevInfoData mirrors the Windows-only struct in devices_windows.go, field for field.
type spDevInfoDataMirror struct {
	cbSize    uint32
	ClassGuid guid
	DevInst   uint32
	Reserved  uintptr
}

func TestSPDevInfoDataLayout(t *testing.T) {
	var d spDevInfoDataMirror
	if got := unsafe.Sizeof(d); got != 32 {
		t.Fatalf("SP_DEVINFO_DATA size = %d, want 32", got)
	}
	if off := unsafe.Offsetof(d.ClassGuid); off != 4 {
		t.Fatalf("ClassGuid offset = %d, want 4", off)
	}
	if off := unsafe.Offsetof(d.DevInst); off != 20 {
		t.Fatalf("DevInst offset = %d, want 20", off)
	}
	if off := unsafe.Offsetof(d.Reserved); off != 24 {
		t.Fatalf("Reserved offset = %d, want 24 (the C struct puts ULONG_PTR there)", off)
	}
}
