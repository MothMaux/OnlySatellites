//go:build linux || darwin || freebsd || openbsd || netbsd

package shared

import (
	"fmt"
	"os"
)

func IsAdmin() bool {
	if os.Geteuid() == 0 {
		fmt.Println("The program is running as root, please run as regular user.")
		return true
	} else {
		return false
	}
}
