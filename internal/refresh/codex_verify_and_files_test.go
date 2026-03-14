package refresh

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestVerifyCodexToken(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	t.Run("success", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.openai.com/v1/me" {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-token")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		})

		if err := VerifyCodexToken(context.Background(), "test-token"); err != nil {
			t.Fatalf("VerifyCodexToken() error = %v", err)
		}
	})

	t.Run("non-200 response", func(t *testing.T) {
		http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
				Header:     make(http.Header),
			}, nil
		})

		err := VerifyCodexToken(context.Background(), "bad-token")
		if err == nil {
			t.Fatal("VerifyCodexToken() expected error for unauthorized response")
		}
		if !strings.Contains(err.Error(), "status 401") {
			t.Fatalf("VerifyCodexToken() error = %v, want status 401", err)
		}
	})
}

func TestFilesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    map[string][]byte
		b    map[string][]byte
		want bool
	}{
		{
			name: "equal maps",
			a: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"abc"}`),
			},
			b: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"abc"}`),
			},
			want: true,
		},
		{
			name: "different lengths",
			a: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"abc"}`),
			},
			b:    map[string][]byte{},
			want: false,
		},
		{
			name: "different content",
			a: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"abc"}`),
			},
			b: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"xyz"}`),
			},
			want: false,
		},
		{
			name: "missing key",
			a: map[string][]byte{
				"/tmp/auth.json": []byte(`{"token":"abc"}`),
			},
			b: map[string][]byte{
				"/tmp/other.json": []byte(`{"token":"abc"}`),
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := filesEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("filesEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}
