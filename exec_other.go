//go:build !unix

package main

import (
	"os"
	"os/exec"
	"os/signal"
)

// execAgent runs the agent as a child with this terminal; interrupts belong
// to the agent, so gh-start ignores them while it waits.
func execAgent(bin string, argv []string) error {
	signal.Ignore(os.Interrupt)
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		os.Exit(exit.ExitCode())
	}
	return err
}
