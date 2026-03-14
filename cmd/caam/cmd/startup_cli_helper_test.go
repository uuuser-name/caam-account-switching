package cmd

import (
	"fmt"
	"os"
	"testing"
)

func TestStartupCLIHelper(t *testing.T) {
	if os.Getenv("GO_WANT_STARTUP_CLI_HELPER") != "1" {
		return
	}

	args := os.Args
	split := -1
	for i, arg := range args {
		if arg == "--" {
			split = i
			break
		}
	}
	if split == -1 || split == len(args)-1 {
		fmt.Fprintln(os.Stderr, "missing startup helper args")
		os.Exit(2)
	}

	cliArgs := append([]string(nil), args[split+1:]...)
	if cliArgs[0] != "caam" {
		cliArgs = append([]string{"caam"}, cliArgs...)
	}
	os.Args = cliArgs

	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
