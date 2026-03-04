package setup

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/deploy"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/sync"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/tailscale"
	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/wezterm"
)

type fixtureTailscaleClient struct {
	available bool
	self      *tailscale.Peer
	selfErr   error
	peers     []*tailscale.Peer
	peersErr  error
	byHost    map[string]*tailscale.Peer
}

func (f *fixtureTailscaleClient) IsAvailable(context.Context) bool {
	return f.available
}

func (f *fixtureTailscaleClient) GetSelf(context.Context) (*tailscale.Peer, error) {
	return f.self, f.selfErr
}

func (f *fixtureTailscaleClient) GetPeers(context.Context) ([]*tailscale.Peer, error) {
	return f.peers, f.peersErr
}

func (f *fixtureTailscaleClient) FindPeerByHostname(_ context.Context, hostname string) (*tailscale.Peer, error) {
	if p, ok := f.byHost[hostname]; ok {
		return p, nil
	}
	return nil, nil
}

type fixtureCoordinatorDeployer struct {
	connectErr error
	deployErr  error
	result     *deploy.DeployResult
	lastConfig deploy.CoordinatorConfig
	connected  bool
	disposed   bool
}

func (f *fixtureCoordinatorDeployer) Connect() error {
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}

func (f *fixtureCoordinatorDeployer) Disconnect() error {
	f.disposed = true
	return nil
}

func (f *fixtureCoordinatorDeployer) DeployCoordinator(_ context.Context, config deploy.CoordinatorConfig) (*deploy.DeployResult, error) {
	f.lastConfig = config
	if f.deployErr != nil {
		return nil, f.deployErr
	}
	return f.result, nil
}

func saveSetupGlobals() func() {
	oldFind := findWezTermConfigPath
	oldParse := parseWezTermConfig
	oldNewTail := newTailscaleClient
	oldTestConn := testMachineConnectivity
	oldNewDeployer := newCoordinatorDeployer
	return func() {
		findWezTermConfigPath = oldFind
		parseWezTermConfig = oldParse
		newTailscaleClient = oldNewTail
		testMachineConnectivity = oldTestConn
		newCoordinatorDeployer = oldNewDeployer
	}
}

func TestDiscoverCoveragePaths(t *testing.T) {
	restore := saveSetupGlobals()
	defer restore()

	ctx := context.Background()

	findWezTermConfigPath = func() string { return "" }
	orch := NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	if err := orch.Discover(ctx); err == nil {
		t.Fatal("expected missing config error")
	}

	findWezTermConfigPath = func() string { return "/tmp/wezterm.lua" }
	parseWezTermConfig = func(string) (*wezterm.Config, error) { return nil, errors.New("parse fail") }
	orch = NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	if err := orch.Discover(ctx); err == nil {
		t.Fatal("expected parse error")
	}

	parseWezTermConfig = func(string) (*wezterm.Config, error) {
		return &wezterm.Config{SSHDomains: nil}, nil
	}
	orch = NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	if err := orch.Discover(ctx); err == nil {
		t.Fatal("expected empty-domain error")
	}

	parseWezTermConfig = func(string) (*wezterm.Config, error) {
		return &wezterm.Config{
			SSHDomains: []wezterm.SSHDomain{
				{Name: "csd", RemoteAddress: "1.2.3.4", Username: "ubuntu", Port: 22},
			},
		}, nil
	}
	newTailscaleClient = func() tailscaleClient {
		return &fixtureTailscaleClient{available: false}
	}
	orch = NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	if err := orch.Discover(ctx); err != nil {
		t.Fatalf("discover should succeed with tailscale unavailable: %v", err)
	}
	if orch.localMachine == nil || len(orch.remoteMachines) != 1 {
		t.Fatalf("expected local + 1 remote after discovery")
	}
	if orch.tailscale != nil {
		t.Fatalf("expected tailscale client to be disabled when unavailable")
	}

	parseWezTermConfig = func(string) (*wezterm.Config, error) {
		return &wezterm.Config{
			SSHDomains: []wezterm.SSHDomain{
				{Name: "css", RemoteAddress: "9.9.9.9", Username: "hope", Port: 22},
			},
		}, nil
	}
	newTailscaleClient = func() tailscaleClient {
		return &fixtureTailscaleClient{
			available: true,
			self: &tailscale.Peer{
				HostName:     "local-ts",
				TailscaleIPs: []string{"100.100.100.1"},
			},
			peers: []*tailscale.Peer{
				{HostName: "coordinator-ts", TailscaleIPs: []string{"100.100.100.9"}},
			},
			byHost: map[string]*tailscale.Peer{
				"css": {HostName: "coordinator-ts", TailscaleIPs: []string{"100.100.100.9"}},
			},
		}
	}
	orch = NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	if err := orch.Discover(ctx); err != nil {
		t.Fatalf("discover with tailscale should succeed: %v", err)
	}
	if orch.localMachine.TailscaleIP != "100.100.100.1" {
		t.Fatalf("expected local tailscale IP to be set, got %q", orch.localMachine.TailscaleIP)
	}
	if got := orch.remoteMachines[0].TailscaleIP; got != "100.100.100.9" {
		t.Fatalf("expected remote tailscale IP match, got %q", got)
	}
	if got := orch.remoteMachines[0].Name; got != "coordinator-ts" {
		t.Fatalf("expected remote name from matched peer, got %q", got)
	}
}

