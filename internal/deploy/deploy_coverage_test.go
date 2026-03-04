package deploy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"golang.org/x/crypto/ssh"
)

type writeCall struct {
	path string
	data []byte
	perm os.FileMode
}

type fakeDeploySSHClient struct {
	connectErr    error
	disconnectErr error
	writeErr      error
	writes        []writeCall
}

func (f *fakeDeploySSHClient) Connect(sync.ConnectOptions) error {
	return f.connectErr
}

func (f *fakeDeploySSHClient) Disconnect() error {
	return f.disconnectErr
}

func (f *fakeDeploySSHClient) WriteFile(path string, data []byte, perm os.FileMode) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	f.writes = append(f.writes, writeCall{path: path, data: cp, perm: perm})
	return nil
}

func newCoverageDeployer(t *testing.T) (*Deployer, *fakeDeploySSHClient) {
	t.Helper()
	m := &sync.Machine{Name: "m1", Address: "127.0.0.1", Port: 22, SSHUser: "hope"}
	d := NewDeployer(m, slog.Default())
	fake := &fakeDeploySSHClient{}
	d.sshClient = fake
	return d, fake
}

func TestDeployCoverageRemoteAndVersionPaths(t *testing.T) {
	d, _ := newCoverageDeployer(t)
	ctx := context.Background()

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case cmd == "echo $HOME":
			return "/home/hope\n", nil
		case strings.Contains(cmd, "test -x '/usr/local/bin/caam'"):
			return "", errors.New("not found")
		case strings.Contains(cmd, "test -x '/usr/bin/caam'"):
			return "yes\n", nil
		case strings.Contains(cmd, "--version"):
			return "v1.2.3\n", nil
		default:
			return "", nil
		}
	}

	remote, err := d.GetRemoteVersion(ctx)
	if err != nil {
		t.Fatalf("GetRemoteVersion failed: %v", err)
	}
	if remote != "v1.2.3" {
		t.Fatalf("unexpected remote version: %q", remote)
	}

	d.localVersion = "v1.2.3"
	local, err := d.GetLocalVersion()
	if err != nil {
		t.Fatalf("GetLocalVersion failed: %v", err)
	}
	if local != "v1.2.3" {
		t.Fatalf("unexpected local version: %q", local)
	}

	needsUpdate, localVer, remoteVer, err := d.NeedsUpdate(ctx)
	if err != nil {
		t.Fatalf("NeedsUpdate failed: %v", err)
	}
	if needsUpdate {
		t.Fatalf("expected no update needed, got update=true")
	}
	if localVer != "v1.2.3" || remoteVer != "v1.2.3" {
		t.Fatalf("unexpected versions local=%q remote=%q", localVer, remoteVer)
	}

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case cmd == "echo $HOME":
			return "/home/hope\n", nil
		case strings.Contains(cmd, "test -x '/usr/local/bin/caam'"):
			return "yes\n", nil
		case strings.Contains(cmd, "--version"):
			return "v9.9.9\n", nil
		default:
			return "", nil
		}
	}
	needsUpdate, localVer, remoteVer, err = d.NeedsUpdate(ctx)
	if err != nil {
		t.Fatalf("NeedsUpdate mismatch case failed: %v", err)
	}
	if !needsUpdate || localVer != "v1.2.3" || remoteVer != "v9.9.9" {
		t.Fatalf("expected update needed with version mismatch, got update=%v local=%q remote=%q", needsUpdate, localVer, remoteVer)
	}
}

