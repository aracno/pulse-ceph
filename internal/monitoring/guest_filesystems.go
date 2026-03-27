package monitoring

// isWindowsGuestFilesystemMountpoint reports whether a guest-agent filesystem
// mountpoint represents a Windows drive. Accept both normalized drive roots
// like "C:" and nested paths like "C:\\Windows".
func isWindowsGuestFilesystemMountpoint(mountpoint string) bool {
	return len(mountpoint) >= 2 && mountpoint[1] == ':'
}
