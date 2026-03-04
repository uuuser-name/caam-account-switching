package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	syncpkg "github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/spf13/cobra"
)

func setupSyncHome(t *testing.T) {
	t.Helper()
	t.Setenv("CAAM_HOME", t.TempDir())
}

func loadAndSaveState(t *testing.T, mutate func(*syncpkg.SyncState)) {
	t.Helper()
	state := syncpkg.NewSyncState("")
	if err := state.Load(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if mutate != nil {
		mutate(state)
	}
	if err := state.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}
}

func TestRunSyncNoMachinesShowsGettingStarted(t *testing.T) {
	setupSyncHome(t)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("machine", "", "")

	if err := runSync(cmd, nil); err != nil {
		t.Fatalf("runSync failed: %v", err)
	}
	if !strings.Contains(out.String(), "No machines in sync pool.") {
		t.Fatalf("expected no-machines message, got: %q", out.String())
	}
}

func TestRunSyncDryRunSpecificMachine(t *testing.T) {
	setupSyncHome(t)
	loadAndSaveState(t, func(state *syncpkg.SyncState) {
		m := syncpkg.NewMachine("work-laptop", "10.0.0.2")
		if err := state.Pool.AddMachine(m); err != nil {
			t.Fatalf("add machine: %v", err)
		}
	})

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.Flags().Bool("dry-run", true, "")
	cmd.Flags().String("machine", "work-laptop", "")

	if err := runSync(cmd, nil); err != nil {
		t.Fatalf("runSync failed: %v", err)
	}
	if !strings.Contains(out.String(), "Dry run - would sync with:") || !strings.Contains(out.String(), "work-laptop") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunSyncAddAndRemoveForce(t *testing.T) {
	setupSyncHome(t)
	loadAndSaveState(t, nil)

	addCmd := &cobra.Command{}
	var addOut bytes.Buffer
	addCmd.SetOut(&addOut)
	addCmd.Flags().String("user", "", "")
	addCmd.Flags().String("key", "", "")
	addCmd.Flags().String("remote-path", "", "")
	addCmd.Flags().Bool("test", false, "")

	if err := runSyncAdd(addCmd, []string{"node-a", "alice@10.0.0.9:2222"}); err != nil {
		t.Fatalf("runSyncAdd failed: %v", err)
	}
	if !strings.Contains(addOut.String(), `Added machine "node-a"`) {
		t.Fatalf("unexpected add output: %q", addOut.String())
	}

	state := syncpkg.NewSyncState("")
	if err := state.Load(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	m := state.Pool.GetMachineByName("node-a")
	if m == nil {
		t.Fatal("expected machine node-a to exist")
	}
	if m.SSHUser != "alice" || m.Port != 2222 {
		t.Fatalf("unexpected machine parse result: user=%q port=%d", m.SSHUser, m.Port)
	}

	removeCmd := &cobra.Command{}
	var removeOut bytes.Buffer
	removeCmd.SetOut(&removeOut)
	removeCmd.Flags().Bool("force", true, "")
	if err := runSyncRemove(removeCmd, []string{"node-a"}); err != nil {
		t.Fatalf("runSyncRemove failed: %v", err)
	}
	if !strings.Contains(removeOut.String(), `Removed machine "node-a"`) {
		t.Fatalf("unexpected remove output: %q", removeOut.String())
	}
}

func TestRunSyncEnableDisableAndTestNoMachines(t *testing.T) {
	setupSyncHome(t)
	loadAndSaveState(t, nil)

	var out bytes.Buffer
	enableCmd := &cobra.Command{}
	enableCmd.SetOut(&out)
	if err := runSyncEnable(enableCmd, nil); err != nil {
		t.Fatalf("runSyncEnable failed: %v", err)
	}
	if !strings.Contains(out.String(), "Auto-sync is now enabled.") {
		t.Fatalf("unexpected enable output: %q", out.String())
	}

	out.Reset()
	testCmd := &cobra.Command{}
	testCmd.SetOut(&out)
	if err := runSyncTest(testCmd, nil); err != nil {
		t.Fatalf("runSyncTest failed: %v", err)
	}
	if !strings.Contains(out.String(), "No machines in sync pool.") {
		t.Fatalf("unexpected test output: %q", out.String())
	}

	out.Reset()
	disableCmd := &cobra.Command{}
	disableCmd.SetOut(&out)
	if err := runSyncDisable(disableCmd, nil); err != nil {
		t.Fatalf("runSyncDisable failed: %v", err)
	}
	if !strings.Contains(out.String(), "Auto-sync is now disabled.") {
		t.Fatalf("unexpected disable output: %q", out.String())
	}
}

func TestRunSyncLogAndQueueCommands(t *testing.T) {
	setupSyncHome(t)
	loadAndSaveState(t, func(state *syncpkg.SyncState) {
		state.History.Entries = append(state.History.Entries, syncpkg.HistoryEntry{
			Timestamp: time.Now().Add(-2 * time.Minute),
			Machine:   "node-a",
			Provider:  "codex",
			Profile:   "work",
			Action:    "push",
			Success:   true,
		})
		state.Queue.Entries = append(state.Queue.Entries, syncpkg.QueueEntry{
			Provider: "codex",
			Profile:  "work",
			Machine:  "node-a",
			Attempts: 1,
		})
	})

	var logOut bytes.Buffer
	logCmd := &cobra.Command{}
	logCmd.SetOut(&logOut)
	logCmd.Flags().Int("limit", 20, "")
	logCmd.Flags().String("machine", "", "")
	logCmd.Flags().String("provider", "", "")
	logCmd.Flags().Bool("errors", false, "")
	if err := runSyncLog(logCmd, nil); err != nil {
		t.Fatalf("runSyncLog failed: %v", err)
	}
	if !strings.Contains(logOut.String(), "Sync History") {
		t.Fatalf("unexpected log output: %q", logOut.String())
	}

	var queueOut bytes.Buffer
	queueCmd := &cobra.Command{}
	queueCmd.SetOut(&queueOut)
	queueCmd.Flags().Bool("clear", false, "")
	queueCmd.Flags().Bool("process", false, "")
	if err := runSyncQueue(queueCmd, nil); err != nil {
		t.Fatalf("runSyncQueue failed: %v", err)
	}
	if !strings.Contains(queueOut.String(), "Sync Queue: 1 pending") {
		t.Fatalf("unexpected queue output: %q", queueOut.String())
	}

	queueOut.Reset()
	clearCmd := &cobra.Command{}
	clearCmd.SetOut(&queueOut)
	clearCmd.Flags().Bool("clear", true, "")
	clearCmd.Flags().Bool("process", false, "")
	if err := runSyncQueue(clearCmd, nil); err != nil {
		t.Fatalf("runSyncQueue --clear failed: %v", err)
	}
	if !strings.Contains(queueOut.String(), "Cleared sync queue.") {
		t.Fatalf("unexpected clear output: %q", queueOut.String())
	}
}

func TestRunSyncInitCSVOnly(t *testing.T) {
	setupSyncHome(t)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.Flags().Bool("csv", true, "")
	cmd.Flags().Bool("discover", false, "")

	if err := runSyncInit(cmd, nil); err != nil {
		t.Fatalf("runSyncInit failed: %v", err)
	}

	if !strings.Contains(out.String(), "Created") && !strings.Contains(out.String(), "already exists") {
		t.Fatalf("unexpected init output: %q", out.String())
	}
}