func TestTestConnectivityCoveragePaths(t *testing.T) {
	restore := saveSetupGlobals()
	defer restore()

	orch := NewOrchestrator(Options{UseTailscale: true, Logger: slog.Default()})
	m := &DiscoveredMachine{Name: "host", PublicIP: "1.2.3.4", Username: "u", Port: 22}

	testMachineConnectivity = func(*sync.Machine, sync.ConnectOptions) *sync.ConnectivityResult {
		return &sync.ConnectivityResult{Success: true}
	}
	if err := orch.TestConnectivity(context.Background(), m); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !m.IsReachable {
		t.Fatalf("expected machine to be marked reachable")
	}

	testMachineConnectivity = func(*sync.Machine, sync.ConnectOptions) *sync.ConnectivityResult {
		return &sync.ConnectivityResult{Success: false, Error: errors.New("unreachable")}
	}
	if err := orch.TestConnectivity(context.Background(), m); err == nil {
		t.Fatalf("expected connectivity error")
	}
	if m.IsReachable {
		t.Fatalf("expected machine to be marked unreachable")
	}
}

func TestDeployCoordinatorCoveragePaths(t *testing.T) {
	restore := saveSetupGlobals()
	defer restore()

	ctx := context.Background()
	orch := NewOrchestrator(Options{RemotePort: 9000, Logger: slog.Default()})
	m := &DiscoveredMachine{Name: "host", PublicIP: "1.2.3.4", Username: "u", Port: 22}

	fixture := &fixtureCoordinatorDeployer{
		result: &deploy.DeployResult{Machine: "host", Success: true},
	}
	newCoordinatorDeployer = func(*sync.Machine, *slog.Logger) coordinatorDeployer {
		return fixture
	}
	result, err := orch.deployCoordinator(ctx, m)
	if err != nil {
		t.Fatalf("expected deploy success, got error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success deploy result")
	}
	if fixture.lastConfig.Port != 9000 {
		t.Fatalf("expected remote port 9000 in deploy config, got %d", fixture.lastConfig.Port)
	}
	if !fixture.connected || !fixture.disposed {
		t.Fatalf("expected connect + disconnect to run")
	}

	newCoordinatorDeployer = func(*sync.Machine, *slog.Logger) coordinatorDeployer {
		return &fixtureCoordinatorDeployer{connectErr: errors.New("connect boom")}
	}
	if _, err := orch.deployCoordinator(ctx, m); err == nil {
		t.Fatalf("expected connect failure")
	}

	newCoordinatorDeployer = func(*sync.Machine, *slog.Logger) coordinatorDeployer {
		return &fixtureCoordinatorDeployer{deployErr: errors.New("deploy boom")}
	}
	if _, err := orch.deployCoordinator(ctx, m); err == nil {
		t.Fatalf("expected deploy failure")
	}
}
