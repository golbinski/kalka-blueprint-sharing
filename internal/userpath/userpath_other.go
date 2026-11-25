//go:build !windows

package userpath

func add(string) (bool, error)    { return false, ErrUnsupported }
func remove(string) (bool, error) { return false, ErrUnsupported }
