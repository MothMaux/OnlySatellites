//go:build windows

package handlers

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func diskTotalsForPath(path string) (total, free uint64, err error) {
	// Extract volume root like "C:\"
	vol := windowsVolumeRoot(path)
	return getDiskFreeSpaceEx(vol)
}

func windowsVolumeRoot(p string) string {
	pp := strings.ReplaceAll(p, "/", `\`)
	// If already "C:\..."
	if len(pp) >= 3 && pp[1] == ':' && (pp[2] == '\\' || pp[2] == '/') {
		return pp[:3]
	}
	vol := filepath.VolumeName(pp)
	if vol == "" {
		return `C:\`
	}
	if !strings.HasSuffix(vol, `\`) && !strings.HasSuffix(vol, `/`) {
		vol += `\`
	}
	return vol
}

func getDiskFreeSpaceEx(path string) (total, free uint64, err error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel.NewProc("GetDiskFreeSpaceExW")

	var freeAvail, totalBytes, freeBytes uint64
	p16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	r, _, e := proc.Call(
		uintptr(unsafe.Pointer(p16)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&freeBytes)),
	)
	if r == 0 {
		if e != nil {
			return 0, 0, e
		}
		return 0, 0, syscall.EINVAL
	}
	// Report total volume size and total free
	return totalBytes, freeBytes, nil
}
