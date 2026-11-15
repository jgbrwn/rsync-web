package srv

import "golang.org/x/sys/unix"

// diskSpace reports the size of the filesystem holding dir and the space available to this user.
func diskSpace(dir string) (total, free uint64) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	return uint64(st.Blocks) * bs, uint64(st.Bavail) * bs
}