func TestDeployCoverageUploadAndConfigWrites(t *testing.T) {
	d, fake := newCoverageDeployer(t)
	ctx := context.Background()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "caam")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write local binary: %v", err)
	}
	d.localBinary = binPath

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case cmd == "echo $HOME":
			return "/home/hope\n", nil
		case strings.HasPrefix(cmd, "sudo mv "):
			return "", errors.New("sudo denied")
		case strings.HasPrefix(cmd, "mkdir -p "):
			return "", nil
		default:
			return "", nil
		}
	}

	installPath, err := d.UploadBinary(ctx)
	if err != nil {
		t.Fatalf("UploadBinary failed: %v", err)
	}
	if installPath != "/home/hope/bin/caam" {
		t.Fatalf("unexpected install path: %q", installPath)
	}
	if len(fake.writes) == 0 {
		t.Fatalf("expected at least one remote file write")
	}
	if fake.writes[0].path != "/home/hope/.caam_upload_tmp" {
		t.Fatalf("unexpected upload temp path: %q", fake.writes[0].path)
	}
	if fake.writes[0].perm != 0o755 {
		t.Fatalf("unexpected upload mode: %#o", fake.writes[0].perm)
	}

	if err := d.WriteCoordinatorConfig(ctx, CoordinatorConfig{Port: 7890, PollInterval: "1s"}); err != nil {
		t.Fatalf("WriteCoordinatorConfig failed: %v", err)
	}
	if len(fake.writes) < 2 {
		t.Fatalf("expected coordinator config write")
	}
	last := fake.writes[len(fake.writes)-1]
	if !strings.Contains(last.path, "/.config/caam/coordinator.json") {
		t.Fatalf("unexpected coordinator config path: %q", last.path)
	}

	if err := d.WriteSystemdUnit(ctx, "caam-coordinator", SystemdUnitConfig{
		Type:      "Auth Recovery Coordinator",
		ExecStart: "/usr/local/bin/caam auth-coordinator",
	}); err != nil {
		t.Fatalf("WriteSystemdUnit failed: %v", err)
	}
	last = fake.writes[len(fake.writes)-1]
	if !strings.Contains(last.path, "/.config/systemd/user/caam-coordinator.service") {
		t.Fatalf("unexpected systemd path: %q", last.path)
	}
	if !strings.Contains(string(last.data), "ExecStart=/usr/local/bin/caam auth-coordinator") {
		t.Fatalf("systemd content missing ExecStart")
	}
}

func TestDeployCoverageServiceAndPlatform(t *testing.T) {
	d, _ := newCoverageDeployer(t)
	ctx := context.Background()

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "loginctl enable-linger"):
			return "", nil
		case cmd == "systemctl --user daemon-reload":
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user enable"):
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user restart"):
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user status"):
			return "active (running)\n", nil
		case strings.HasPrefix(cmd, "systemctl --user stop"):
			return "", nil
		case cmd == "uname -s":
			return runtime.GOOS + "\n", nil
		case cmd == "uname -m":
			if runtime.GOARCH == "amd64" {
				return "x86_64\n", nil
			}
			if runtime.GOARCH == "arm64" {
				return "aarch64\n", nil
			}
			return runtime.GOARCH + "\n", nil
		default:
			return "", nil
		}
	}

	if err := d.EnableAndStartService(ctx, "caam-coordinator"); err != nil {
		t.Fatalf("EnableAndStartService failed: %v", err)
	}

	status, err := d.GetServiceStatus(ctx, "caam-coordinator")
	if err != nil {
		t.Fatalf("GetServiceStatus failed: %v", err)
	}
	if !strings.Contains(status, "active") {
		t.Fatalf("unexpected service status: %q", status)
	}

	if err := d.StopService(ctx, "caam-coordinator"); err != nil {
		t.Fatalf("StopService failed: %v", err)
	}

	if got := d.GetRemoteOS(ctx); got != runtime.GOOS {
		t.Fatalf("GetRemoteOS mismatch: got %q want %q", got, runtime.GOOS)
	}
	canDeploy, reason := d.CanDeploy(ctx)
	if !canDeploy {
		t.Fatalf("expected CanDeploy=true, got false (%s)", reason)
	}

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch cmd {
		case "uname -s":
			return "mismatch-os\n", nil
		case "uname -m":
			return "x86_64\n", nil
		default:
			return "", nil
		}
	}
	canDeploy, reason = d.CanDeploy(ctx)
	if canDeploy || !strings.Contains(reason, "OS mismatch") {
		t.Fatalf("expected OS mismatch, got canDeploy=%v reason=%q", canDeploy, reason)
	}
}

