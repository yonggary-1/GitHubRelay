//go:build !windows

package main

import (
	"fmt"
	"os"
)

// GitHub Relay is a Windows program. This stub only lets the core logic be built and tested elsewhere.
func main() {
	fmt.Fprintln(os.Stderr, "GitHub Relay runs on Windows 10/11 only.")
	os.Exit(1)
}
