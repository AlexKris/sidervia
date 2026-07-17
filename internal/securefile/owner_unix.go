//go:build darwin || linux || freebsd || openbsd || netbsd

package securefile

import (
	"errors"
	"os"
	"syscall"
)

func checkOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine secret file owner")
	}
	if int(stat.Uid) != os.Geteuid() {
		return errors.New("secret file must be owned by the running user")
	}
	return nil
}