func TestDeployCoverageCoordinatorFlow(t *testing.T) {
	d, _ := newCoverageDeployer(t)
	ctx := context.Background()
	d.localVersion = "v1.0.0"

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case cmd == "echo $HOME":
			return "/home/hope\n", nil
		case strings.Contains(cmd, "test -x '/usr/local/bin/caam'"):
			return "yes\n", nil
		case strings.Contains(cmd, "--version"):
			return "v1.0.0\n", nil
		case strings.HasPrefix(cmd, "mkdir -p "):
			return "", nil
		case cmd == "systemctl --user daemon-reload":
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user enable"):
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user restart"):
			return "", nil
		case strings.HasPrefix(cmd, "systemctl --user status"):
			return "active\n", nil
		case strings.HasPrefix(cmd, "loginctl enable-linger"):
			return "", nil
		case cmd == "uname -s":
			return runtime.GOOS + "\n", nil
		case cmd == "uname -m":
			if runtime.GOARCH == "amd64" {
				return "x86_64\n", nil
			}
			if runtime.GOARCH == "arm64" {
				return "aarch64\n", nil
			}
			return runtime.GOARCH + "\n", nil
		default:
			return "", nil
		}
	}

	result, err := d.DeployCoordinator(ctx, DefaultCoordinatorConfig())
	if err != nil {
		t.Fatalf("DeployCoordinator failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected successful deploy result")
	}
	if result.BinaryUpdated {
		t.Fatalf("expected no binary upload for matching versions")
	}
	if !result.ConfigWritten {
		t.Fatalf("expected config written")
	}

	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch cmd {
		case "uname -s":
			return "other-os\n", nil
		case "uname -m":
			return "x86_64\n", nil
		default:
			return "", nil
		}
	}
	if _, err := d.DeployCoordinator(ctx, DefaultCoordinatorConfig()); err == nil {
		t.Fatalf("expected deploy rejection on incompatible platform")
	}
}

func TestDeployCoverageLocalBinaryAndVersion(t *testing.T) {
	d, _ := newCoverageDeployer(t)

	tmpDir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	// Scenario 1: local ./caam found.
	localScript := filepath.Join(tmpDir, "caam")
	if err := os.WriteFile(localScript, []byte("#!/bin/sh\necho v2.0.0\n"), 0o755); err != nil {
		t.Fatalf("write local script: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	path, err := d.findLocalBinary()
	if err != nil {
		t.Fatalf("findLocalBinary failed: %v", err)
	}
	if !strings.Contains(path, "caam") {
		t.Fatalf("unexpected local binary path: %q", path)
	}

	d.localBinary = localScript
	d.localVersion = ""
	ver, err := d.GetLocalVersion()
	if err != nil {
		t.Fatalf("GetLocalVersion failed: %v", err)
	}
	if ver != "v2.0.0" {
		t.Fatalf("unexpected local version: %q", ver)
	}

	// Scenario 2: fallback via PATH.
	d2, _ := newCoverageDeployer(t)
	pathDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("mkdir path dir: %v", err)
	}
	pathScript := filepath.Join(pathDir, "caam")
	if err := os.WriteFile(pathScript, []byte("#!/bin/sh\necho v3.0.0\n"), 0o755); err != nil {
		t.Fatalf("write path script: %v", err)
	}

	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d2.localBinary = ""
	looked, err := d2.findLocalBinary()
	if err != nil {
		t.Fatalf("findLocalBinary via PATH failed: %v", err)
	}
	if !strings.Contains(looked, "caam") {
		t.Fatalf("unexpected looked-up path: %q", looked)
	}
}

func TestDeployCoverageConnectAndRunCommandErrors(t *testing.T) {
	// Connect with immediate SSH client failure.
	d, fake := newCoverageDeployer(t)
	fake.connectErr = errors.New("connect denied")
	if err := d.Connect(); err == nil {
		t.Fatalf("expected connect failure")
	}

	// Connect with SSH client success but command client setup failure.
	d2, _ := newCoverageDeployer(t)
	d2.machine.SSHKeyPath = filepath.Join(t.TempDir(), "missing-key")
	if err := d2.Connect(); err == nil {
		t.Fatalf("expected command connection failure")
	}

	// RunCommand with no connected client.
	if _, err := d2.RunCommand(context.Background(), "echo hi"); err == nil {
		t.Fatalf("expected not connected error")
	}

	// Disconnect should delegate safely even without command client.
	if err := d2.Disconnect(); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}
}

