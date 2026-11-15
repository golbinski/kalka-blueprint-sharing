//go:build !windows

package urlscheme

func systemRegistry() Registry { return nil }
