// Package main is the entry point for caam - Coding Agent Account Manager.
package main

import (
	"errors"
	"os"

	"github.com/Dicklesworthstone/coding_agent_account_manager/cmd/caam/cmd"
	caamexec "github.com/Dicklesworthstone/coding_agent_account_manager/internal/exec"
)

func main() {
	if err := cmd.Execute(); err != nil {
		var exitErr *caamexec.ExitCodeError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
