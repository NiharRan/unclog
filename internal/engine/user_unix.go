//go:build !windows

package engine

func currentUsername() string { return "" }
