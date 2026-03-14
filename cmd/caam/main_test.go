package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestMainVersionAndGenericErrorExit(t *testing.T) {
	t.Run("version exits successfully", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
		cmd.Env = append(os.Environ(),
			"GO_WANT_CAAM_MAIN_HELPER=1",
			"CAAM_MAIN_SCENARIO=version",
		)
		if err := cmd.Run(); err != nil {
			t.Fatalf("version helper failed: %v", err)
		}
	})

	t.Run("invalid command exits one", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=TestMainHelperProcess")
		cmd.Env = append(os.Environ(),
			"GO_WANT_CAAM_MAIN_HELPER=1",
			"CAAM_MAIN_SCENARIO=invalid",
		)
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected invalid command helper to exit non-zero")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("helper error = %T, want *exec.ExitError", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("invalid command exit code = %d, want 1", exitErr.ExitCode())
		}
	})
}

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CAAM_MAIN_HELPER") != "1" {
		return
	}

	switch os.Getenv("CAAM_MAIN_SCENARIO") {
	case "version":
		os.Args = []string{"caam", "version"}
	case "invalid":
		os.Args = []string{"caam", "definitely-invalid-command"}
	default:
		os.Exit(2)
	}

	main()
	os.Exit(0)
}
