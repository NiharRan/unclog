package main

import (
	"os"
	"runtime/debug"

	"github.com/NiharRan/unclog/cmd"
)

func main() {
	// The disk walk makes lots of short-lived garbage; collecting earlier keeps peak memory low.
	debug.SetGCPercent(25)
	os.Exit(cmd.Execute())
}
