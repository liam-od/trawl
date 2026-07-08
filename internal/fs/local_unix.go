//go:build !windows

package fs

// CleanName is the identity on POSIX filesystems: any component that can
// arrive from the other side of a transfer is already legal here (only '/' and
// NUL are forbidden, and neither survives being a directory entry name).
func (localFS) CleanName(name string) string { return name }