func TestDeployCoverageFindBinaryFallbackAndVersionMissing(t *testing.T) {
	d, _ := newCoverageDeployer(t)
	ctx := context.Background()

	d.localVersion = "v1.0.0"
	d.commandRunner = func(_ context.Context, cmd string) (string, error) {
		switch {
		case cmd == "echo $HOME":
			return "/home/hope\n", nil
		case strings.Contains(cmd, "test -x '/usr/local/bin/caam'"):
			return "", errors.New("missing")
		case strings.Contains(cmd, "test -x '/usr/bin/caam'"):
			return "", errors.New("missing")
		case cmd == "which caam":
			return "/opt/bin/caam\n", nil
		case strings.Contains(cmd, "--version"):
			return "", errors.New("not installed")
		case strings.HasPrefix(cmd, "systemctl --user status"):
			return "", errors.New("status missing")
		default:
			return "", nil
		}
	}

	path := d.findBinaryPath(ctx)
	if path != "/opt/bin/caam" {
		t.Fatalf("expected which fallback path, got %q", path)
	}

	needsUpdate, localVer, remoteVer, err := d.NeedsUpdate(ctx)
	if err != nil {
		t.Fatalf("NeedsUpdate error path should not fail: %v", err)
	}
	if !needsUpdate || localVer != "v1.0.0" || remoteVer != "" {
		t.Fatalf("expected update when remote version missing; got update=%v local=%q remote=%q", needsUpdate, localVer, remoteVer)
	}

	status, err := d.GetServiceStatus(ctx, "missing-service")
	if err != nil {
		t.Fatalf("GetServiceStatus should swallow command error: %v", err)
	}
	if status != "unknown" {
		t.Fatalf("unexpected status fallback: %q", status)
	}
}

func TestDeployCoverageConnectUsesDefaultSSHPort(t *testing.T) {
	d, _ := newCoverageDeployer(t)
	d.machine.Port = 0
	d.machine.Address = "127.0.0.1"
	d.machine.SSHKeyPath = filepath.Join(t.TempDir(), "invalid")

	err := d.Connect()
	if err == nil {
		t.Fatalf("expected connect failure with invalid key")
	}
	// Ensure Connect attempted to build host:port with default 22 path.
	if !strings.Contains(d.machine.HostPort(), ":"+strconv.Itoa(sync.DefaultSSHPort)) {
		t.Fatalf("expected hostport to include default ssh port, got %q", d.machine.HostPort())
	}
}

func TestDeployCoverageRunCommandWithMockSSHServer(t *testing.T) {
	server := newMockSSHServer(t)
	server.setCommand("echo hello", "hello\n")
	addr := server.start(t)
	defer server.stop()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi port: %v", err)
	}

	m := &sync.Machine{Name: "mock", Address: host, Port: port, SSHUser: "test"}
	d := NewDeployer(m, slog.New(slog.NewTextHandler(io.Discard, nil)))

	deployConn, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial mock ssh server: %v", err)
	}
	defer deployConn.Close()
	d.client = deployConn

	out, err := d.RunCommand(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("RunCommand success path failed: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("unexpected command output: %q", out)
	}

	if _, err := d.RunCommand(context.Background(), "unknown command"); err != nil {
		t.Fatalf("unexpected RunCommand error for unknown command in mock server: %v", err)
	}
}
