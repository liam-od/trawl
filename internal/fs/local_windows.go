//go:build windows

package fs

// CleanName rewrites name so Windows accepts it; see cleanWindowsName.
func (localFS) CleanName(name string) string { return cleanWindowsName(name) }
