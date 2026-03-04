package refresh

import (
	"errors"
	"fmt"
)

// ErrUnsupported indicates that a refresh operation is not supported or not
// configured for a given provider/profile.
//
// Callers should generally treat this as a "skipped" outcome rather than a hard
// failure.
var ErrUnsupported = errors.New("refresh unsupported")

// UnsupportedError is returned when refresh cannot be performed for a provider
// due to missing required configuration or unsupported auth file formats.
type UnsupportedError struct {
	Provider string
	Reason   string
}

func (e *UnsupportedError) Error() string {
	if e == nil {
		return "refresh unsupported"
	}

	switch {
	case e.Provider == "" && e.Reason == "":
		return "refresh unsupported"
	case e.Provider == "":
		return fmt.Sprintf("refresh unsupported: %s", e.Reason)
	case e.Reason == "":
		return fmt.Sprintf("%s refresh unsupported", e.Provider)
	default:
		return fmt.Sprintf("%s refresh unsupported: %s", e.Provider, e.Reason)
	}
}

func (e *UnsupportedError) Unwrap() error {
	return ErrUnsupported
}
