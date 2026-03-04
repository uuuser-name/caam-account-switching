package cmd

import "testing"

func TestIsProfileUsableForSummary(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		inCooldown bool
		want       bool
	}{
		{name: "healthy usable", status: "healthy", inCooldown: false, want: true},
		{name: "warning usable", status: "warning", inCooldown: false, want: true},
		{name: "critical blocked", status: "critical", inCooldown: false, want: false},
		{name: "cooldown blocks healthy", status: "healthy", inCooldown: true, want: false},
		{name: "cooldown blocks warning", status: "warning", inCooldown: true, want: false},
		{name: "unknown status fail-open", status: "mystery", inCooldown: false, want: true},
		{name: "empty status usable", status: "", inCooldown: false, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isProfileUsableForSummary(tc.status, tc.inCooldown)
			if got != tc.want {
				t.Fatalf("isProfileUsableForSummary(%q, %v) = %v, want %v", tc.status, tc.inCooldown, got, tc.want)
			}
		})
	}
}
