package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
)

// confirm asks the user to type expected before a destructive action.
// With yes set it proceeds; without a terminal it refuses, so scripts must
// pass --yes explicitly.
func (a *App) confirm(yes bool, action, expected string) error {
	if yes {
		return nil
	}
	if !a.IO.InTTY {
		return fmt.Errorf("refusing to %s without confirmation: pass --yes", action)
	}
	fmt.Fprintf(a.IO.Err, "To %s, type %q to confirm: ", action, expected)
	line, err := bufio.NewReader(a.IO.In).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != expected {
		return errors.New("confirmation did not match, aborting")
	}
	return nil
}
