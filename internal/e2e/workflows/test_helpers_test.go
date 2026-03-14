package workflows

import (
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

func withEnvOverrides(base []string, overrides map[string]string) []string {
	envMap := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		envMap[parts[0]] = parts[1]
	}
	for key, value := range overrides {
		envMap[key] = value
	}

	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+envMap[key])
	}
	return env
}

func registerProcessCleanup(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	t.Cleanup(func() {
		stopStartedProcess(cmd, timeout)
	})
}

func stopStartedProcess(cmd *exec.Cmd, timeout time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return
	}

	waitDone := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitDone)
	}()

	_ = cmd.Process.Signal(syscall.SIGTERM)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-waitDone:
		return
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-waitDone
	}
}
