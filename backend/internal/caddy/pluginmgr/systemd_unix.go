//go:build unix

package pluginmgr

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// platformUnsupported is empty where the permission check is available.
func platformUnsupported() string { return "" }

// readableBy checks that userName can read and execute path and search every
// parent directory. An empty userName means root, which can always access it.
func readableBy(path, userName string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if userName == "" || userName == "root" {
		return nil
	}
	u, err := user.Lookup(userName)
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return err
	}
	gids := map[uint32]bool{}
	if g, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
		gids[uint32(g)] = true
	}
	if groups, err := u.GroupIds(); err == nil {
		for _, s := range groups {
			if g, err := strconv.ParseUint(s, 10, 32); err == nil {
				gids[uint32(g)] = true
			}
		}
	}

	check := func(p string, need os.FileMode) error {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot read ownership of %s", p)
		}
		mode := fi.Mode().Perm()
		var have os.FileMode
		switch {
		case uint64(st.Uid) == uid:
			have = (mode >> 6) & 7
		case gids[st.Gid]:
			have = (mode >> 3) & 7
		default:
			have = mode & 7
		}
		if have&need != need {
			return fmt.Errorf("%s has insufficient permissions for user %s", p, userName)
		}
		return nil
	}

	if err := check(path, 5); err != nil { // r-x
		return err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if err := check(dir, 1); err != nil { // --x
			return err
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return nil
}
