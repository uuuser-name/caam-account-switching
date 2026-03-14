package testutil

import "os"

type envTestT interface {
	Helper()
	Cleanup(func())
	Setenv(string, string)
}

// UnsetEnv unsets an environment variable for the duration of a test and
// restores the original value on cleanup when one existed.
func UnsetEnv(t envTestT, key string) {
	t.Helper()

	orig, ok := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	if ok {
		t.Cleanup(func() {
			t.Setenv(key, orig)
		})
	}
}
