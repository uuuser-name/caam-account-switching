package exec

// HandoffState represents the current state of the smart handoff process.
type HandoffState int

const (
	// Running means the CLI is executing normally.
	Running HandoffState = iota

	// RateLimited means a rate limit pattern was detected.
	RateLimited

	// SelectingBackup means we are choosing the next profile.
	SelectingBackup

	// SwappingAuth means we are updating the auth files on disk.
	SwappingAuth

	// LoggingIn means we are injecting the login command into the PTY.
	LoggingIn

	// LoginComplete means the login flow finished successfully.
	LoginComplete

	// HandoffFailed means the handoff failed and we are rolling back or alerting.
	HandoffFailed

	// ManualMode means automatic handoff failed or is disabled, user must act.
	ManualMode
)

func (s HandoffState) String() string {
	switch s {
	case Running:
		return "RUNNING"
	case RateLimited:
		return "RATE_LIMITED"
	case SelectingBackup:
		return "SELECTING_BACKUP"
	case SwappingAuth:
		return "SWAPPING_AUTH"
	case LoggingIn:
		return "LOGGING_IN"
	case LoginComplete:
		return "LOGIN_COMPLETE"
	case HandoffFailed:
		return "HANDOFF_FAILED"
	case ManualMode:
		return "MANUAL_MODE"
	default:
		return "UNKNOWN"
	}
}
