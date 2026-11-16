//go:build unix

package main

import (
	"os"
	"syscall"
)

// execAgent replaces gh-start with the agent so it owns the terminal.
func execAgent(bin string, argv []string) error {
	return syscall.Exec(bin, argv, os.Environ())
}
