//go:build !darwin && !windows

package autostart

const startsOnEnable = false

func start(exe string) error { return spawn(exe) }
