//go:build windows

package shared

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func IsAdmin() bool {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if elevated {
		fmt.Println("Program running with elevated privilages, please run as regular user.")
	}
	return elevated
}
