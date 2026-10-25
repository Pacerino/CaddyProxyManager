//go:build !unix

package pluginmgr

import "errors"

func platformUnsupported() string {
	return "plugin install via systemd is only supported on Linux"
}

func readableBy(path, userName string) error {
	return errors.New("not supported on this platform")
}
