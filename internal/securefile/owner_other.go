//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package securefile

import "os"

func checkOwner(os.FileInfo) error { return nil }
