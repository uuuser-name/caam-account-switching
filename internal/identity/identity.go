// Package identity extracts account identity details from provider auth artifacts.
package identity

import "time"

// Identity captures account metadata extracted from auth files.
type Identity struct {
	Email        string    `json:"email,omitempty"`
	Organization string    `json:"organization,omitempty"`
	PlanType     string    `json:"plan_type,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Provider     string    `json:"provider"`
}
