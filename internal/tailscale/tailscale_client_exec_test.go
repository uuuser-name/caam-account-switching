package tailscale

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
	return path
}

func TestClientCommandExecutionPaths(t *testing.T) {
	tmp := t.TempDir()
	okScript := writeExecutable(t, tmp, "tailscale-ok.sh", "#!/bin/sh\nif [ \"$1\" = \"status\" ] && [ \"$2\" = \"--json\" ]; then\ncat <<'JSON'\n"+sampleStatusJSON+"\nJSON\nexit 0\nfi\necho \"bad args\" >&2\nexit 2\n")
	errScript := writeExecutable(t, tmp, "tailscale-err.sh", "#!/bin/sh\necho \"tailscale not available\" >&2\nexit 1\n")
	badJSONScript := writeExecutable(t, tmp, "tailscale-badjson.sh", "#!/bin/sh\necho '{'\n")

	t.Run("is available true", func(t *testing.T) {
		c := &Client{binaryPath: okScript}
		if !c.IsAvailable(context.Background()) {
			t.Fatal("IsAvailable() = false, want true")
		}
	})

	t.Run("is available false", func(t *testing.T) {
		c := &Client{binaryPath: errScript}
		if c.IsAvailable(context.Background()) {
			t.Fatal("IsAvailable() = true, want false")
		}
	})

	t.Run("get status success", func(t *testing.T) {
		c := &Client{binaryPath: okScript}
		status, err := c.GetStatus(context.Background())
		if err != nil {
			t.Fatalf("GetStatus() error = %v", err)
		}
		if status == nil || status.Self == nil {
			t.Fatal("GetStatus() returned nil status/self")
		}
		if len(status.Peer) != 4 {
			t.Fatalf("GetStatus().Peer len = %d, want 4", len(status.Peer))
		}
	})

	t.Run("get status command error", func(t *testing.T) {
		c := &Client{binaryPath: errScript}
		if _, err := c.GetStatus(context.Background()); err == nil {
			t.Fatal("GetStatus() expected command error")
		}
	})

	t.Run("get status invalid json", func(t *testing.T) {
		c := &Client{binaryPath: badJSONScript}
		if _, err := c.GetStatus(context.Background()); err == nil {
			t.Fatal("GetStatus() expected JSON parse error")
		}
	})
}

func TestClientPeerDiscoveryPaths(t *testing.T) {
	tmp := t.TempDir()
	okScript := writeExecutable(t, tmp, "tailscale-ok.sh", "#!/bin/sh\nif [ \"$1\" = \"status\" ] && [ \"$2\" = \"--json\" ]; then\ncat <<'JSON'\n"+sampleStatusJSON+"\nJSON\nexit 0\nfi\nexit 2\n")

	c := &Client{binaryPath: okScript}
	ctx := context.Background()

	self, err := c.GetSelf(ctx)
	if err != nil {
		t.Fatalf("GetSelf() error = %v", err)
	}
	if self == nil || self.HostName != "threadripperje" {
		t.Fatalf("GetSelf() host = %v, want threadripperje", self)
	}

	peers, err := c.GetPeers(ctx)
	if err != nil {
		t.Fatalf("GetPeers() error = %v", err)
	}
	if len(peers) != 4 {
		t.Fatalf("GetPeers() len = %d, want 4", len(peers))
	}

	online, err := c.GetOnlinePeers(ctx)
	if err != nil {
		t.Fatalf("GetOnlinePeers() error = %v", err)
	}
	if len(online) != 3 {
		t.Fatalf("GetOnlinePeers() len = %d, want 3", len(online))
	}

	peer, err := c.FindPeerByHostname(ctx, "superserver")
	if err != nil {
		t.Fatalf("FindPeerByHostname exact error = %v", err)
	}
	if peer == nil || peer.HostName != "SuperServer" {
		t.Fatalf("FindPeerByHostname exact = %v", peer)
	}

	peer, err = c.FindPeerByHostname(ctx, "sense")
	if err != nil {
		t.Fatalf("FindPeerByHostname prefix error = %v", err)
	}
	if peer == nil || peer.HostName != "SenseDemoBox" {
		t.Fatalf("FindPeerByHostname prefix = %v", peer)
	}

	peer, err = c.FindPeerByHostname(ctx, "mac mini")
	if err != nil {
		t.Fatalf("FindPeerByHostname contains error = %v", err)
	}
	if peer == nil || peer.HostName != "Jeffrey's Mac mini" {
		t.Fatalf("FindPeerByHostname contains = %v", peer)
	}

	peer, err = c.FindPeerByHostname(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("FindPeerByHostname missing error = %v", err)
	}
	if peer != nil {
		t.Fatalf("FindPeerByHostname missing = %v, want nil", peer)
	}

	peer, err = c.FindPeerByIP(ctx, "100.90.148.85")
	if err != nil {
		t.Fatalf("FindPeerByIP found error = %v", err)
	}
	if peer == nil || peer.HostName != "SuperServer" {
		t.Fatalf("FindPeerByIP found = %v", peer)
	}

	peer, err = c.FindPeerByIP(ctx, "100.64.0.1")
	if err != nil {
		t.Fatalf("FindPeerByIP missing error = %v", err)
	}
	if peer != nil {
		t.Fatalf("FindPeerByIP missing = %v, want nil", peer)
	}
}

