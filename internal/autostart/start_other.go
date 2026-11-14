//go:build !darwin && !windows

package autostart

const startsOnEnable = false

func start(t Target) error { return detached(t.Exe, t.Args) }
