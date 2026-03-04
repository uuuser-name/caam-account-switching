package cmd

import "testing"

func TestResumeCommandFlags(t *testing.T) {
	// Check session flag with shorthand exists
	sessionFlag := resumeCmd.Flags().Lookup("session")
	if sessionFlag == nil {
		t.Fatal("expected --session flag")
	}
	if sessionFlag.Shorthand != "s" {
		t.Fatalf("expected --session shorthand 's', got %q", sessionFlag.Shorthand)
	}

	noLockFlag := resumeCmd.Flags().Lookup("no-lock")
	if noLockFlag == nil {
		t.Fatal("expected --no-lock flag")
	}
}

func TestResumeCommandArgs(t *testing.T) {
	err := resumeCmd.Args(resumeCmd, []string{"codex", "work"})
	if err != nil {
		t.Fatalf("expected no error for valid args: %v", err)
	}

	err = resumeCmd.Args(resumeCmd, []string{"codex"})
	if err == nil {
		t.Fatal("expected error for missing profile arg")
	}
}
