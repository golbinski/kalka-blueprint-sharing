//go:build !windows

package fsutil

const isWindows = false

// restrictToUser is a no-op where mode 0600 already does the job.
func restrictToUser(string) error { return nil }
