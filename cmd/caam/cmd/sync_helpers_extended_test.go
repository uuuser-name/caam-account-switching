package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	syncstate "github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/stretchr/testify/require"
)

func TestPromptForMachineParsesVariants(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	m, err := promptForMachine(reader, &bytes.Buffer{})
	require.NoError(t, err)
	require.Nil(t, m)

	reader = bufio.NewReader(strings.NewReader("node-a\n\n"))
	_, err = promptForMachine(reader, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "address required")

	reader = bufio.NewReader(strings.NewReader("node-b\nalice@10.0.0.2:2222\n~/.ssh/id_ed25519\n"))
	m, err = promptForMachine(reader, &bytes.Buffer{})
	require.NoError(t, err)
	require.Equal(t, "node-b", m.Name)
	require.Equal(t, "10.0.0.2", m.Address)
	require.Equal(t, "alice", m.SSHUser)
	require.Equal(t, 2222, m.Port)
	require.Equal(t, "~/.ssh/id_ed25519", m.SSHKeyPath)
}

func TestRunSyncStatusJSONAndHelpers(t *testing.T) {
	state := syncstate.NewSyncState(t.TempDir())
	state.Identity = &syncstate.LocalIdentity{Hostname: "local-host"}
	state.Pool.AutoSync = true
	state.Pool.LastFullSync = time.Now().Add(-15 * time.Minute)

	m := syncstate.NewMachine("node-1", "10.0.0.8")
	m.Status = syncstate.StatusOnline
	m.LastSync = time.Now().Add(-1 * time.Hour)
	require.NoError(t, state.Pool.AddMachine(m))

	state.Queue.Entries = append(state.Queue.Entries, syncstate.QueueEntry{
		Provider: "codex",
		Profile:  "work",
		Machine:  m.ID,
	})
	state.History.Entries = append(state.History.Entries, syncstate.HistoryEntry{
		Trigger:  "manual",
		Provider: "codex",
		Profile:  "work",
		Machine:  m.Name,
		Success:  true,
	})

	var out bytes.Buffer
	require.NoError(t, runSyncStatusJSON(state, &out))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Equal(t, "local-host", payload["local_machine"])
	require.Equal(t, true, payload["auto_sync"])
	require.Equal(t, float64(1), payload["queue_pending"])
	require.Equal(t, float64(1), payload["history_count"])
	require.NotNil(t, payload["machines"])

	require.Equal(t, "🟢", getStatusIcon(syncstate.StatusOnline))
	require.Equal(t, "🔴", getStatusIcon(syncstate.StatusOffline))
	require.Equal(t, "🔄", getStatusIcon(syncstate.StatusSyncing))
	require.Equal(t, "⚠️", getStatusIcon(syncstate.StatusError))
	require.Equal(t, "⚪", getStatusIcon("unknown"))

	require.Equal(t, "just now", formatTimeAgo(time.Now().Add(-10*time.Second)))
	require.Contains(t, formatTimeAgo(time.Now().Add(-2*time.Minute)), "mins ago")
	require.Contains(t, formatTimeAgo(time.Now().Add(-2*time.Hour)), "hours ago")
	require.Contains(t, formatTimeAgo(time.Now().Add(-48*time.Hour)), "days ago")
}

func TestRunSyncQueueProcessEmptyQueue(t *testing.T) {
	state := syncstate.NewSyncState(t.TempDir())
	var out bytes.Buffer
	require.NoError(t, runSyncQueueProcess(state, &out))
	require.Contains(t, out.String(), "Sync queue is empty")
}
