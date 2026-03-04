package refresh

import (
	"errors"
	"testing"
)

// =============================================================================
// UnsupportedError Tests
// =============================================================================

func TestUnsupportedError_Error_Nil(t *testing.T) {
	var e *UnsupportedError
	result := e.Error()
	if result != "refresh unsupported" {
		t.Errorf("nil error.Error() = %q, want %q", result, "refresh unsupported")
	}
}

func TestUnsupportedError_Error_Empty(t *testing.T) {
	e := &UnsupportedError{}
	result := e.Error()
	if result != "refresh unsupported" {
		t.Errorf("empty error.Error() = %q, want %q", result, "refresh unsupported")
	}
}

func TestUnsupportedError_Error_OnlyProvider(t *testing.T) {
	e := &UnsupportedError{Provider: "codex"}
	result := e.Error()
	want := "codex refresh unsupported"
	if result != want {
		t.Errorf("error.Error() = %q, want %q", result, want)
	}
}

func TestUnsupportedError_Error_OnlyReason(t *testing.T) {
	e := &UnsupportedError{Reason: "missing credentials"}
	result := e.Error()
	want := "refresh unsupported: missing credentials"
	if result != want {
		t.Errorf("error.Error() = %q, want %q", result, want)
	}
}

func TestUnsupportedError_Error_ProviderAndReason(t *testing.T) {
	e := &UnsupportedError{Provider: "gemini", Reason: "no refresh token"}
	result := e.Error()
	want := "gemini refresh unsupported: no refresh token"
	if result != want {
		t.Errorf("error.Error() = %q, want %q", result, want)
	}
}

func TestUnsupportedError_Unwrap(t *testing.T) {
	e := &UnsupportedError{Provider: "claude", Reason: "test reason"}
	unwrapped := e.Unwrap()
	if unwrapped != ErrUnsupported {
		t.Errorf("Unwrap() = %v, want ErrUnsupported", unwrapped)
	}
}

func TestUnsupportedError_ErrorsIs(t *testing.T) {
	e := &UnsupportedError{Provider: "codex", Reason: "not supported"}

	// Should be able to use errors.Is with ErrUnsupported
	if !errors.Is(e, ErrUnsupported) {
		t.Error("errors.Is(e, ErrUnsupported) = false, want true")
	}
}

func TestErrUnsupported_Sentinel(t *testing.T) {
	// Verify ErrUnsupported is a proper sentinel error
	if ErrUnsupported.Error() != "refresh unsupported" {
		t.Errorf("ErrUnsupported.Error() = %q, want %q", ErrUnsupported.Error(), "refresh unsupported")
	}
}
